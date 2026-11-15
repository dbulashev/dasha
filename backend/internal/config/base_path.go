package config

import (
	"errors"
	"regexp"
	"strings"
)

var basePathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// NormalizeBasePath validates the URL prefix and returns an empty string for root.
func NormalizeBasePath(value string) (string, error) {
	value = strings.TrimSuffix(value, "/")
	if value == "" {
		return "", nil
	}
	if !basePathPattern.MatchString(value) {
		return "", errors.New("base_path: expected an absolute path with non-empty segments and no query or fragment")
	}
	segments := strings.Split(value, "/")[1:]
	switch segments[0] {
	case "api", "auth", "mcp":
		return "", errors.New("base_path: first segment must not be api, auth or mcp")
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." {
			return "", errors.New("base_path: . and .. segments are not allowed")
		}
	}
	return value, nil
}
