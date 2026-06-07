package autosnapshot

import (
	"context"
	"time"

	"go.uber.org/zap"
)

const retentionInterval = 24 * time.Hour

// maybeRunRetention drops the oldest day-triples while the total size of all
// partitioned tables exceeds cfg.RetentionBytes. Partitions younger than
// cfg.RetentionMinDays are always kept. Runs at most once per retentionInterval.
func (d *Daemon) maybeRunRetention(ctx context.Context, cfg Config) {
	if cfg.RetentionBytes <= 0 {
		return
	}

	d.mu.Lock()
	if !d.lastRetry.IsZero() && time.Since(d.lastRetry) < retentionInterval {
		d.mu.Unlock()
		return
	}
	d.lastRetry = time.Now().UTC()
	d.mu.Unlock()

	sizes, err := d.store.ComputePartitionSizes(ctx)
	if err != nil {
		d.logger.Warn("compute partition sizes failed", zap.Error(err))
		return
	}

	var total int64
	for _, s := range sizes {
		total += s.TotalSize
	}

	if total <= cfg.RetentionBytes {
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.RetentionMinDays)

	var (
		droppedDays  int
		freedBytes   int64
		startingSize = total
	)

	for _, s := range sizes {
		if total <= cfg.RetentionBytes {
			break
		}

		if !s.Day.Before(cutoff) {
			break
		}

		if err := d.store.DropDayPartitions(ctx, s.Day); err != nil {
			d.logger.Warn("drop day partitions failed",
				zap.String("day", s.Day.Format("2006-01-02")),
				zap.Error(err))

			continue
		}

		total -= s.TotalSize
		freedBytes += s.TotalSize
		droppedDays++
	}

	if droppedDays > 0 {
		d.logger.Info("autosnapshot retention ran",
			zap.Int("dropped_day_triplets", droppedDays),
			zap.Int64("freed_bytes", freedBytes),
			zap.Int64("size_before", startingSize),
			zap.Int64("size_after", total),
			zap.Int64("retention_bytes", cfg.RetentionBytes))
	}
}
