package lifecycle

import "time"

const (
	TimerKindCleanup = "CLEANUP"

	TimerStateScheduled = "SCHEDULED"
	TimerStateFired     = "FIRED"
	TimerStateCancelled = "CANCELLED"
)

const (
	SettingLabTTLSeconds          = "lab_ttl_seconds"
	SettingFreezeTTLSeconds       = "freeze_ttl_seconds"
	SettingCapacityThreshold      = "capacity_threshold_percent"
	DefaultCapacityThresholdValue = 90
)

type Timer struct {
	ID                 int64
	LabRunID           string
	Kind               string
	State              string
	DueAt              time.Time
	Reason             string
	CreatedByMessageID string
}

type RuntimeSettings struct {
	LabTTLSeconds            int64
	FreezeTTLSeconds         int64
	CapacityThresholdPercent float64
}

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
