package filetime

import (
	"fmt"
	"os"
	"strings"
)

// Formatted returns a formatted timestamp for the file at path.
//
// kind:
//   - "modified" (aka "mtime") : available on all platforms (os.Stat().ModTime())
//   - "created"  (aka "btime") : supported on Windows and platform-specific where available
func Formatted(path, kind, layout string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	switch strings.ToLower(kind) {
	case "modified", "mod", "mtime":
		return fi.ModTime().Format(layout), nil
	case "created", "create", "birth", "btime":
		t, ok, err := creationTime(path, fi)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("creation time not available for %q on this platform/filesystem", path)
		}
		return t.Format(layout), nil
	default:
		return "", fmt.Errorf("unknown kind %q (use %q or %q)", kind, "modified", "created")
	}
}
