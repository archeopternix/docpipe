//go:build windows
// +build windows

package io

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

func creationTime(path string, _ os.FileInfo) (time.Time, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return time.Time{}, false, err
	}

	var data syscall.Win32FileAttributeData
	if err := syscall.GetFileAttributesEx(p, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&data))); err != nil {
		return time.Time{}, false, err
	}

	// Filetime -> time.Time
	t := time.Unix(0, data.CreationTime.Nanoseconds())
	return t, true, nil
}
