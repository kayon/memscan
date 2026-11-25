package scanner

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

// chunkMatcherData builds a buffer of noise with many copies of the searched
// pattern planted at random (aligned and misaligned) places, adjacent copies,
// and copies placed right around every 8-byte boundary that tests cut on.
func chunkMatcherData(rng *rand.Rand, size int, plant func(rng *rand.Rand, dst []byte), width int) []byte {
	data := make([]byte, size)
	// small alphabet: many accidental partial matches (hard cases for Index)
	for i := range data {
		data[i] = byte(rng.Intn(4))
	}
	for k := 0; k < 400; k++ {
		pos := rng.Intn(size - width)
		plant(rng, data[pos:pos+width])
	}
	// adjacent copies
	for k := 0; k < 50; k++ {
		pos := rng.Intn(size - 4*width)
		for j := 0; j < 4; j++ {
			plant(rng, data[pos+j*width:pos+(j+1)*width])
		}
	}
	// copies around chunk boundaries (multiples of 64)
	for b := 64; b+width < size; b += 64 * 37 {
		for d := -width; d <= width; d++ {
			if b+d >= 0 && b+d+width <= size {
				plant(rng, data[b+d:b+d+width])
			}
		}
	}
	return data
}

func collectOffsets(m ChunkMatcher, data []byte, chunkSize int) (offsets []int) {
	for off := 0; off < len(data); off += chunkSize {
		end := off + chunkSize
		if end > len(data) {
			end = len(data)
		}
		m.ScanChunk(data[off:end], off, func(offset int) bool {
			offsets = append(offsets, offset)
			return true
		})
	}
	return
}

func TestChunkMatcherEquivalence(t *testing.T) {
	withOption := func(v *Value, opt Option) *Value {
		v.WithOption(opt)
		return v
	}
	fixed := func(v *Value) func(*rand.Rand, []byte) {
		return func(_ *rand.Rand, dst []byte) { copy(dst, v.data) }
	}
	// planted floats are spread around the target so that the range matchers
	// (rounded / extreme / truncated) see values inside and outside their range
	nearF32 := func(target float64) func(*rand.Rand, []byte) {
		return func(rng *rand.Rand, dst []byte) {
			v := float32(target + (rng.Float64()*3 - 1.5))
			byteOrder.PutUint32(dst, math.Float32bits(v))
		}
	}
	nearF64 := func(target float64) func(*rand.Rand, []byte) {
		return func(rng *rand.Rand, dst []byte) {
			v := target + (rng.Float64()*3 - 1.5)
			byteOrder.PutUint64(dst, math.Float64bits(v))
		}
	}

	cases := []struct {
		name  string
		value *Value
		plant func(*rand.Rand, []byte)
	}{
		{"Int8", NewInt8(2), fixed(NewInt8(2))},
		{"Int8 high bit", NewInt8(-56), fixed(NewInt8(-56))},
		{"Int16", NewInt16(0x0302), fixed(NewInt16(0x0302))},
		{"Int16 repeated byte", NewInt16(0x0101), fixed(NewInt16(0x0101))},
		{"Int32", NewInt32(0x01020301), fixed(NewInt32(0x01020301))},
		{"Int32 repeated byte", NewInt32(0x01010101), fixed(NewInt32(0x01010101))},
		{"Int64", NewInt64(0x0102030001020300), fixed(NewInt64(0x0102030001020300))},
		{"Int64 repeated byte", NewInt64(0x0101010101010101), fixed(NewInt64(0x0101010101010101))},
		{"Float32 unrounded", NewFloat32(3.4), fixed(NewFloat32(3.4))},
		{"Float64 unrounded", NewFloat64(3.4), fixed(NewFloat64(3.4))},
	}
	for _, opt := range []struct {
		name string
		opt  Option
	}{{"rounded", OptionFloatRounded}, {"extreme", OptionFloatExtreme}, {"truncated", OptionFloatTruncated}} {
		cases = append(cases,
			struct {
				name  string
				value *Value
				plant func(*rand.Rand, []byte)
			}{"Float32 " + opt.name, withOption(NewFloat32(3.4), opt.opt), nearF32(3.4)},
			struct {
				name  string
				value *Value
				plant func(*rand.Rand, []byte)
			}{"Float64 " + opt.name, withOption(NewFloat64(3.4), opt.opt), nearF64(3.4)},
			struct {
				name  string
				value *Value
				plant func(*rand.Rand, []byte)
			}{"Float32 integer " + opt.name, withOption(NewFloat32(3), opt.opt), nearF32(3)},
		)
	}

	rng := rand.New(rand.NewSource(42))
	// size is not a multiple of 8: the tail must behave like Scanner too
	const size = 1<<20 + 13
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			width := tc.value.Size()
			data := chunkMatcherData(rng, size, tc.plant, width)

			var want []int
			legacy := NewScanner(context.Background(), *tc.value)
			legacy.ScanCollector(bytes.NewReader(data), func(offset int) bool {
				want = append(want, offset)
				return true
			}, nil)
			if len(want) < 50 {
				t.Fatalf("test data is too weak: only %d reference matches", len(want))
			}

			matcher := NewChunkMatcher(*tc.value)
			if matcher == nil {
				t.Fatal("no matcher for a fixed-size type")
			}
			if matcher.Type() != tc.value.Type() {
				t.Fatalf("Type() = %v, want %v", matcher.Type(), tc.value.Type())
			}

			for _, chunkSize := range []int{8, 16, 24, 64, 4096, 1 << 16, size} {
				got := collectOffsets(matcher, data, chunkSize)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("chunk size %d: %d matches, want %d (first diff at %s)",
						chunkSize, len(got), len(want), firstDiff(got, want))
				}
			}
		})
	}
}

