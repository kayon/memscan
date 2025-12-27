package scanner

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

type mockReader struct {
	data []byte
	off  int
	step int // if > 0 每次 Read 返回的字节数
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if m.off >= len(m.data) {
		return 0, io.EOF
	}
	n = copy(p, m.data[m.off:])
	if m.step > 0 && n > m.step {
		n = m.step
	}
	m.off += n
	return n, nil
}

func (m *mockReader) ReadFloat32(index int) (float32, error) {
	if index+4 > len(m.data) {
		return 0, io.EOF
	}
	value := NewFloat32(0)
	value.SetBytes(m.data[index : index+4])
	return value.ToRaw(Float32).(float32), nil
}

func (m *mockReader) ReadFloat64(index int) (float64, error) {
	if index+8 > len(m.data) {
		return 0, io.EOF
	}
	value := NewFloat64(0)
	value.SetBytes(m.data[index : index+8])
	return value.ToRaw(Float64).(float64), nil
}

func TestScanner(t *testing.T) {
	v := NewBytes([]byte(`hello`))
	scan := NewScanner(context.TODO(), *v)
	s := strings.Builder{}

	for i := 0; i < 128; i++ {
		s.WriteString("hhhellhell")
		data := strings.NewReader(s.String() + "hello")
		indexes := scan.Scan(data)
		if len(indexes) != 1 || indexes[0] != (i+1)*10 {
			t.Log(indexes, i)
			t.FailNow()
		}
	}
}

func TestScannerBuffer(t *testing.T) {
	// 00 00 00 00 00 00 F0 3F
	v := NewFloat64(1)
	scan := NewScannerBuffer(context.TODO(), *v, v.Size())

	for i := 1; i < v.Size(); i++ {
		n := i * 8
		b := append(make([]byte, n), v.data...)
		data := bytes.NewReader(b)
		ret := scan.Scan(data)
		if len(ret) != 1 || ret[0] != n {
			t.Log(ret, i)
			t.FailNow()
		}
	}
}

func TestScanner_Aligned(t *testing.T) {
	// 00 00 3F 80
	v := NewFloat32(1)
	scan := NewScannerBuffer(context.TODO(), *v, v.Size()+1)

	data := bytes.NewReader([]byte{
		0, 0, 0, 0, 0,
		0, 0x3F, 0x80, 0, 0,
	})
	ret := scan.Scan(data)
	if len(ret) > 0 {
		t.FailNow()
	}
}

func TestScanner_Truncated(t *testing.T) {
	t.Run("Aligned Cross-boundary Match", func(t *testing.T) {
		// [78 56 34 12]
		targetInt := int32(0x12345678)
		val := NewInt32(targetInt)

		data := make([]byte, 20)
		copy(data[8:], val.data)

		s := NewScannerBuffer(context.TODO(), *val, 10)

		// 模拟分片读取，每步 10 字节
		reader := &mockReader{data: data, step: 10}
		indexes := s.Scan(reader)

		expected := []int{8}
		if !reflect.DeepEqual(indexes, expected) {
			t.Errorf("Truncated alignment test failed: expected %v, got %v", expected, indexes)
		}
	})

	t.Run("Alignment and Truncation", func(t *testing.T) {
		// 测试对齐：特征码在索引 2 (非对齐) 和索引 4 (对齐)
		pattern := []byte{0xAA, 0xBB, 0xCC, 0xDD}
		v := NewBytes(pattern)
		v.typ = Int32 // 强制设为 Int32 对齐内存

		// 构造数据: 索引 4 是对齐的，索引 6 是非对齐的
		// [00 00 00 00 AA BB CC DD 00 00 AA BB CC DD]
		//              ^ Aligned         ^ Non aligned
		complexData := make([]byte, 14)
		copy(complexData[4:], pattern)
		copy(complexData[10:], pattern)

		// 缓冲区设为 5，步长设为 5
		// 这会使得索引 4 的特征码在读取第一块 [00 00 00 00 AA] 时就被截断
		s := NewScannerBuffer(context.TODO(), *v, 5)
		reader := &mockReader{data: complexData, step: 5}

		indexes := s.Scan(reader)

		// 期待结果：
		// 索引 4: 对齐 (4 % 4 == 0) -> 应该存在
		// 索引 10: 非对齐 (10 % 4 != 0) -> 应该被过滤
		expected := []int{4}
		if !reflect.DeepEqual(indexes, expected) {
			t.Errorf("Alignment failed, expected %v, got %v", expected, indexes)
		}
	})
}

