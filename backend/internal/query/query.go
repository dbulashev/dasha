package query

import (
	"embed"
	_ "embed"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/dbulashev/dasha/internal/enums"
)

//go:embed all:sql
var sqlFS embed.FS

type TemplateData any

func Get(serverVersion int, query enums.Query, data TemplateData) (string, error) {
	if !query.IsValid() {
		return "", enums.ErrInvalidQuery
	}

	queryName := query.String()

	tmplContent, err := findTemplate(serverVersion, queryName)
	if err != nil {
		return "", fmt.Errorf("failed to find template: %w", err)
	}

	tmpl, err := template.New(queryName).Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
}

func findTemplate(serverVersion int, queryName string) (string, error) {
	basePath := filepath.Join("sql", queryName)

	versionDirs, err := sqlFS.ReadDir(basePath)
	if err != nil {
		return "", fmt.Errorf("failed to read base path %s: %w", basePath, err)
	}

	var (
		bestMatch   string
		bestVersion = -1
	)

	for _, dir := range versionDirs {
		if !dir.IsDir() {
			continue
		}

		dirVersion, err := strconv.Atoi(dir.Name())
		if err != nil {
			continue
		}

		if dirVersion <= serverVersion {
			continue
		}

		if bestVersion == -1 || dirVersion < bestVersion {
			bestVersion = dirVersion
			bestMatch = dir.Name()
		}
	}

	var templatePath string
	if bestMatch != "" {
		templatePath = filepath.Join(basePath, bestMatch, path.Base(queryName)+".tmpl.sql")
	} else {
		templatePath = filepath.Join(basePath, path.Base(queryName)+".tmpl.sql")
	}

	content, err := sqlFS.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}

	return string(content), nil
}
