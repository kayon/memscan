// Copyright (C) 2025 kayon <kayon.hu@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package scanner

import (
	"bytes"
	"context"
	"io"
)

const (
	defScanBufferSize = 1 << 16
)

type Scanner struct {
	ctx     context.Context
	value   Value
	bufSize int
	rf32    *RoundedFloat32
	rf64    *RoundedFloat64
}

func NewScanner(ctx context.Context, value Value) *Scanner {
	return NewScannerBuffer(ctx, value, defScanBufferSize)
}

func NewScannerBuffer(ctx context.Context, value Value, bufSize int) *Scanner {
	if bufSize < value.Size() {
		bufSize = value.Size() + 1
	}

	scan := &Scanner{
		ctx:   ctx,
		value: value,
	}

	if value.HasOption() {
		switch value.Type() {
		case Float32:
			scan.rf32 = newRoundedFloat32(value)
			if bufSize < vectorFloat32Size {
				bufSize = vectorFloat32Size
			} else if bufSize%vectorFloat32Size != 0 {
				bufSize = ((bufSize + vectorFloat32Size - 1) / vectorFloat32Size) * vectorFloat32Size
			}
		case Float64:
			scan.rf64 = newRoundedFloat64(value)
			if bufSize < vectorFloat64Size {
				bufSize = vectorFloat64Size
			} else if bufSize%vectorFloat64Size != 0 {
				bufSize = ((bufSize + vectorFloat64Size - 1) / vectorFloat64Size) * vectorFloat64Size
			}
		}
	} else {
		if bufSize < value.Size() {
			bufSize = value.Size()
		} else if bufSize%4 != 0 {
			bufSize = ((bufSize + 3) / 4) * 4
		}
	}

	scan.bufSize = bufSize

	return scan
}

func (s *Scanner) ScanCollector(reader io.Reader, collector Collector) {
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	if collector == nil {
		return
	}
	if s.rf32 != nil {
		scanFloat32Rounded(s.ctx, reader, s.bufSize, s.rf32.min, s.rf32.max, collector)
	} else if s.rf64 != nil {
		scanFloat64Rounded(s.ctx, reader, s.bufSize, s.rf64.min, s.rf64.max, collector)
	} else {
		s.scanBytes(reader, collector)
	}
}

func (s *Scanner) Scan(reader io.Reader) []int {
	var collector = NewSliceCollector(1024)
	s.ScanCollector(reader, collector)
	return collector.Results
}

func (s *Scanner) scanBytes(reader io.Reader, collector Collector) {
	var (
		size = s.value.Size()
		// 每次搜索的块, 并为truncated预留了容量 size, 因为 truncated 只会小于 Value.Size
		chunk = make([]byte, s.bufSize+size)
		// 上一次 chunk 末尾可能截断的数据, 在下一轮追加到 chunk 头部
		truncated         = make([]byte, size-1)
		truncLen          = 0
		forward, backward int
		offset            int
		isAligned         = s.value.Aligned()
		pattern           = s.value.data
		err               error
	)

	for {
		if s.ctx.Err() != nil {
			return
		}

		// 将上次截断的数据移到开头
		if truncLen > 0 {
			forward = copy(chunk, truncated[:truncLen])
		}
		// 填充剩余空间
		backward, err = io.ReadFull(reader, chunk[forward:forward+s.bufSize])

		// 上次截断的 truncated + 本次读取, 不足以完成一次搜索
		if forward+backward < size {
			break
		}

		currentChunk := chunk[:forward+backward]
		chunkOffset := offset - forward

		i := 0
		for {
			n := bytes.Index(currentChunk[i:], pattern)
			if n < 0 {
				break
			}
			pos := i + n
			finalIndex := chunkOffset + pos

			if !isAligned || finalIndex%size == 0 {
				collector.Collect(finalIndex)
				i = pos + size
			} else {
				i = pos + (size - (finalIndex % size))
			}
		}

		if len(currentChunk) >= size {
			// 预留上一次可能匹配截断的 < Value.Size 大小
			truncLen = copy(truncated, currentChunk[len(currentChunk)-size+1:])
		} else {
			truncLen = copy(truncated, currentChunk)
		}

		if err != nil || backward < s.bufSize {
			break
		}

		offset += backward
	}
	return
}
