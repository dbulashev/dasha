package explain

import (
	"fmt"
	"strings"
)

// Reasons a plan body yields no tree. They travel to the API as codes, the way
// Index Advisor reports its skipped statements.
const (
	CodeUnsupportedFormat = "unsupported_plan_format"
	CodeEmptyPlan         = "empty_plan"
	CodeParseError        = "parse_error"
)

// Error is a refusal to parse, carrying the code the caller reports.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Code
	}

	return e.Code + ": " + e.Detail
}

func parseErrorf(format string, args ...any) *Error {
	return &Error{Code: CodeParseError, Detail: fmt.Sprintf(format, args...)}
}

// DetectFormat decides the format from the body alone. The order of the checks
// is part of the contract: text and yaml both open with "Query Text: ", so yaml
// has to be recognised explicitly instead of falling through to text, where the
// text parser would quietly return a one-node tree.
func DetectFormat(body string) (Format, error) {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if trimmed == "" {
		return "", &Error{Code: CodeEmptyPlan}
	}

	switch trimmed[0] {
	case '{', '[':
		return FormatJSON, nil
	case '<':
		if strings.HasPrefix(trimmed, "<explain") {
			return "", &Error{Code: CodeUnsupportedFormat, Detail: "xml"}
		}
	}

	if isYAML(trimmed) {
		return "", &Error{Code: CodeUnsupportedFormat, Detail: "yaml"}
	}

	return FormatText, nil
}

func isYAML(body string) bool {
	if strings.HasPrefix(body, `Query Text: "`) {
		return true
	}

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}

		switch strings.TrimRight(line, " ") {
		case "Plan:", "- Plan:":
			return true
		}
	}

	return false
}

// Parse detects the format and parses accordingly.
func Parse(body string, src Source) (Plan, error) {
	format, err := DetectFormat(body)
	if err != nil {
		return Plan{}, err
	}

	if format == FormatJSON {
		return ParseJSON(body, src)
	}

	return ParseText(body, src)
}
