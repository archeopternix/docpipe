//go:build linux
// +build linux

package util

import (
	"os"
	"time"
)

func creationTime(path string, fi os.FileInfo) (time.Time, bool, error) {
	// Linux: creation time (btime) isn't reliably exposed via Go's stat.
	return time.Time{}, false, nil
}
