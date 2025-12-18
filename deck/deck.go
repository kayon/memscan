package deck

import (
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// DeckUID from username "deck"
const DeckUID = 1000

const ReaperPathPrefix = "/home/deck/.local/share/Steam/"

var selfPID int

func init() {
	selfPID = os.Getpid()
}

var gameProcessExclude = []*regexp.Regexp{
	regexp.MustCompile(`^/usr/lib/`),
	regexp.MustCompile(`\s+/home/deck/\.local/`),
	regexp.MustCompile(`^[cC]:`),
	regexp.MustCompile(`^/home/deck/\.local/share/Steam/steamapps/common/(GE-)?Proton`),
	regexp.MustCompile(`^/home/deck/\.local/share/Steam/compatibilitytools\.d/`),
	regexp.MustCompile(`^/home/deck/\.local/share/Steam/steamapps/common/Steam`),
}

var procNameInvalid = map[string]bool{
	"Main":       true,
	"MainThread": true,
}

func EnumGameProcesses() []*Process {
	processes, err := EnumDeckProcesses()
	if err != nil {
		return nil
	}
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].PID > processes[j].PID
	})

	var (
		results = make([]*Process, 0, 20)
		// PID
		indexes = make(map[int]struct{}, len(processes))
	)

	for _, process := range processes {
		if len(indexes) == 0 {
			if process.Comm != "reaper" || strings.Index(process.Command, ReaperPathPrefix) != 0 {
				continue
			}
		} else if _, ok := indexes[process.PPID]; !ok {
			continue
		}
		indexes[process.PID] = struct{}{}
		results = append(results, process)
	}

	if len(results) > 0 {
		results = results[1:]
	}

	results = slices.DeleteFunc(results, func(p *Process) bool {
		for _, reg := range gameProcessExclude {
			if reg.MatchString(p.Command) {
				return true
			}
		}
		return false
	})

	return results
}
