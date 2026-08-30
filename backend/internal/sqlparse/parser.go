package sqlparse

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	pgast "github.com/pganalyze/pg_query_go/v6"
	pgq "github.com/wasilibs/go-pgquery"
)

// Defaults. MaxQueryBytes matches the size beyond which a normalized statement
// stops being a query anyone hand-wrote (generated IN lists, mostly) and starts
// being a parser stress test.
const (
	DefaultMaxQueryBytes = 100 * 1024
	DefaultCacheSize     = 1000
)

// insufficientPrivilege is what pg_stat_statements returns instead of the text
// when the caller may not see it.
const insufficientPrivilege = "<insufficient privilege>"

// truncationMark is the tail pg_stat_statements leaves when it clips a statement
// that outgrew track_activity_query_size.
const truncationMark = "..."

// Config tunes the parser. Zero fields fall back to the defaults.
type Config struct {
	MaxQueryBytes int
	CacheSize     int
	// MaxParallel bounds concurrent parses, and one is the right default: the
	// library builds a whole wazero runtime and compiles the libpg_query module
	// for every parse that finds its pool empty, then keeps it for the life of
	// the process. Parsing itself is cheap, so serializing costs a report far
	// less than a second compiled module costs the host.
	MaxParallel int
}

func (c Config) withDefaults() Config {
	if c.MaxQueryBytes <= 0 {
		c.MaxQueryBytes = DefaultMaxQueryBytes
	}

	if c.CacheSize <= 0 {
		c.CacheSize = DefaultCacheSize
	}

	if c.MaxParallel <= 0 {
		c.MaxParallel = 1
	}

	return c
}

// Error carries the reason code for a text that produced no Statement, so a
// caller can count causes instead of collecting free-form parser messages.
type Error struct {
	Code string
	err  error
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.err)
	}

	return e.Code
}

func (e *Error) Unwrap() error { return e.err }

// ReasonOf reports the reason code behind err, falling back to a parse error for
// anything the parser did not classify itself.
func ReasonOf(err error) string {
	var perr *Error
	if errors.As(err, &perr) {
		return perr.Code
	}

	return ReasonParseError
}

func reason(code string, err error) error { return &Error{Code: code, err: err} }

type parser struct {
	cfg   Config
	cache *lru
	// sem bounds how many statements are in the WASM parser at once. The library
	// keeps an unbounded pool of parser instances and builds a fresh wazero
	// runtime whenever the pool is empty, so unbounded concurrency would leave
	// one compiled module per in-flight parse alive for the process lifetime.
	sem chan struct{}
}

// New builds a Parser. It is safe for concurrent use and holds no database state.
func New(cfg Config) Parser {
	cfg = cfg.withDefaults()

	return &parser{
		cfg:   cfg,
		cache: newLRU(cfg.CacheSize),
		sem:   make(chan struct{}, cfg.MaxParallel),
	}
}

// Parse describes one statement. Texts that cannot yield candidates — blank,
// oversized, clipped by pg_stat_statements, or rejected by the grammar — come
// back as *Error with a reason code, never as a partial Statement: half of a
// clipped WHERE clause would produce a confidently wrong index.
func (p *parser) Parse(sql string) (Statement, error) {
	key := sha256.Sum256([]byte(sql))
	if e, ok := p.cache.get(key); ok {
		return e.st, e.err
	}

	st, err := p.parseUncached(sql)
	p.cache.put(key, st, err)

	return st, err
}

func (p *parser) parseUncached(sql string) (Statement, error) {
	if err := p.precheck(sql); err != nil {
		return Statement{}, err
	}

	text := sql

	tree, err := p.parseTree(text)
	if err != nil {
		if fixed := RestoreParamCasts(sql); fixed != sql {
			text = fixed
			tree, err = p.parseTree(text)
		}
	}

	if err != nil {
		return Statement{}, reason(ReasonParseError, err)
	}

	fp, err := p.fingerprint(text)
	if err != nil {
		return Statement{}, reason(ReasonParseError, err)
	}

	st := describe(tree)
	st.Fingerprint = fp

	return st, nil
}

func (p *parser) precheck(sql string) error {
	trimmed := strings.TrimSpace(sql)

	switch {
	case trimmed == "":
		return reason(ReasonEmpty, nil)
	case strings.Contains(trimmed, insufficientPrivilege):
		return reason(ReasonInsufficientPrivilege, nil)
	case len(sql) > p.cfg.MaxQueryBytes:
		return reason(ReasonTooLong, nil)
	case looksTruncated(trimmed):
		return reason(ReasonTruncated, nil)
	}

	return nil
}

// Fingerprint hashes the structure of a statement: two texts differing only in
// constants, parameter numbering or whitespace share a fingerprint.
func (p *parser) Fingerprint(sql string) (string, error) {
	if err := p.precheck(sql); err != nil {
		return "", err
	}

	fp, err := p.fingerprint(sql)
	if err != nil {
		if fixed := RestoreParamCasts(sql); fixed != sql {
			fp, err = p.fingerprint(fixed)
		}
	}

	if err != nil {
		return "", reason(ReasonParseError, err)
	}

	return fp, nil
}

func looksTruncated(sql string) bool {
	return strings.HasSuffix(strings.TrimRight(sql, "; \t\r\n"), truncationMark)
}

// parseTree and fingerprint are the only calls into the WASM parser. Both recover:
// a malformed statement must cost one query, not the whole report.
func (p *parser) parseTree(sql string) (tree *pgast.ParseResult, err error) {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	defer func() {
		if r := recover(); r != nil {
			tree, err = nil, fmt.Errorf("parser panicked: %v", r)
		}
	}()

	return pgq.Parse(sql)
}

func (p *parser) fingerprint(sql string) (fp string, err error) {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	defer func() {
		if r := recover(); r != nil {
			fp, err = "", fmt.Errorf("fingerprint panicked: %v", r)
		}
	}()

	return pgq.Fingerprint(sql)
}
