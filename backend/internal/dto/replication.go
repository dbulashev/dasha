package dto

import "time"

type ReplicationStatus struct {
	Pid              int
	Usename          string
	ApplicationName  string
	ClientAddr       string
	State            string
	SentLsn          string
	WriteLsn         string
	FlushLsn         string
	ReplayLsn        string
	WriteLagSeconds  float64
	FlushLagSeconds  float64
	ReplayLagSeconds float64
	ReplayLagBytes   int64
	SyncState        string
	SlotName         string
}

type ReplicationConfig struct {
	SynchronousStandbyNames string
	SynchronousCommit       string
}

type ReplicationSlot struct {
	SlotName     string
	SlotType     string
	Database     string
	Active       bool
	WalStatus    string
	SafeWalSize  *int64
	BacklogBytes *int64
}

type WalPosition struct {
	Lsn        *string
	CapturedAt time.Time
}

type ReplicationSlotsForecast struct {
	ElapsedSeconds     *float64
	WalBytesPerSecond  *float64
	MaxSlotWalKeepSize *int64
	Slots              []ReplicationSlotForecast
}

type ReplicationSlotForecast struct {
	SlotName      string
	SlotType      string
	Database      string
	Active        bool
	WalStatus     string
	RetainedBytes *int64
	SafeWalSize   *int64
	SecondsToLost *float64
}
