package pathutil

import (
	"fmt"
	"path"
	"strings"
)

func CleanDocID(id string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if value == "" {
		return "", fmt.Errorf("document id is empty")
	}
	if strings.Contains(value, ":") {
		return "", fmt.Errorf("invalid document id %q", id)
	}
	value = path.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "/") || strings.Contains(value, "/") {
		return "", fmt.Errorf("invalid document id %q", id)
	}
	return value, nil
}

func CleanName(name string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if value == "" {
		return "", fmt.Errorf("document path is empty")
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("invalid document path %q", name)
	}
	value = path.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") {
		return "", fmt.Errorf("invalid document path %q", name)
	}
	return value, nil
}

func CleanDir(name string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if value == "" {
		return ".", nil
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("invalid document directory %q", name)
	}
	value = path.Clean(value)
	if value == "." {
		return ".", nil
	}
	if value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") {
		return "", fmt.Errorf("invalid document directory %q", name)
	}
	return value, nil
}
