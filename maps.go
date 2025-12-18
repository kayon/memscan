package memscan

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	"memscan/utils"
)

type Maps struct {
	file *os.File
}

func (m *Maps) Close() error {
	return m.file.Close()
}

func (m *Maps) Parse() (regions Regions) {
	_, _ = m.file.Seek(0, io.SeekStart)
	bufScan := bufio.NewScanner(m.file)
	regions = make(Regions, 0, defRegionsCaps)
	var row []byte
	for bufScan.Scan() {
		row = bufScan.Bytes()
		if region := parseMapsRow(row); region != nil {
			regions = append(regions, *region)
		}
	}
	return
}

func openMaps(pid int) (*Maps, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	return &Maps{file: f}, nil
}

func parseMapsRow(raw []byte) *Region {
	i := bytes.IndexByte(raw, ' ')
	if i < 0 || i+4 >= len(raw) {
		return nil
	}
	// readable and writable and not executable
	// rw-p
	if raw[i+1] != 'r' || raw[i+2] != 'w' || raw[i+3] == 'x' || raw[i+4] != 'p' {
		return nil
	}

	dashIdx := bytes.IndexByte(raw[:i], '-')
	if dashIdx == -1 {
		return nil
	}

	start, _ := strconv.ParseUint(utils.BytesToString(raw[:dashIdx]), 16, 64)
	end, _ := strconv.ParseUint(utils.BytesToString(raw[dashIdx+1:i]), 16, 64)

	region := &Region{
		Start: start,
		End:   end,
		Size:  end - start,
	}
	return region
}
