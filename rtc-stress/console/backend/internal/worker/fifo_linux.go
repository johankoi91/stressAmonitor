//go:build linux

package worker

import (
	"os"
	"syscall"
	"unsafe"
)

// FIONREAD also works on the FIFO write end; it counts only bytes still in the
// kernel pipe, excluding libc read-ahead and all SDK queues.
func fifoBuffer(file *os.File) (buffered, capacity *int, problem string) {
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, nil, err.Error()
	}
	var operationErr syscall.Errno
	err = raw.Control(func(fd uintptr) {
		size, _, code := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETPIPE_SZ, 0)
		if code == 0 {
			n := int(size)
			capacity = &n
		} else {
			operationErr = code
		}
		var pending int32
		_, _, code = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCINQ, uintptr(unsafe.Pointer(&pending)))
		if code == 0 {
			n := int(pending)
			buffered = &n
		} else {
			operationErr = code
		}
	})
	if err != nil {
		return buffered, capacity, err.Error()
	}
	if operationErr != 0 {
		problem = operationErr.Error()
	}
	return
}
