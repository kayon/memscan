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

import (
	"io"
	"slices"
)

// VirtualRegion represents a consolidated memory range derived from scan results.
// It merges multiple discrete result addresses into a single contiguous block to optimize batch I/O operations.
// Note that this range may include padding between discrete addresses,
// containing unmapped or protected memory pages.
type VirtualRegion struct {
	Start     uint64
	End       uint64
	Size      uint64
	addresses []uint64
	count     int
	cursor    int
}

func (region *VirtualRegion) Pipe(pid int) io.ReadSeekCloser {
	return getRegionReader(pid, region.Start, region.End)
}

func (region *VirtualRegion) Match(address uint64) bool {
	if region.cursor >= region.count {
		return false
	}
	if region.addresses[region.cursor] == address {
		region.cursor++
		return true
	}
	if region.addresses[region.cursor] < address {
		i, found := slices.BinarySearch(region.addresses[region.cursor:], address)
		region.cursor += i
		if found {
			region.cursor++
			return true
		}
	}

	return false
}

func (region *VirtualRegion) IsFinished() bool {
	return region.cursor >= region.count
}

// BuildVirtualRegions groups discrete result addresses into contiguous memory spans.
// Note that this may span across multiple distinct physical pages.
func BuildVirtualRegions(addresses []uint64, valueSize uint64) []*VirtualRegion {
	if len(addresses) == 0 {
		return nil
	}

	var regions []*VirtualRegion

	currentStartIdx := 0
	// Start: Aligned down to the nearest 4KB page boundary
	regionStartAddr := addresses[0] &^ 0xFFF

	for i := 1; i < len(addresses); i++ {
		addr := addresses[i]
		if (addr+valueSize)-regionStartAddr > regionLargeSize {
			region := &VirtualRegion{
				Start: regionStartAddr,
				// End: Aligned up to the nearest 4KB page boundary
				End:       (addresses[i-1] + valueSize + 0xFFF) &^ 0xFFF,
				addresses: addresses[currentStartIdx:i],
			}
			region.Size = region.End - region.Start
			region.count = len(region.addresses)

			regions = append(regions, region)

			currentStartIdx = i
			regionStartAddr = addr &^ 0xFFF
		}
	}

	region := &VirtualRegion{
		Start:     regionStartAddr,
		End:       (addresses[len(addresses)-1] + valueSize + 0xFFF) &^ 0xFFF,
		addresses: addresses[currentStartIdx:],
	}
	region.Size = region.End - region.Start
	region.count = len(region.addresses)
	regions = append(regions, region)

	return regions
}
