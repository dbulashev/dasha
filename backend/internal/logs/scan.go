package logs

import (
	"context"
	"errors"

	"github.com/dbulashev/dasha/internal/logs/source"
)

// scanLimits bounds one read of a window; a zero limit is no limit.
type scanLimits struct {
	MaxRecords int
	MaxBytes   int64
	// MaxRecordBytes caps what one record is charged against MaxBytes.
	MaxRecordBytes int64
}

// scanStats says what the read consumed and why it ended.
type scanStats struct {
	Records int
	// Bytes counts field values, and only under a byte budget.
	Bytes int64
	// Capped means a limit ended the read before the window was exhausted.
	Capped bool
	// Partial means the source gave up and cannot hand out a cursor to resume.
	Partial bool
}

func (s *service) searchLimits() scanLimits {
	return scanLimits{MaxRecords: s.cfg.MaxScan, MaxBytes: 0, MaxRecordBytes: 0}
}

// scan streams a window record by record. A record visit rejects is neither
// counted nor charged, and ends the read: callers use that to look ahead
// without consuming.
func (s *service) scan(
	ctx context.Context,
	provider source.Provider,
	params source.StreamParams,
	limits scanLimits,
	visit func(source.Record) bool,
) (scanStats, error) {
	var st scanStats

	err := provider.Stream(ctx, params, func(rec source.Record) bool {
		if !visit(rec) {
			return false
		}

		st.Records++

		if limits.MaxBytes > 0 {
			n := recordBytes(rec)
			if limits.MaxRecordBytes > 0 {
				n = min(n, limits.MaxRecordBytes)
			}

			st.Bytes += n
		}

		if limits.MaxRecords > 0 && st.Records >= limits.MaxRecords {
			st.Capped = true

			return false
		}

		if limits.MaxBytes > 0 && st.Bytes >= limits.MaxBytes {
			st.Capped = true

			return false
		}

		return true
	})
	if err != nil {
		st.Partial = errors.Is(err, source.ErrPartial)

		return st, s.classify(ctx, err)
	}

	return st, nil
}

func recordBytes(rec source.Record) int64 {
	var n int64
	for _, v := range rec.Fields {
		n += int64(len(v))
	}

	return n
}
