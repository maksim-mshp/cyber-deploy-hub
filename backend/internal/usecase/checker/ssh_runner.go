package checker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHRunner struct {
	dialTimeout           time.Duration
	defaultCommandTimeout time.Duration
}

func NewSSHRunner(dialTimeout time.Duration, defaultCommandTimeout time.Duration) *SSHRunner {
	if dialTimeout <= 0 {
		dialTimeout = 20 * time.Second
	}
	if defaultCommandTimeout <= 0 {
		defaultCommandTimeout = 15 * time.Second
	}
	return &SSHRunner{dialTimeout: dialTimeout, defaultCommandTimeout: defaultCommandTimeout}
}

func (r *SSHRunner) Run(ctx context.Context, profile Profile, target RemoteTarget) ([]StepResult, error) {
	if target.Host == "" {
		return nil, errors.New("ssh target host is empty")
	}
	if target.Port <= 0 {
		target.Port = 22
	}
	if target.User == "" {
		target.User = profile.SSHUser
	}
	signer, err := ssh.ParsePrivateKey(target.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	client, err := r.dial(ctx, target, signer)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	results := make([]StepResult, 0, len(profile.Steps))
	for _, step := range profile.Steps {
		started := time.Now().UTC()
		command, err := commandForStep(step)
		if err != nil {
			return results, err
		}
		remoteResult, err := runSSHCommand(ctx, client, command, timeoutForStep(step, r.defaultCommandTimeout))
		finished := time.Now().UTC()
		if err != nil {
			return results, fmt.Errorf("run step %q: %w", step.Name, err)
		}
		passed := remoteResult.ExitCode == step.ExpectedExitCode
		message := "passed"
		if !passed {
			message = fmt.Sprintf("expected exit code %d, got %d", step.ExpectedExitCode, remoteResult.ExitCode)
		}
		results = append(results, StepResult{
			Sequence:   step.Sequence,
			Name:       step.Name,
			Type:       step.Type,
			Passed:     passed,
			ExitCode:   remoteResult.ExitCode,
			Message:    message,
			StdoutTail: trimTail(remoteResult.Stdout, 4096),
			StderrTail: trimTail(remoteResult.Stderr, 4096),
			StartedAt:  started,
			FinishedAt: finished,
		})
		if !passed {
			break
		}
	}
	return results, nil
}

func (r *SSHRunner) dial(ctx context.Context, target RemoteTarget, signer ssh.Signer) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: r.dialTimeout}
	address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial ssh %s: %w", address, err)
	}

	config := &ssh.ClientConfig{
		User:            target.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         r.dialTimeout,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("handshake ssh %s: %w", address, err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func runSSHCommand(ctx context.Context, client *ssh.Client, command string, timeout time.Duration) (RemoteCommandResult, error) {
	session, err := client.NewSession()
	if err != nil {
		return RemoteCommandResult{}, err
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-runCtx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return RemoteCommandResult{ExitCode: -1, Stdout: stdout.String(), Stderr: stderr.String()}, runCtx.Err()
	case err := <-done:
		result := RemoteCommandResult{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return result, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			return result, nil
		}
		return result, err
	}
}
