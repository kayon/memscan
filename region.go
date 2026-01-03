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
	"bytes"
	"fmt"
	"io"
	"strconv"

	"github.com/kayon/memscan/utils"
)

const (
	memPageSize    = 4096
	scanBufferSize = memPageSize << 4

	regionSmallSize = scanBufferSize
	regionLargeSize = memPageSize << 9

	defRegionsCaps = 1 << 11
)

type RegionType uint8

const (
	REGION_TYPE_MISC RegionType = iota
	REGION_TYPE_CODE
	REGION_TYPE_EXE
	REGION_TYPE_HEAP
	REGION_TYPE_STACK
)

func (r RegionType) String() string {
	switch r {
	case REGION_TYPE_MISC:
		return "MISC"
	case REGION_TYPE_CODE:
		return "CODE"
	case REGION_TYPE_EXE:
		return "EXE"
	case REGION_TYPE_HEAP:
		return "HEAP"
	case REGION_TYPE_STACK:
		return "STACK"
	}
	return "UNKNOWN"
}

type Region struct {
	Start    uint64
	End      uint64
	Size     uint64
	Type     RegionType
	Perm     Permissions
	Filename string
	BaseAddr uint64
}

func (region Region) String() string {
	return fmt.Sprintf("%08X-%08X %10d %s %-5s %08X %q", region.Start, region.End, region.Size, region.Perm, region.Type, region.BaseAddr, region.Filename)
}

func (region Region) Pipe(pid int) io.ReadSeekCloser {
	return getRegionReader(pid, region.Start, region.End)
}

func ParseRegion(line []byte) *Region {
	i := bytes.IndexByte(line, ' ')
	if i <= 0 || i+5 > len(line) {
		return nil
	}

	dashIdx := bytes.IndexByte(line, '-')
	if dashIdx <= 0 {
		return nil
	}

	region := &Region{
		Perm: ParsePermissions(line[i+1 : i+5]),
	}
	region.Start, _ = strconv.ParseUint(utils.BytesToString(line[:dashIdx]), 16, 64)
	region.End, _ = strconv.ParseUint(utils.BytesToString(line[dashIdx+1:i]), 16, 64)
	region.Size = region.End - region.Start

	line = line[i+5:]

	for i = 0; i < 3; i++ {
		line = bytes.TrimLeft(line, " \t")
		nextSpace := bytes.IndexByte(line, ' ')
		if nextSpace == -1 {
			return region
		}
		line = line[nextSpace:]
	}

	region.Filename = string(bytes.TrimLeft(line, " \t"))

	return region
}
