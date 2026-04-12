package sanitize

import "regexp"

// Patterns that may leak credentials in SQL text (pg_stat_activity, logs, etc.).
var patterns = []*regexp.Regexp{
	// CONNECTION 'host=... password=secret ...' or password=secret in any connection string
	regexp.MustCompile(`(?i)\bpassword\s*=\s*([^\s']+)`),
	// PASSWORD 'secret' or PASSWORD "secret" in CREATE/ALTER ROLE/USER
	regexp.MustCompile(`(?i)\bpassword\s+'[^']*'`),
	regexp.MustCompile(`(?i)\bpassword\s+"[^"]*"`),
}

var replacements = []string{
	"password=***",
	"PASSWORD '***'",
	"PASSWORD '***'",
}

// SQL masks credentials in SQL text.
func SQL(s string) string {
	for i, re := range patterns {
		s = re.ReplaceAllString(s, replacements[i])
	}
	return s
}
