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

package memscan

type Regions []Region

func (regions *Regions) Size() (size uint64) {
	for _, region := range *regions {
		size += region.Size
	}
	return
}

func RegionsOptimize(regions []Region) (optimized []Region) {
	n := len(regions)
	if n == 0 {
		return
	}
	n = n + (n >> 3)
	optimized = make([]Region, 0, n)

	for i := 0; i < len(regions); {
		curr := regions[i]

		// 合并连续小块
		for i+1 < len(regions) {
			next := regions[i+1]
			if curr.End == next.Start &&
				curr.Size <= regionSmallSize &&
				next.Size <= regionSmallSize {

				curr.End = next.End
				curr.Size = curr.End - curr.Start
				i++

				if curr.Size >= regionLargeSize {
					optimized = append(optimized, curr)
					i++
					goto nextLoop
				}
			} else {
				break
			}
		}

		// 拆分超大块
		if curr.Size > regionLargeSize {
			currentStart := curr.Start
			for currentStart < curr.End {
				// 预设结束位置
				nextEnd := currentStart + regionLargeSize

				// 对齐到 4096 页面边界
				// 这样拆分点永远在页面边缘，不会切断页内数据
				if nextEnd < curr.End {
					nextEnd = nextEnd & ^uint64(4095)
					// 如果对齐后导致 nextEnd 回退到了 currentStart，
					// 说明 regionLargeSize 太小，强制加一个 PageSize
					if nextEnd <= currentStart {
						nextEnd = currentStart + 4096
					}
				} else {
					nextEnd = curr.End
				}

				optimized = append(optimized, Region{
					Start: currentStart,
					End:   nextEnd,
					Size:  nextEnd - currentStart,
					// 其它属性已不再重要
				})
				currentStart = nextEnd
			}
		} else {
			optimized = append(optimized, curr)
		}

		i++
	nextLoop:
	}
	return
}
