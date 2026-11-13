package config

import (
	"errors"
	"regexp"
	"strings"
)

var basePathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// NormalizeBasePath возвращает пустой префикс для корня и запрещает неоднозначные URL.
func NormalizeBasePath(value string) (string, error) {
	if value == "//" {
		return "", errors.New("base_path: пустой сегмент запрещён")
	}
	value = strings.TrimSuffix(value, "/")
	if value == "" {
		return "", nil
	}
	if !basePathPattern.MatchString(value) {
		return "", errors.New("base_path: ожидается абсолютный путь из непустых сегментов без query/fragment")
	}
	for _, segment := range strings.Split(value, "/")[1:] {
		if segment == "." || segment == ".." {
			return "", errors.New("base_path: сегменты . и .. запрещены")
		}
	}
	return value, nil
}
