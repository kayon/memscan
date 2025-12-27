package deck

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"memscan/utils"
)

type ProcessState byte

func (state ProcessState) String() string {
	return fmt.Sprintf("%c", state)
}

func EnumDeckProcesses() ([]*Process, error) {
	proc, err := os.Open("/proc")
	if err != nil {
		return nil, err
	}
	defer proc.Close()

	var (
		names   []string
		process *Process
		results = make([]*Process, 0, 50)
	)

	for {
		names, err = proc.Readdirnames(10)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if name[0] < '0' || name[0] > '9' {
				continue
			}
			uid := GetPathUID(fmt.Sprintf("/proc/%s", name))
			// search only the processes of user "deck"
			if uid == DeckUID {
				if process = newProcess(name); process != nil {
					results = append(results, process)
				}
			}
		}
	}
	return results, nil
}

type Process struct {
	PID int
	// the PID of the parent of this process
	PPID int
	// the process group ID of the process
	PGRP int
	// the filename of the executable
	Comm string
	// complete command line for the process
	Command string
	// One of the following characters, indicating process
	// R  Running
	// S  Sleeping in an interruptible wait
	// D  Waiting in uninterruptible disk sleep
	// Z  Zombie
	// T  Stopped (on a signal) or (before Linux 2.6.33) trace stopped
	// t  Tracing stop (Linux 2.6.33 onward)
	// W  Paging (only before Linux 2.6.0)
	// X  Dead (from Linux 2.6.0 onward)
	// x  Dead (Linux 2.6.33 to 3.13 only)
	// K  Wakekill (Linux 2.6.33 to 3.13 only)
	// W  Waking (Linux 2.6.33 to 3.13 only)
	// P  Parked (Linux 3.9 to 3.13 only)
	State ProcessState

	RSS uint64
}

// Refresh read and parse /proc/pid/stat
func (proc *Process) Refresh() error {
	buf, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", proc.PID))
	if err != nil {
		return err
	}

	var (
		l = bytes.IndexByte(buf, '(')
		r = bytes.LastIndexByte(buf, ')')
	)

	if l < 0 || r <= l {
		return fmt.Errorf("unable to extract Comm in %q", buf)
	}
	proc.Comm = utils.BytesToString(buf[l+1 : r])

	if _, err = fmt.Sscanf(utils.BytesToString(buf[r+2:]), "%c %d %d",
		&proc.State,
		&proc.PPID,
		&proc.PGRP,
	); err == nil {
		statParts := bytes.Fields(buf)
		if len(statParts) >= 24 {
			proc.RSS, _ = strconv.ParseUint(string(statParts[23]), 10, 64)
		}

		buf, err = os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", proc.PID))
		if err == nil {
			// may be truncated. The kernel truncates it to 15 characters
			if len(proc.Comm) == 15 {
				p := buf[:bytes.IndexByte(buf, 0x0)]
				n := bytes.LastIndexByte(p, '/')
				if n < 0 {
					n = bytes.LastIndexByte(p, '\\')
				}
				if n > -1 && n < len(p) {
					p = p[n+1:]
				}
				proc.Comm = utils.BytesToString(p)
			}
			// replace 0x0 to space
			for i := range buf {
				if buf[i] == 0x0 {
					buf[i] = ' '
				}
			}
			proc.Command = utils.BytesToString(bytes.TrimSpace(buf))
		}

		if _, ok := procNameInvalid[proc.Comm]; ok {
			proc.Comm = proc.Command
			n := strings.LastIndexByte(proc.Comm, '/')
			if n < 0 {
				n = strings.LastIndexByte(proc.Comm, '\\')
			}
			if n > -1 {
				proc.Comm = proc.Comm[n+1:]
				n = strings.Index(proc.Comm, " ")
				if n > -1 {
					proc.Comm = proc.Comm[:n]
				}
			}
		}
	}
	return err
}

func (proc *Process) Alive() bool {
	return ProcessExists(proc.PID)
}

func (proc *Process) Pause() error {
	return ProcessPause(proc.PID)
}

func (proc *Process) Resume() error {
	return ProcessResume(proc.PID)
}

func (proc *Process) hasFileDescriptor(target string) bool {
	fdPath := fmt.Sprintf("/proc/%d/fd", proc.PID)
	fds, err := os.ReadDir(fdPath)
	if err != nil {
		return false
	}
	for _, fd := range fds {
		dest, _ := os.Readlink(filepath.Join(fdPath, fd.Name()))
		if strings.Contains(dest, target) {
			return true
		}
	}
	return false
}

func (proc *Process) hasEnvVariable(name string) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", proc.PID))
	if err != nil {
		return false
	}
	return strings.Contains(utils.BytesToString(data), name)
}

func (proc *Process) GetIdentityScore() int {
	score := 0

	if proc.hasFileDescriptor("renderD128") {
		score += 200
	}

	if proc.RSS > 262144 {
		score += 100
	} else if proc.RSS > 51200 {
		score += 50
	}

	if proc.hasEnvVariable("STEAM_COMPAT_DATA_PATH") {
		score += 80
	}

	cmd := strings.ToLower(proc.Command)
	if strings.Contains(cmd, "steamapps/common") {
		score += 60
	}
	if strings.Contains(cmd, ".exe") {
		score += 40
	}

	if strings.Contains(cmd, "steamwebhelper") || strings.Contains(cmd, "steam.sh") {
		score -= 500
	}

	return score
}

func newProcess(pidStr string) *Process {
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return nil
	}
	if pid == selfPID {
		return nil
	}
	process, err := NewProcess(pid)
	if err != nil {
		return nil
	}
	return process
}

func NewProcess(pid int) (*Process, error) {
	process := &Process{PID: pid}
	return process, process.Refresh()
}
