package memscan

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getconf IOV_MAX
const IOV_MAX = 1024

func processVmReadv(pid int, local, remote []unix.Iovec) (int, error) {
	lIovCnt := len(local)
	rIovCnt := len(remote)

	if lIovCnt == 0 || rIovCnt == 0 {
		return 0, nil
	}

	if lIovCnt > IOV_MAX || rIovCnt > IOV_MAX {
		return 0, unix.EINVAL
	}

	n, _, errno := unix.Syscall6(
		unix.SYS_PROCESS_VM_READV,
		uintptr(pid),
		uintptr(unsafe.Pointer(&local[0])),
		uintptr(lIovCnt),
		uintptr(unsafe.Pointer(&remote[0])),
		uintptr(rIovCnt),
		0,
	)

	runtime.KeepAlive(local)
	runtime.KeepAlive(remote)

	if errno != 0 {
		return int(n), errno
	}
	return int(n), nil
}

func processVmWritev(pid int, local, remote []unix.Iovec) (int, error) {
	lIovCnt := len(local)
	rIovCnt := len(remote)

	if lIovCnt == 0 || rIovCnt == 0 {
		return 0, nil
	}

	if lIovCnt > IOV_MAX || rIovCnt > IOV_MAX {
		return 0, unix.EINVAL
	}

	n, _, errno := unix.Syscall6(
		unix.SYS_PROCESS_VM_WRITEV,
		uintptr(pid),
		uintptr(unsafe.Pointer(&local[0])),
		uintptr(lIovCnt),
		uintptr(unsafe.Pointer(&remote[0])),
		uintptr(rIovCnt),
		0,
	)

	runtime.KeepAlive(local)
	runtime.KeepAlive(remote)

	if errno != 0 {
		return int(n), errno
	}
	return int(n), nil
}
