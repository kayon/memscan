package deck

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// DeckUID from username "deck"
const DeckUID = 1000

//const ReaperPathPrefix = "/home/deck/.local/share/Steam/"

type GameProcess struct {
	Name       string
	InstanceID int
	PID        int
}

var selfPID int

func init() {
	selfPID = os.Getpid()
}

var gameProcessExclude = []*regexp.Regexp{
	regexp.MustCompile(`^/usr/`),
	regexp.MustCompile(`^Xwayland`),
	//regexp.MustCompile(`home[/\\]deck[/\\]\.local[/\\]`),
	regexp.MustCompile(`^\\\\`),
	regexp.MustCompile(`^[cC]:`),
	regexp.MustCompile(`/home/deck/homebrew/plugins/`),
	regexp.MustCompile(`/home/deck/\.local/share/Steam/ubuntu`),
	regexp.MustCompile(`/home/deck/\.local/share/Steam/steamapps/common/(GE-)?Proton`),
	regexp.MustCompile(`/home/deck/\.local/share/Steam/compatibilitytools\.d/`),
	regexp.MustCompile(`/home/deck/\.local/share/Steam/steamapps/common/Steam`),
}

var procNameInvalid = map[string]bool{
	"Main":       true,
	"MainThread": true,
}

func findSteamLaunchGameProcess(processes []*Process, reaperPID int) *Process {
	wrapIndex := slices.IndexFunc(processes, func(proc *Process) bool {
		return proc.PPID == reaperPID
	})

	if wrapIndex == -1 {
		return nil
	}
	wrap := processes[wrapIndex]

	adverbIndex := slices.IndexFunc(processes[wrapIndex:], func(proc *Process) bool {
		return proc.PPID == wrap.PID
	})
	if adverbIndex == -1 {
		return nil
	}
	adverbIndex += wrapIndex
	adverb := processes[adverbIndex]

	var children = make([]*Process, 0, 32)

	for _, proc := range processes[adverbIndex:] {
		if proc.PPID == adverb.PID {
			children = append(children, proc)
		}
	}

	if len(children) == 0 {
		return nil
	}

	slices.SortFunc(children, func(a, b *Process) int {
		return int(b.RSS - a.RSS)
	})

	return children[0]
}

func FindGameWithAppID(appID int64) *Process {
	processes, err := EnumDeckProcesses()
	if err != nil {
		return nil
	}
	slices.SortFunc(processes, func(a, b *Process) int {
		return a.PID - b.PID
	})

	var pattern = fmt.Sprintf("SteamLaunch AppId=%d", appID)

	reaperIndex := slices.IndexFunc(processes, func(proc *Process) bool {
		return strings.Contains(proc.Command, pattern)
	})

	if reaperIndex == -1 {
		return nil
	}
	reaperPID := processes[reaperIndex].PID
	return findSteamLaunchGameProcess(processes[reaperIndex:], reaperPID)
}

func FindGameWithInstanceID(instanceID int) *Process {
	processes, err := EnumDeckProcesses()
	if err != nil {
		return nil
	}
	slices.SortFunc(processes, func(a, b *Process) int {
		return a.PID - b.PID
	})
	return findSteamLaunchGameProcess(processes, instanceID)
}

func EnumGameProcesses() []*Process {
	processes, err := EnumDeckProcesses()
	if err != nil {
		return nil
	}
	slices.SortFunc(processes, func(a, b *Process) int {
		return a.PID - b.PID
	})

	reaperIndex := slices.IndexFunc(processes, func(proc *Process) bool {
		return strings.Contains(proc.Command, `SteamLaunch AppId=`)
	})

	if reaperIndex == -1 {
		return nil
	}
	reaperPID := processes[reaperIndex].PID
	proc := findSteamLaunchGameProcess(processes[reaperIndex:], reaperPID)
	if proc == nil {
		return nil
	}
	return []*Process{proc}
}

// LegacyEnumGameProcesses Deprecated
func LegacyEnumGameProcesses() []*Process {
	processes, err := EnumDeckProcesses()
	if err != nil {
		return nil
	}

	var candidates []*Process
	for _, p := range processes {
		if p.GetIdentityScore() > 0 {
			var excluded bool
			for _, reg := range gameProcessExclude {
				if reg.MatchString(p.Command) {
					excluded = true
					break
				}
			}
			if !excluded {
				candidates = append(candidates, p)
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		scoreI := candidates[i].GetIdentityScore()
		scoreJ := candidates[j].GetIdentityScore()

		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		return candidates[i].RSS > candidates[j].RSS
	})

	return candidates
}
