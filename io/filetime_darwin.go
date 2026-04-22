//go:build darwin
// +build darwin

package util

import (
	"os"
	"syscall"
	"time"
)

func creationTime(path string, fi os.FileInfo) (time.Time, bool, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return time.Time{}, false, nil
	}

	// Birth time (creation time) on macOS
	t := time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	return t, true, nil
}
