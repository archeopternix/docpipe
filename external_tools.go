package docpipe

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultExternalToolTimeout     = time.Minute
	defaultScreenshotToolTimeout   = 2 * time.Minute
	defaultMaxZipEntryReadBytes    = int64(512 << 20)
	defaultCommandErrorSnippetSize = 4096
)

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contextWithToolTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, time.Duration) {
	ctx = contextOrBackground(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return ctx, func() {}, time.Until(deadline)
	}
	child, cancel := context.WithTimeout(ctx, timeout)
	return child, cancel, timeout
}

func requiredTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %w: %s not found in PATH", ErrUnsupported, ErrToolMissing, name)
	}
	return path, nil
}

func firstAvailableTool(names ...string) (string, error) {
	var missing []string
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
		missing = append(missing, name)
	}
	return "", fmt.Errorf("%w: %w: none of %s found in PATH", ErrUnsupported, ErrToolMissing, strings.Join(missing, ", "))
}

func commandRunError(ctx context.Context, tool string, timeout time.Duration, err error, stderr []byte) error {
	if contextErr := ctx.Err(); errors.Is(contextErr, context.DeadlineExceeded) {
		if timeout > 0 {
			return fmt.Errorf("%w: %s timed out after %s", ErrTimeout, tool, timeout.Round(time.Second))
		}
		return fmt.Errorf("%w: %s timed out", ErrTimeout, tool)
	} else if errors.Is(contextErr, context.Canceled) {
		return fmt.Errorf("%w: %s canceled", context.Canceled, tool)
	}

	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return fmt.Errorf("%s failed: %w", tool, err)
	}
	return fmt.Errorf("%s failed: %w: %s", tool, err, message)
}

func responseSnippet(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) <= defaultCommandErrorSnippetSize {
		return text
	}
	return text[:defaultCommandErrorSnippetSize] + "..."
}
