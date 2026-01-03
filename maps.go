package memscan

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type RegionScanLevel uint8

const (
	REGION_ALL RegionScanLevel = iota
	REGION_ALL_RW
	REGION_HEAP_STACK_EXECUTABLE
	REGION_HEAP_STACK_EXECUTABLE_BSS
)

var regionSkipped = []string{
	"/usr/share/",        // 字体、多语言数据
	"/usr/lib/",          // 系统库
	"/run/host/usr/lib/", // 系统库
	"/usr/lib/x86_64-linux-gnu/",
	"/dev/dri", // 硬件驱动映射
	"[vvar]",   // 内核同步数据
	"[vdso]",   // 系统调用加速
	"/home/deck/.local/share/Steam/ubuntu12_64/",
}

func isSkipRegion(filename string) bool {
	// 匿名映射
	if filename == "" {
		return false
	}
	for _, b := range regionSkipped {
		if strings.HasPrefix(filename, b) {
			return true
		}
	}
	return false
}

func OpenMaps(pid int) (*Maps, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	exePath, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return &Maps{pid: pid, exe: exePath, file: f}, nil
}

type Maps struct {
	pid  int
	exe  string
	file *os.File
}

func (m *Maps) Close() error {
	return m.file.Close()
}

func (m *Maps) Parse(args ...RegionScanLevel) (regions Regions) {
	var scanLevel = REGION_ALL
	if len(args) > 0 && args[0] <= REGION_HEAP_STACK_EXECUTABLE_BSS {
		scanLevel = args[0]
	}

	_, _ = m.file.Seek(0, io.SeekStart)
	scan := bufio.NewScanner(m.file)
	regions = make(Regions, 0, defRegionsCaps)

	var (
		codeRegions uint = 0
		exeRegions  uint = 0
		prevEnd     uint64
		loadAddr    uint64
		exeLoad     uint64
		isExe       bool
		binName     string
	)

	for scan.Scan() {
		line := scan.Bytes()
		r := ParseRegion(line)
		if r == nil {
			continue
		}

		start, end, filename := r.Start, r.End, r.Filename
		perms := r.Perm

		if codeRegions > 0 {
			// 检查是否脱离了当前 ELF 映射序列
			if perms.Exec() || (filename != binName && (filename != "" || start != prevEnd)) || codeRegions >= 4 {
				codeRegions = 0
				isExe = false
				if exeRegions > 1 {
					exeRegions = 0
				}
			} else {
				codeRegions++
				if isExe {
					exeRegions++
				}
			}
		}

		if codeRegions == 0 {
			// 寻找 ELF 的起始段 (通常是 .text)
			if perms.Exec() && filename != "" {
				codeRegions++
				if filename == m.exe {
					exeRegions = 1
					exeLoad = start
					isExe = true
				}
				binName = filename
			} else if exeRegions == 1 && filename != "" && filename == m.exe {
				exeRegions++
				codeRegions = exeRegions
				exeLoad = start // 更新起始基址
				isExe = true
				binName = filename
			}

			if exeRegions < 2 {
				loadAddr = start
			} else {
				loadAddr = exeLoad
			}
		}
		prevEnd = end

		if !perms.Read() || r.Size <= 0 {
			continue
		}

		regionType := REGION_TYPE_MISC
		if isExe {
			regionType = REGION_TYPE_EXE
		} else if codeRegions > 0 {
			regionType = REGION_TYPE_CODE
		} else if filename == "[heap]" {
			regionType = REGION_TYPE_HEAP
		} else if filename == "[stack]" {
			regionType = REGION_TYPE_STACK
		}

		useful := false

		// 如果不是 REGION_ALL, 通常只关注可写内存 rw--
		if scanLevel != REGION_ALL && !perms.Write() {
			continue
		}

		switch scanLevel {
		case REGION_ALL, REGION_ALL_RW:
			useful = true
		case REGION_HEAP_STACK_EXECUTABLE_BSS:
			// 匿名映射通常是 BSS
			if filename == "" {
				useful = true
				break
			}
			fallthrough
		case REGION_HEAP_STACK_EXECUTABLE:
			if regionType == REGION_TYPE_HEAP || regionType == REGION_TYPE_STACK {
				useful = true
			} else if regionType == REGION_TYPE_EXE || filename == m.exe {
				useful = true
			}
		}

		if useful {
			if isSkipRegion(filename) {
				continue
			}
			if r.Perm.Shared() && filename != m.exe {
				continue
			}

			r.Type = regionType
			r.BaseAddr = loadAddr
			regions = append(regions, *r)
		}
	}
	return
}
