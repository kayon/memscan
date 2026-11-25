package memscan

import (
	"errors"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/kayon/memscan/deck"
	"github.com/kayon/memscan/scanner"
)

// The tests below scan their own process. A page-aligned anonymous mapping is
// filled with known values; the results are then restricted to that mapping,
// so that whatever else lives in the test binary does not matter.

type plantedArea struct {
	mem   []byte
	start uint64
}

func newPlantedArea(t *testing.T, size int) *plantedArea {
	t.Helper()
	mem, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Skipf("cannot map memory: %v", err)
	}
	t.Cleanup(func() { _ = unix.Munmap(mem) })
	return &plantedArea{mem: mem, start: uint64(uintptr(unsafe.Pointer(&mem[0])))}
}

// plant copies the value at the given offsets and returns the addresses.
func (a *plantedArea) plant(v *scanner.Value, offsets ...int) (addrs []uint64) {
	for _, off := range offsets {
		copy(a.mem[off:], v.Bytes())
		addrs = append(addrs, a.start+uint64(off))
	}
	return
}

// slots returns count offsets, one per 4 KiB page, in the 16-byte slot slotIndex
// of the page (every offset is a multiple of 8).
func slots(count, slotIndex int) (offsets []int) {
	for k := 0; k < count; k++ {
		offsets = append(offsets, k*4096+slotIndex*16)
	}
	return
}

// Tests that count results use values that are different on every run: copies
// of a constant needle pile up in the heap of the test process (garbage of the
// previous tests or iterations) and would be counted as results too.
func randomInt32() int32 { return int32(rand.Uint32() | 0x01010101) }

func randomInt64() int64 { return int64(rand.Uint64() | 0x0101010101010101) }

// within keeps the addresses that belong to the area.
func (a *plantedArea) within(addrs []uint64) (kept []uint64) {
	for _, addr := range addrs {
		if addr >= a.start && addr < a.start+uint64(len(a.mem)) {
			kept = append(kept, addr)
		}
	}
	return
}

func selfProcess(t *testing.T) *deck.Process {
	t.Helper()
	proc, err := deck.NewProcess(os.Getpid())
	if err != nil || proc == nil {
		t.Skipf("cannot inspect our own process: %v", err)
	}
	return proc
}

type pauseCounter struct{ paused, resumed int }

