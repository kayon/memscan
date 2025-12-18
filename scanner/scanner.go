package scanner

import (
	"bytes"
	"context"
	"io"
)

const defScanBufferSize = 1 << 16

type Scanner struct {
	value   *Value
	bufSize int
	ctx     context.Context
}

func NewScanner(ctx context.Context, value *Value) *Scanner {
	return NewScannerBuffer(ctx, value, defScanBufferSize)
}

func NewScannerBuffer(ctx context.Context, value *Value, bufSize int) *Scanner {
	if bufSize < value.Size() {
		bufSize = value.Size() + 1
	}
	return &Scanner{
		value:   value,
		bufSize: bufSize,
		ctx:     ctx,
	}
}

func (s *Scanner) New(value *Value) {
	s.value = value
	if s.bufSize < value.Size() {
		s.bufSize = value.Size() + 1
	}
}

func (s *Scanner) Scan(reader io.Reader) (results []int) {
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	var (
		size = s.value.Size()
		// 每次搜索的块, 并为truncated预留了容量 size, 因为 truncated 只会小于 Value.Size
		chunk = make([]byte, s.bufSize+size)
		// 上一次 chunk 末尾的截断数据, 在下一轮追加到 chunk 头部
		truncated         []byte
		forward, backward int
		offset            int
		isAligned         = s.value.Aligned()
		pattern           = s.value.data

		f32Min, f32Max float32
		f64Min, f64Max float64
	)

	results = make([]int, 0, 1024)

	// float32/64 是整数, 并且使用了舍入选项
	applyFloatOption := s.value.isIntegerFloatWithOption()
	if applyFloatOption {
		switch s.value.Type() {
		case Float32:
			f32Min, f32Max = s.value.integerFloat32Range()
		case Float64:
			f64Min, f64Max = s.value.integerFloat64Range()
		}
	}

Scanning:
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			// 将上次截断的数据移到开头
			forward = copy(chunk, truncated)
			// 填充剩余空间
			backward = s.read(reader, chunk[forward:forward+s.bufSize])

			// 上次截断的 truncated + 本次读取, 不足以完成一次搜索
			if forward+backward < size {
				break
			}

			currentChunk := chunk[:forward+backward]
			chunkOffset := offset - forward

			if applyFloatOption {
				if s.value.Type() == Float32 {
					results = s.scanFloat32Unrolled(currentChunk, f32Min, f32Max, chunkOffset, results)
				} else {
					results = s.scanFloat64Unrolled(currentChunk, f64Min, f64Max, chunkOffset, results)
				}
			} else {
				i := 0
				for {
					n := bytes.Index(currentChunk[i:], pattern)
					if n < 0 {
						break
					}
					pos := i + n
					finalIndex := chunkOffset + pos

					if !isAligned || finalIndex%size == 0 {
						results = append(results, finalIndex)
					}
					i = pos + size
				}
			}

			if len(currentChunk) >= size {
				// 预留上一次可能匹配截断的 Value.Size 大小
				truncated = currentChunk[len(currentChunk)-size+1:]
			} else {
				truncated = currentChunk
			}

			offset += backward
			if backward < s.bufSize {
				break Scanning
			}
		}
	}
	return
}

func (s *Scanner) read(reader io.Reader, buf []byte) (n int) {
	if len(buf) == 0 {
		return
	}
	var err error
	var l = len(buf)
	for n < l && err == nil {
		var nn int
		nn, err = reader.Read(buf[n:])
		n += nn
	}
	return
}
