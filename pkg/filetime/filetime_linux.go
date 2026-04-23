//go:build linux
// +build linux

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
	return time.Unix(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec)), true, nil
}
