//go:build linux

package session

import (
	"errors"
	"fmt"
	"syscall"
)

// pidfd syscall numbers are shared by the Linux architectures supported by
// Control Agents release builds (amd64 and arm64).
const (
	sysPIDFDSendSignal = 424
	sysPIDFDOpen       = 434
)

type linuxProcessHandle struct {
	fd int
}

func openLinuxProcessHandle(pid int) (*linuxProcessHandle, error) {
	fd, _, errno := syscall.Syscall(sysPIDFDOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		return nil, fmt.Errorf("pidfd_open process %d: %w", pid, errno)
	}
	return &linuxProcessHandle{fd: int(fd)}, nil
}

func (h *linuxProcessHandle) Signal(signal syscall.Signal) error {
	_, _, errno := syscall.Syscall6(sysPIDFDSendSignal, uintptr(h.fd), uintptr(signal), 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func (h *linuxProcessHandle) Alive() (bool, error) {
	err := h.Signal(0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

func (h *linuxProcessHandle) Close() error {
	if h == nil || h.fd < 0 {
		return nil
	}
	err := syscall.Close(h.fd)
	h.fd = -1
	return err
}
