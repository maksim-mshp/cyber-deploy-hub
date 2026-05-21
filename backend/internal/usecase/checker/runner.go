package checker

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Runner interface {
	Run(ctx context.Context, profile Profile, target RemoteTarget) ([]StepResult, error)
}

func commandForStep(step Step) (string, error) {
	switch step.Type {
	case StepPackageInstalled:
		pkg := shellQuote(step.Package)
		return "if command -v dpkg-query >/dev/null 2>&1; then dpkg-query -W -f='${Status}' " + pkg + " 2>/dev/null | grep -q 'install ok installed'; elif command -v rpm >/dev/null 2>&1; then rpm -q " + pkg + " >/dev/null 2>&1; else command -v " + pkg + " >/dev/null 2>&1; fi", nil
	case StepFileExists:
		return "test -e " + shellQuote(step.Path), nil
	case StepFileContains:
		return "grep -F -- " + shellQuote(step.Contains) + " " + shellQuote(step.Path) + " >/dev/null", nil
	case StepServiceActive:
		return "systemctl is-active --quiet " + shellQuote(step.Service), nil
	case StepPortOpen:
		return fmt.Sprintf("if command -v ss >/dev/null 2>&1; then ss -ltnH | awk '{print $4}' | grep -Eq '(^|[.:])%d$'; else netstat -ltn 2>/dev/null | awk '{print $4}' | grep -Eq '(^|[.:])%d$'; fi", step.Port, step.Port), nil
	case StepCommandExitCode:
		return step.Command, nil
	default:
		return "", fmt.Errorf("unsupported checker step type %q", step.Type)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func timeoutForStep(step Step, fallback time.Duration) time.Duration {
	if step.TimeoutSeconds > 0 {
		return time.Duration(step.TimeoutSeconds) * time.Second
	}
	if fallback > 0 {
		return fallback
	}
	return 15 * time.Second
}

func trimTail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
