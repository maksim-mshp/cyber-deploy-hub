package checker

import "time"

const (
	runStatePassed = "PASSED"
	runStateFailed = "FAILED"
	runStateError  = "ERROR"
)

type StepType string

const (
	StepPackageInstalled StepType = "package_installed"
	StepFileExists       StepType = "file_exists"
	StepFileContains     StepType = "file_contains"
	StepServiceActive    StepType = "service_active"
	StepPortOpen         StepType = "port_open"
	StepCommandExitCode  StepType = "command_exit_code"
)

type Profile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SSHUser string `json:"ssh_user"`
	Steps   []Step `json:"steps"`
}

type Step struct {
	Sequence         int      `json:"sequence,omitempty"`
	Name             string   `json:"name"`
	Type             StepType `json:"type"`
	Package          string   `json:"package,omitempty"`
	Path             string   `json:"path,omitempty"`
	Contains         string   `json:"contains,omitempty"`
	Service          string   `json:"service,omitempty"`
	Port             int      `json:"port,omitempty"`
	Command          string   `json:"command,omitempty"`
	ExpectedExitCode int      `json:"expected_exit_code,omitempty"`
	TimeoutSeconds   int      `json:"timeout_seconds,omitempty"`
}

type Target struct {
	LabRunID            string
	ProjectID           string
	Host                string
	Port                int
	EncryptedPrivateKey []byte
	PrivateKeyNonce     []byte
	PrivateKeyKeyID     string
}

type RemoteTarget struct {
	Host       string
	Port       int
	User       string
	PrivateKey []byte
}

type RemoteCommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type StepResult struct {
	Sequence   int
	Name       string
	Type       StepType
	Passed     bool
	ExitCode   int
	Message    string
	StdoutTail string
	StderrTail string
	StartedAt  time.Time
	FinishedAt time.Time
}

type RunRecord struct {
	ID           string
	LabRunID     string
	ProfileID    string
	State        string
	Passed       bool
	ErrorCode    string
	ErrorMessage string
	StartedAt    time.Time
	FinishedAt   time.Time
	Results      []StepResult
}
