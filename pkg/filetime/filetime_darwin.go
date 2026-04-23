//go:build darwin
// +build darwin

package filetime

import (
	"os"
	"syscall"
	"time"
)

func creationTime(path string, fi os.FileInfo) (time.Time, bool, error) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false, nil
	}
	return time.Unix(int64(stat.Birthtimespec.Sec), int64(stat.Birthtimespec.Nsec)), true, nil
}
