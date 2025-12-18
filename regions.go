package memscan

type Regions []Region

func (regions *Regions) reset() {
	clear(*regions)
	*regions = (*regions)[:0]
}

func (regions *Regions) Size() (size uint64) {
	for _, region := range *regions {
		size += region.Size
	}
	return
}

// Optimize regions
// Merge consecutive small region and split large region
// 合并连续的小块 Size <= regionSmallSize, 拆分大块 Size > regionLargeSize
// 这是性能提升的关键
func (regions *Regions) Optimize() {
	n := len(*regions)
	var next *Region
	for i := 0; i < n; {
		curr := &(*regions)[i]
		if i+1 < n {
			next = &(*regions)[i+1]
		} else {
			next = nil
		}
		if next != nil && curr.Size <= regionSmallSize {
			if next.Start == curr.End && next.Size <= regionSmallSize {
				curr.End = next.End
				curr.Size += next.Size
				n -= 1
				*regions = append((*regions)[:i+1], (*regions)[i+2:]...)
			} else {
				i++
			}
		} else if curr.Size > regionLargeSize && curr.Size/regionLargeSize > 1 {
			chunks := curr.split()
			after := make(Regions, n-i-1)
			// remove current item
			copy(after, (*regions)[i+1:])
			*regions = append((*regions)[:i], chunks...)
			*regions = append(*regions, after...)
			i += len(chunks) + 1
			n += len(chunks) - 1
		} else {
			i++
		}
	}
}
