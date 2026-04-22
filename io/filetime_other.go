//go:build !windows && !darwin && !linux
// +build !windows,!darwin,!linux

package io

import (
	"os"
	"time"
)

func creationTime(_ string, _ os.FileInfo) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
