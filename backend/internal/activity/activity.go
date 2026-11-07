// Package activity samples pg_stat_activity of one host and classifies what
// every counted session is doing: on CPU, waiting on a wait event type, or idle
// inside an open transaction.
package activity

import (
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MinVersionNum is the first PostgreSQL version with pg_stat_activity.query_id.
const MinVersionNum = 140000

var ErrUnsupportedVersion = errors.New("activity sampling requires PostgreSQL 14 or newer")

const (
	ClassCPU          = "CPU"
	ClassIO           = "IO"
	ClassLock         = "Lock"
	ClassLWLock       = "LWLock"
	ClassBufferPin    = "BufferPin"
	ClassIPC          = "IPC"
	ClassClient       = "Client"
	ClassClientIdleTx = "ClientIdleTx"
	ClassTimeout      = "Timeout"
	ClassExtension    = "Extension"
	ClassActivity     = "Activity"
	ClassOther        = "Other"
)

// Classes is the stacking order, bottom first. The swagger enum lists the same order.
var Classes = []string{
	ClassCPU, ClassIO, ClassLock, ClassLWLock, ClassBufferPin, ClassIPC,
	ClassClient, ClassTimeout, ClassClientIdleTx, ClassExtension, ClassActivity, ClassOther,
}

var waitTypeClasses = map[string]string{
	"Activity":  ClassActivity,
	"BufferPin": ClassBufferPin,
	"Client":    ClassClient,
	"Extension": ClassExtension,
	"IO":        ClassIO,
	"IPC":       ClassIPC,
	"Lock":      ClassLock,
	"LWLock":    ClassLWLock,
	"Timeout":   ClassTimeout,
}

const (
	KeyKindQueryID     = "query_id"
	KeyKindFingerprint = "fingerprint"
	KeyKindText        = "text"
	KeyKindNone        = "none"
)

const (
	clientBackend  = "client backend"
	parallelWorker = "parallel worker"
)

// RunsStatement reports whether a backend type executes a client statement.
// A parallel worker runs its leader's statement under the same query_id.
func RunsStatement(backendType string) bool {
	return backendType == clientBackend || backendType == parallelWorker
}

type Session struct {
	PID         int32
	BackendType string
	Database    string
	User        string
	Application string
	ClientAddr  string
	State       string
	WaitType    string
	WaitEvent   string
	QueryID     int64
	QueryStart  time.Time // zero when NULL
	XactStart   time.Time // zero when NULL
	HoldsXmin   bool
	Text        string // sanitized, truncated
	Key         string
	KeyKind     string
}

// Sample is read-only once built: the sampler hands the same value to every caller.
type Sample struct {
	At        time.Time // server now() of the sample
	Sessions  []Session
	Truncated bool // more sessions than max_sessions
}

type Access struct {
	CanReadAllStats  bool
	QueryIDAvailable bool
	// TrackUtility mirrors track_utility of the preloaded statistics library.
	TrackUtility bool
}

// QueryIDAvailable reports whether the server computes query_id by itself
// given compute_query_id and whether pg_stat_statements is preloaded.
func QueryIDAvailable(computeQueryID string, pgssPreloaded bool) bool {
	switch strings.ToLower(computeQueryID) {
	case "on", "regress":
		return true
	case "auto":
		return pgssPreloaded
	default:
		return false
	}
}

func Class(s Session) string {
	if strings.HasPrefix(s.State, "idle in transaction") {
		return ClassClientIdleTx
	}

	if s.WaitType == "" {
		return ClassCPU
	}

	if c, ok := waitTypeClasses[s.WaitType]; ok {
		return c
	}

	return ClassOther
}

// Counted reports whether the session belongs to the active load.
func Counted(s Session, includeBackground bool) bool {
	if RunsStatement(s.BackendType) {
		switch s.State {
		case "active", "idle in transaction", "idle in transaction (aborted)", "fastpath function call":
			return true
		default:
			return false
		}
	}

	// Background processes park on an Activity wait in their idle main loop.
	return includeBackground && s.WaitType != "Activity"
}

type Fingerprinter interface {
	Fingerprint(sql string) (string, error)
}

const insufficientPrivilege = "<insufficient privilege>"

var (
	stringLiteral  = regexp.MustCompile(`'(?:[^']|'')*'`)
	numericLiteral = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	literalList    = regexp.MustCompile(`\(\s*\?(?:\s*,\s*\?)*\s*\)`)
	tupleList      = regexp.MustCompile(`\(\?\)(?:\s*,\s*\(\?\))+`)
)

// QueryKey groups sessions running the same statement. Expects the full
// sanitized text, before truncation for display.
func QueryKey(s Session, fp Fingerprinter) (key, kind string) {
	if s.QueryID != 0 {
		return "q:" + strconv.FormatInt(s.QueryID, 10), KeyKindQueryID
	}

	text := strings.TrimSpace(s.Text)
	if text == "" || strings.Contains(text, insufficientPrivilege) {
		return "", KeyKindNone
	}

	if fp != nil {
		if f, err := fp.Fingerprint(text); err == nil && f != "" {
			return "f:" + f, KeyKindFingerprint
		}
	}

	masked := numericLiteral.ReplaceAllString(stringLiteral.ReplaceAllString(text, "?"), "?")
	masked = tupleList.ReplaceAllString(literalList.ReplaceAllString(masked, "(?)"), "(?)")

	h := fnv.New64a()
	_, _ = h.Write([]byte(masked))

	return fmt.Sprintf("t:%016x", h.Sum64()), KeyKindText
}

// TruncateText cuts s to at most n runes.
func TruncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}

	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}

	return s
}
