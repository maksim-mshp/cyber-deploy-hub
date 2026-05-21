package vdigateway

import "time"

const (
	TokenStateActive  = "ACTIVE"
	TokenStateRevoked = "REVOKED"
	TokenStateExpired = "EXPIRED"
)

type AccessToken struct {
	TokenHash    string
	LabRunID     string
	StudentID    string
	ProjectID    string
	State        string
	ExpiresAt    time.Time
	IssuedAt     time.Time
	RevokedAt    *time.Time
	RevokeReason string
}

type IssueResult struct {
	AccessURL string
	ExpiresAt time.Time
}

type SessionLaunch struct {
	LaunchURL    string    `json:"launch_url"`
	ExpiresAt    time.Time `json:"expires_at"`
	ServerID     string    `json:"server_id,omitempty"`
	InstanceName string    `json:"instance_name,omitempty"`
}

type OpenSessionRequest struct {
	Token        string
	ServerID     string
	InstanceName string
	RemoteAddr   string
	UserAgent    string
}

type InstanceTarget struct {
	ServerID string
	Name     string
	State    string
}

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
