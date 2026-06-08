package yandex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/mdb/postgresql/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServiceType selects which Yandex MDB log stream to read.
type ServiceType int

const (
	// ServicePostgreSQL reads the PostgreSQL server log.
	ServicePostgreSQL ServiceType = iota
	// ServicePooler reads the connection pooler (Odyssey) log.
	ServicePooler
)

func (t ServiceType) proto() postgresql.StreamClusterLogsRequest_ServiceType {
	if t == ServicePooler {
		return postgresql.StreamClusterLogsRequest_POOLER
	}

	return postgresql.StreamClusterLogsRequest_POSTGRESQL
}

// LogRecord is a single decoded log line: a timestamp, the raw message map
// whose keys depend on the service type (see the design doc for column lists),
// and the cursor token that resumes streaming right after this record.
type LogRecord struct {
	Timestamp time.Time
	Fields    map[string]string
	Token     string
}

// StreamLogsParams configures a single StreamClusterLogs read.
type StreamLogsParams struct {
	ClusterID   string
	ServiceType ServiceType
	From, To    time.Time
	Filter      string   // native expression (host + severity only)
	Columns     []string // column_filter; empty = all columns
	RecordToken string   // cursor to resume from a previous read
}

// StreamLogs opens StreamClusterLogs as a bounded historical read (from/to set)
// and invokes fn for each decoded record. It stops when fn returns false, when
// the stream ends, or when ctx is cancelled. The caller controls the scan budget
// and per-record cursor via fn (each LogRecord carries its resume Token).
func (sdk *SDK) StreamLogs(
	ctx context.Context,
	p StreamLogsParams,
	fn func(LogRecord) bool,
) error {
	client, err := sdk.Client(ctx)
	if err != nil {
		return fmt.Errorf("StreamLogs | %w", err)
	}

	req := &postgresql.StreamClusterLogsRequest{ //nolint:exhaustruct
		ClusterId:    p.ClusterID,
		ColumnFilter: p.Columns,
		ServiceType:  p.ServiceType.proto(),
		FromTime:     timestamppb.New(p.From),
		ToTime:       timestamppb.New(p.To),
		RecordToken:  p.RecordToken,
		Filter:       p.Filter,
	}

	stream, err := client.MDB().PostgreSQL().Cluster().StreamLogs(ctx, req)
	if err != nil {
		return fmt.Errorf("StreamLogs | open stream: %w", err)
	}

	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return nil
		}

		if recvErr != nil {
			return fmt.Errorf("StreamLogs | recv: %w", recvErr)
		}

		rec := msg.GetRecord()
		if rec == nil {
			continue
		}

		if !fn(LogRecord{
			Timestamp: rec.GetTimestamp().AsTime(),
			Fields:    rec.GetMessage(),
			Token:     msg.GetNextRecordToken(),
		}) {
			return nil
		}
	}
}
