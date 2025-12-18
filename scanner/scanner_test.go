package scanner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
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
	step int // 每次 Read 返回的字节数
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

func TestScanner(t *testing.T) {
	v := NewBytes([]byte(`hello`))
	scanner := NewScanner(context.TODO(), v)
	s := strings.Builder{}

	for i := 0; i < 128; i++ {
		s.WriteString("hhhellhell")
		data := strings.NewReader(s.String() + "hello")
		indexes := scanner.Scan(data)
		if len(indexes) != 1 || indexes[0] != (i+1)*10 {
			t.Log(indexes, i)
			t.FailNow()
		}
	}
}

func TestScannerBuffer(t *testing.T) {
	// 00 00 00 00 00 00 F0 3F
	v := NewFloat64(1)
	scanner := NewScannerBuffer(context.TODO(), v, v.Size()+1)

	for i := 1; i < v.Size(); i++ {
		n := i * 8
		b := append(make([]byte, n), v.data...)
		data := bytes.NewReader(b)
		ret := scanner.Scan(data)
		if len(ret) != 1 || ret[0] != n {
			t.Log(ret, i)
			t.FailNow()
		}
	}
}

func TestScanner_Aligned(t *testing.T) {
	// 00 00 3F 80
	v := NewFloat32(1)
	scanner := NewScannerBuffer(context.TODO(), v, v.Size()+1)

	data := bytes.NewReader([]byte{
		0, 0, 0, 0, 0,
		0, 0x3F, 0x80, 0, 0,
	})
	ret := scanner.Scan(data)
	if len(ret) > 0 {
		t.FailNow()
	}
}

func TestScanner_Truncated(t *testing.T) {
	t.Run("Aligned Cross-boundary Match", func(t *testing.T) {
		// [78 56 34 12]
		targetInt := int32(0x12345678)
		val := NewInt32(targetInt)

		// 特征码放在索引 8 (对齐位置: 8 % 4 == 0)
		// [0 1 2 3 4 5 6 7 78 56 34 12 00 00 00 00]
		//                  ^ 期望结果在 8
		data := make([]byte, 20)
		copy(data[8:], val.data)

		// bufSize = 10
		// 第一轮：读取 data[0:10] -> [0 1 2 3 4 5 6 7 78 56]
		//        此时命中 [78 56] truncated
		// 第二轮：forward=2, 读取剩余数据
		//        拼接后在本地索引 0 处找到特征码
		s := NewScannerBuffer(context.TODO(), val, 10)

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
		s := NewScannerBuffer(context.TODO(), v, 5)
		reader := &mockReader{data: complexData, step: 5}

		indexes := s.Scan(reader)

		// 期待结果：
		// 索引 4: 对齐 (4 % 4 == 0) -> 应该存在
		// 索引 10: 不对齐 (10 % 4 != 0) -> 应该被过滤
		expected := []int{4}
		if !reflect.DeepEqual(indexes, expected) {
			t.Errorf("Alignment failed, expected %v, got %v", expected, indexes)
		}
	})
}

func TestScanner_FloatOptions(t *testing.T) {
	testValues := []float32{99.0, 100.0, 100.2, 100.5, 100.8, 101.0, 102.0}
	buf := new(bytes.Buffer)
	for _, v := range testValues {
		binary.Write(buf, binary.LittleEndian, v)
	}
	data := buf.Bytes()

	tests := []struct {
		name      string
		searchVal float32
		option    Option
		wantCount int
		wantVals  []float32 // 预期应该匹配到的原始值
	}{
		{
			name:      "Rounded",
			searchVal: 100.0,
			option:    OptionFloatRounded,
			wantCount: 2,
		},
		{
			name:      "Extreme",
			searchVal: 100.0,
			option:    OptionFloatExtreme,
			wantCount: 4,
		},
		{
			name:      "Truncated",
			searchVal: 100.0,
			option:    OptionFloatTruncated,
			wantCount: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewFloat32(tt.searchVal, tt.option)
			s := NewScanner(context.TODO(), v)
			results := s.Scan(bytes.NewReader(data))

			if len(results) != tt.wantCount {
				minV, maxV := v.integerFloat32Range()
				t.Errorf("%s[%f,%f]: got %d matches, want %d, %v", tt.name, minV, maxV, len(results), tt.wantCount, results)
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
			s := NewScannerBuffer(context.TODO(), val, size)

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

func BenchmarkScanner_Float_Truncated(b *testing.B) {
	const dataSize = 64 * 1024 * 1024
	data := make([]byte, dataSize)
	rand.Read(data)
	target := float32(1234.0)
	targetBytes := math.Float32bits(target)
	byteOrder.PutUint32(data[dataSize/4:], targetBytes)
	byteOrder.PutUint32(data[dataSize/2:], math.Float32bits(1234.9))

	b.Run("Float32-Truncated", func(b *testing.B) {
		v := NewFloat32(target, OptionFloatTruncated)
		s := NewScanner(context.TODO(), v)

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

	b.Run("Float64-Truncated", func(b *testing.B) {
		v := NewFloat64(float64(target), OptionFloatTruncated)
		s := NewScanner(context.TODO(), v)

		b.SetBytes(int64(dataSize))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := &mockReader{data: data}
			_ = s.Scan(reader)
		}
	})
}