func TestScanner_Float32WithOption(t *testing.T) {
	testValues := []float32{0, 99.5, 0, 100.0, 0, 100.2, 100.5, 101.0}
	var data = make([]byte, 32*2)

	for i, f := range testValues {
		v := NewFloat32(f)
		copy(data[32-8+i*4:], v.data)
	}

	f := float32(100.0)
	value := NewFloat32(f, OptionFloatRounded)
	rf32 := newRoundedFloat32(*value)
	t.Logf("[%f, %f]", rf32.min, rf32.max)

	want := make([]float32, 0, 8)
	for _, v := range testValues {
		if v >= rf32.min && v < rf32.max {
			want = append(want, v)
		}
	}
	t.Logf("Want: %v", want)

	s := NewScannerBuffer(context.TODO(), *value, 32)

	results := s.Scan(&mockReader{data: data})
	if len(results) != len(want) {
		t.FailNow()
	}

	for i, idx := range results {
		v := NewFloat32(0)
		v.SetBytes(data[idx : idx+4])
		fv := v.ToRaw(Float32).(float32)
		if fv != want[i] {
			t.Fatalf("want[%d] = %f, got: %f, at: %d", i, want[i], fv, idx)
		}
	}
}

func TestScanner_FloatOptions(t *testing.T) {
	testValues := []float32{99.0, 99.5, 100.0, 100.2, 100.5, 100.8, 101.0, 102.0}
	data := make([]byte, 4096)

	for i, v := range testValues {
		byteOrder.PutUint32(data[(i+1)*2*4:], math.Float32bits(v))
	}

	tests := []struct {
		name      string
		searchVal float32
		option    Option
		wantVals  []float32
	}{
		{
			name:      "Rounded",
			searchVal: 100.0,
			option:    OptionFloatRounded,
			wantVals:  []float32{99.5, 100.0, 100.2},
		},
		{
			name: "Extreme",
			// [99.0,101.0]
			searchVal: 100.0,
			option:    OptionFloatExtreme,
			wantVals:  []float32{99.5, 100.0, 100.2, 100.5, 100.8},
		},
		{
			name:      "Truncated",
			searchVal: 100.0,
			option:    OptionFloatTruncated,
			wantVals:  []float32{100.0, 100.2, 100.5, 100.8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := NewFloat32(tt.searchVal, tt.option)
			s := NewScannerBuffer(context.TODO(), *value, 4*10)

			reader := &mockReader{data: data}
			results := s.Scan(reader)

			if len(results) != len(tt.wantVals) {
				t.Errorf("%s: count mismatch. got %d, want %d", tt.name, len(results), len(tt.wantVals))
				return
			}

			for i, offset := range results {
				f, _ := reader.ReadFloat32(offset)
				if f != tt.wantVals[i] {
					t.Errorf("%s result[%d]: at offset %d got %f, want %f", tt.name, i, offset, f, tt.wantVals[i])
				}
			}
		})
	}
}

func BenchmarkScanner(b *testing.B) {
	const dataSize = 64 * 1024 * 1024
	data := make([]byte, dataSize)
	val := NewInt32(0x12345678)
	pattern := val.data
	copy(data[dataSize/2:], pattern)
	copy(data[dataSize-12:], pattern)

	var bufferSizes = [3]int{
		1 << 12,
		1 << 16,
		1 << 20,
	}

	var buffSizeFormat = func(n int) string {
		return fmt.Sprintf("%dKB", n/1024)
	}

	for _, size := range bufferSizes {
		b.Run("BufSize-"+buffSizeFormat(size), func(b *testing.B) {
			s := NewScannerBuffer(context.TODO(), *val, size)

			b.SetBytes(int64(dataSize))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				reader := &mockReader{data: data}
				indexes := s.Scan(reader)
				if len(indexes) == 0 {
					b.Fatal("should have matches")
				}
			}
		})
	}
}

func BenchmarkScannerFloatRounded(b *testing.B) {
	const dataSize = 64 * 1024 * 1024
	data := make([]byte, dataSize)
	rand.Read(data)

	target32 := float32(1234.9)
	target32Bytes := math.Float32bits(target32)
	byteOrder.PutUint32(data[dataSize/4:], target32Bytes)

	target64 := 1234.9
	target64Bytes := math.Float64bits(target64)
	byteOrder.PutUint64(data[dataSize/2:], target64Bytes)

	b.Run("Float32-Rounded", func(b *testing.B) {
		v := NewFloat32(target32, OptionFloatTruncated)
		s := NewScanner(context.TODO(), *v)

		b.SetBytes(int64(dataSize))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := &mockReader{data: data}
			indexes := s.Scan(reader)
			if len(indexes) == 0 {
				b.Fatal("float32 should have matches")
			}
		}
	})

	b.Run("Float64-Rounded", func(b *testing.B) {
		v := NewFloat64(target64, OptionFloatTruncated)
		s := NewScanner(context.TODO(), *v)

		b.SetBytes(int64(dataSize))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := &mockReader{data: data}
			indexes := s.Scan(reader)
			if len(indexes) == 0 {
				b.Fatal("float64 should have matches")
			}
		}
	})
}
