//go:build !windows

package store

import (
	"errors"
	"syscall"
)

// processAlive probes a PID without signaling it on Unix-like systems.
func processAlive(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}
