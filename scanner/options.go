package scanner

import "io"

type Options struct {
	// 当ExpectedSize > 0时, 如果读取发生意外(通常是 bad address)
	// 应用 AlignNextPage 对齐并翻页
	// 这通常在 NextScan 场景, io.Reader 是 VirtualRegion 时
	ExpectedSize uint64
}

func (options *Options) alignNextPage(seeker io.Seeker, offset uint64) (nextOffset uint64, ok bool) {
	if options.ExpectedSize == 0 || seeker == nil {
		return
	}

	nextOffset = (offset + 0xFFF) &^ 0xFFF
	// 确保步进到下一页
	if nextOffset <= offset {
		nextOffset += 0x1000
	}
	if nextOffset >= options.ExpectedSize {
		return 0, false
	}
	if _, err := seeker.Seek(int64(nextOffset), io.SeekStart); err != nil {
		return 0, false
	}
	return nextOffset, true
}
