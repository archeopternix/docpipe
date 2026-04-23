//go:build windows
// +build windows

package filetime

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

func creationTime(path string, fi os.FileInfo) (time.Time, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return time.Time{}, false, err
	}

	var data syscall.Win32FileAttributeData
	if err := syscall.GetFileAttributesEx(p, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&data))); err != nil {
		return time.Time{}, false, err
	}

	return time.Unix(0, data.CreationTime.Nanoseconds()), true, nil
}
