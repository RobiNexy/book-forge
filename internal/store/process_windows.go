//go:build windows

package store

import "syscall"

// processAlive queries the Windows process handle without sending it a signal.
func processAlive(pid int) (bool, error) {
	handle, err := syscall.OpenProcess(0x00100000, false, uint32(pid)) // SYNCHRONIZE
	if err != nil {
		if err == syscall.ERROR_ACCESS_DENIED {
			return true, nil
		}
		return false, nil
	}
	defer syscall.CloseHandle(handle)
	state, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	return state == syscall.WAIT_TIMEOUT, nil
}
