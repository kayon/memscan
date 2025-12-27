//go:build darwin

package memscan

import "golang.org/x/sys/unix"

const IOV_MAX = 1024

func processVmReadv(pid int, local, remote []unix.Iovec) (int, error) {
	return 0, unix.EFAULT
}

func processVmWritev(pid int, local, remote []unix.Iovec) (int, error) {
	return 0, unix.EFAULT
}