func firstDiff(got, want []int) string {
	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			return fmt.Sprintf("#%d: got %d, want %d", i, got[i], want[i])
		}
	}
	return fmt.Sprintf("length %d vs %d", len(got), len(want))
}

func TestChunkMatcherBytesUnsupported(t *testing.T) {
	if m := NewChunkMatcher(*NewBytes([]byte{1, 2, 3})); m != nil {
		t.Fatalf("Bytes must not get a chunk matcher, got %T", m)
	}
}

func TestChunkMatcherSmallChunks(t *testing.T) {
	for _, v := range []*Value{NewInt8(1), NewInt16(1), NewInt32(1), NewInt64(1), NewFloat32(1), NewFloat64(1)} {
		m := NewChunkMatcher(*v)
		for n := 0; n < v.Size(); n++ {
			// shorter than a value, or empty: nothing to report, nothing to crash on
			m.ScanChunk(make([]byte, n), 0, func(int) bool {
				t.Fatalf("%v: unexpected match in %d bytes", v.Type(), n)
				return true
			})
		}
	}
}

func TestChunkMatcherReportsAbsoluteOffsets(t *testing.T) {
	data := make([]byte, 64)
	copy(data[16:], NewInt32(0x01020304).data)
	var got []int
	NewChunkMatcher(*NewInt32(0x01020304)).ScanChunk(data, 4096, func(offset int) bool {
		got = append(got, offset)
		return true
	})
	if !reflect.DeepEqual(got, []int{4096 + 16}) {
		t.Fatalf("got %v", got)
	}
}

func TestChunkMatcherIgnoresMisalignedMatches(t *testing.T) {
	data := make([]byte, 32)
	copy(data[6:], NewInt32(0x01020304).data) // offset 6: not a multiple of 4
	NewChunkMatcher(*NewInt32(0x01020304)).ScanChunk(data, 0, func(offset int) bool {
		t.Fatalf("misaligned match reported at %d", offset)
		return true
	})
}

func TestChunkMatcherExactStopsWhenCollectorReturnsFalse(t *testing.T) {
	data := make([]byte, 64)
	for i := 0; i < 64; i += 4 {
		copy(data[i:], NewInt32(7).data)
	}
	calls := 0
	NewChunkMatcher(*NewInt32(7)).ScanChunk(data, 0, func(int) bool {
		calls++
		return calls < 3
	})
	if calls != 3 {
		t.Fatalf("collector called %d times, want 3", calls)
	}
}