func newSelfSession(t *testing.T) (*Session, *pauseCounter) {
	t.Helper()
	s := NewSession()
	if err := s.Open(selfProcess(t)); err != nil {
		t.Skipf("cannot open /proc/self/maps: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// pausing ourselves would freeze the test
	counter := &pauseCounter{}
	s.pause = func() error { counter.paused++; return nil }
	s.resume = func() error { counter.resumed++; return nil }
	return s, counter
}

func assertSorted(t *testing.T, name string, addrs []uint64) {
	t.Helper()
	for i := 1; i < len(addrs); i++ {
		if addrs[i-1] >= addrs[i] {
			t.Fatalf("%s: results not strictly ascending at %d (%#x, %#x)", name, i, addrs[i-1], addrs[i])
		}
	}
}

func TestSessionFirstScanFindsPlantedValues(t *testing.T) {
	const pages = 120
	area := newPlantedArea(t, pages*4096)

	values := []*scanner.Value{
		scanner.NewInt8(0x5A),
		scanner.NewInt16(0x6D3F),
		scanner.NewInt32(0x6F1E9A3C),
		scanner.NewInt64(0x1D2C3B4A5F607182),
		scanner.NewFloat32(1234.5),
		scanner.NewFloat64(98765.4321),
	}
	want := make(map[scanner.Type][]uint64)
	for i, v := range values {
		want[v.Type()] = area.plant(v, slots(pages, i)...)
		if v.Type() != scanner.Int8 {
			// the same bytes on an odd offset are not aligned: must be ignored
			area.plant(v, slots(pages, i)[0]+2048+1)
		}
	}

	s, counter := newSelfSession(t)
	if _, err := s.FirstScan(values, false); err != nil {
		t.Fatal(err)
	}
	if counter.paused != 1 || counter.resumed != 1 {
		t.Fatalf("process paused %d times, resumed %d times; want once each", counter.paused, counter.resumed)
	}

	infos := s.Infos()
	if len(infos) != len(values) {
		t.Fatalf("%d types reported, want %d", len(infos), len(values))
	}
	for _, v := range values {
		m := s.Scan(v.Type())
		if m == nil {
			t.Fatalf("%v: no scan", v.Type())
		}
		all := m.Results()
		assertSorted(t, v.Type().String(), all)
		if m.Rounds() != 1 || m.CanUndo() {
			t.Fatalf("%v: round %d, canUndo %v", v.Type(), m.Rounds(), m.CanUndo())
		}
		got := area.within(all)
		if !reflect.DeepEqual(got, want[v.Type()]) {
			t.Fatalf("%v: %d hits in the area, want %d", v.Type(), len(got), len(want[v.Type()]))
		}
	}
	for _, info := range infos {
		if info.Status != StatusOK {
			t.Fatalf("%v: status %v", info.Type, info.Status)
		}
	}
}

// The session must find exactly what the single-type engine finds.
func TestSessionMatchesSingleTypeEngine(t *testing.T) {
	const pages = 150
	area := newPlantedArea(t, pages*4096)

	rounded := scanner.NewFloat32(3)
	rounded.WithOption(scanner.OptionFloatRounded)
	truncated := scanner.NewFloat64(7)
	truncated.WithOption(scanner.OptionFloatTruncated)

	// no Int8 here: a one-byte needle can match so many bytes of a region that
	// the single-type engine overflows its per-region buffer (it does not check
	// it); Int8 is covered by TestSessionFirstScanFindsPlantedValues
	i32, i64 := scanner.NewInt32(randomInt32()), scanner.NewInt64(randomInt64())
	values := []*scanner.Value{i32, i64, rounded, truncated}
	// values around the targets: inside and outside the rounded/truncated ranges
	for k := 0; k < pages; k++ {
		area.plant(scanner.NewFloat32(float32(2.4+0.2*float64(k%9))), k*4096+0)
		area.plant(scanner.NewFloat64(6.5+0.25*float64(k%7)), k*4096+8)
		area.plant(i32, k*4096+16)
		area.plant(i64, k*4096+24+8*(k%3))
	}

	proc := selfProcess(t)
	s, _ := newSelfSession(t)
	if _, err := s.FirstScan(values, false); err != nil {
		t.Fatal(err)
	}

	for _, v := range values {
		legacy := NewMemscan()
		if err := legacy.Open(proc); err != nil {
			t.Skipf("cannot open the process: %v", err)
		}
		legacy.FirstScan(v, true) // true: process is already "paused"
		want := area.within(append([]uint64(nil), legacy.Results()...))
		legacy.release()
		_ = legacy.Close()

		got := area.within(s.Scan(v.Type()).Results())
		if len(want) < 10 {
			t.Fatalf("%v: test data is too weak (%d reference hits)", v.Type(), len(want))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%v (option %v): %d hits in the area, single-type engine finds %d",
				v.Type(), v.Option(), len(got), len(want))
		}
	}
}

func TestSessionLimits(t *testing.T) {
	t.Run("soft limit parks a type, hard limit drops it", func(t *testing.T) {
		area := newPlantedArea(t, 400*4096)
		ok := scanner.NewInt32(randomInt32())
		parked := scanner.NewInt64(randomInt64())
		flood := scanner.NewInt8(0x5A)

		okAddrs := area.plant(ok, slots(30, 0)...)
		parkedAddrs := area.plant(parked, slots(300, 1)...)
		for i := 0; i < 8000; i++ {
			area.mem[350*4096+i] = 0x5A // after the pages used by the other types
		}

		s, _ := newSelfSession(t)
		s.limits = ResultLimits{Soft: 100, Hard: 1000, Global: 1 << 30}
		if _, err := s.FirstScan([]*scanner.Value{ok, parked, flood}, false); err != nil {
			t.Fatal(err)
		}

		status := map[scanner.Type]TypeStatus{}
		for _, info := range s.Infos() {
			status[info.Type] = info.Status
		}
		if status[scanner.Int32] != StatusOK {
			t.Errorf("Int32 %v, want ok", status[scanner.Int32])
		}
		if status[scanner.Int64] != StatusParked {
			t.Errorf("Int64 %v, want parked", status[scanner.Int64])
		}
		if status[scanner.Int8] != StatusTruncated {
			t.Errorf("Int8 %v, want truncated", status[scanner.Int8])
		}

		if got := area.within(s.Scan(scanner.Int32).Results()); !reflect.DeepEqual(got, okAddrs) {
			t.Errorf("Int32: %d hits kept, want %d", len(got), len(okAddrs))
		}
		// a parked type is stored in full: it can still be refined
		if got := area.within(s.Scan(scanner.Int64).Results()); !reflect.DeepEqual(got, parkedAddrs) {
			t.Errorf("Int64: %d hits kept, want %d", len(got), len(parkedAddrs))
		}
		if n := s.Scan(scanner.Int8).Count(); n != 0 {
			t.Errorf("truncated Int8 still holds %d results", n)
		}
	})

	t.Run("global limit drops the biggest type", func(t *testing.T) {
		area := newPlantedArea(t, 400*4096)
		a := scanner.NewInt32(randomInt32())
		b := scanner.NewInt64(randomInt64())
		area.plant(a, slots(300, 0)...)
		bAddrs := area.plant(b, slots(200, 1)...)

		s, _ := newSelfSession(t)
		s.limits = ResultLimits{Soft: 1 << 30, Hard: 1 << 30, Global: 400}
		if _, err := s.FirstScan([]*scanner.Value{a, b}, false); err != nil {
			t.Fatal(err)
		}

		status := map[scanner.Type]TypeStatus{}
		for _, info := range s.Infos() {
			status[info.Type] = info.Status
		}
		if status[scanner.Int32] != StatusTruncated || status[scanner.Int64] != StatusOK {
			t.Fatalf("statuses Int32=%v Int64=%v, want truncated/ok", status[scanner.Int32], status[scanner.Int64])
		}
		if got := area.within(s.Scan(scanner.Int64).Results()); !reflect.DeepEqual(got, bAddrs) {
			t.Fatalf("Int64: %d hits kept, want %d", len(got), len(bAddrs))
		}
	})
}

func TestSessionRejectsBadRequests(t *testing.T) {
	s, counter := newSelfSession(t)

	if _, err := s.FirstScan(nil, false); !errors.Is(err, ErrNoValues) {
		t.Errorf("no value: %v", err)
	}
	if _, err := s.FirstScan([]*scanner.Value{scanner.NewBytes([]byte{1, 2, 3})}, false); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("bytes: %v", err)
	}
	dup := []*scanner.Value{scanner.NewInt32(1), scanner.NewInt32(2)}
	if _, err := s.FirstScan(dup, false); !errors.Is(err, ErrDuplicateType) {
		t.Errorf("duplicate type: %v", err)
	}
	if counter.paused != 0 {
		t.Errorf("the process was paused for a rejected request")
	}

	closed := NewSession()
	if _, err := closed.FirstScan([]*scanner.Value{scanner.NewInt32(1)}, false); !errors.Is(err, ErrSessionNotOpen) {
		t.Errorf("closed session: %v", err)
	}
}

func TestSessionResumesAfterPauseFailure(t *testing.T) {
	s, _ := newSelfSession(t)
	resumed := 0
	s.pause = func() error { return errors.New("no such process") }
	s.resume = func() error { resumed++; return nil }
	if _, err := s.FirstScan([]*scanner.Value{scanner.NewInt32(1)}, false); err == nil {
		t.Fatal("expected an error when the process cannot be paused")
	}
	if resumed != 0 {
		t.Fatalf("resume called %d times although the pause failed", resumed)
	}
}
