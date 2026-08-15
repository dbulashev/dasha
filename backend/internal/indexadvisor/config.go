package indexadvisor

import "time"

// Defaults for the global index_advisor section.
//
// MinTableRows deliberately equals the threshold of the indexes/missing signal:
// two signals about the same table must not disagree on whether it is big enough
// to care about.
const (
	DefaultMaxQueries      = 500
	DefaultMaxQueryBytes   = 100 * 1024
	DefaultMaxCandidates   = 50
	DefaultMaxIndexColumns = 3
	DefaultMinTableRows    = 10000
	DefaultParseCacheSize  = 1000
	DefaultTimeout         = 60 * time.Second
)

// MaxIndexColumnsCeiling caps what an operator may configure. Past four columns a
// btree candidate is guesswork: the normalized text of pg_stat_statements carries
// no constants, so nothing in this step knows the selectivity that would justify
// the tail of the key.
const MaxIndexColumnsCeiling = 4

// Config is the global index_advisor section — global rather than per-cluster,
// like the other analysis settings.
type Config struct {
	// Enabled defaults to true. The switch exists to turn off the WASM parser if
	// it turns out to be expensive on a given host, not for safety: the whole
	// feature is read-only and never emits DDL.
	Enabled         *bool         `mapstructure:"enabled"`
	MaxQueries      int           `mapstructure:"max_queries"`
	MaxQueryBytes   int           `mapstructure:"max_query_bytes"`
	MaxCandidates   int           `mapstructure:"max_candidates"`
	MaxIndexColumns int           `mapstructure:"max_index_columns"`
	MinTableRows    int64         `mapstructure:"min_table_rows"`
	ParseCacheSize  int           `mapstructure:"parse_cache_size"`
	Timeout         time.Duration `mapstructure:"timeout"`
}

// IsEnabled reports the flag with its default applied: an absent key means on.
func (c Config) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// WithDefaults fills the unset fields, so a zero Config is a working one.
func (c Config) WithDefaults() Config {
	if c.MaxQueries <= 0 {
		c.MaxQueries = DefaultMaxQueries
	}

	if c.MaxQueryBytes <= 0 {
		c.MaxQueryBytes = DefaultMaxQueryBytes
	}

	if c.MaxCandidates <= 0 {
		c.MaxCandidates = DefaultMaxCandidates
	}

	if c.MaxIndexColumns <= 0 {
		c.MaxIndexColumns = DefaultMaxIndexColumns
	}

	if c.MaxIndexColumns > MaxIndexColumnsCeiling {
		c.MaxIndexColumns = MaxIndexColumnsCeiling
	}

	if c.MinTableRows <= 0 {
		c.MinTableRows = DefaultMinTableRows
	}

	if c.ParseCacheSize <= 0 {
		c.ParseCacheSize = DefaultParseCacheSize
	}

	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}

	return c
}
