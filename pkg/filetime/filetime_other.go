//go:build !windows && !linux && !darwin
// +build !windows,!linux,!darwin

package filetime

import (
	"os"
	"time"
)

func creationTime(path string, fi os.FileInfo) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
