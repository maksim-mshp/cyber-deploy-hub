package checker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestCommandForStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step Step
		want string
	}{
		{
			name: "file exists quotes path",
			step: Step{Type: StepFileExists, Path: "/tmp/a file"},
			want: "test -e '/tmp/a file'",
		},
		{
			name: "file contains quotes pattern",
			step: Step{Type: StepFileContains, Path: "/etc/app.conf", Contains: "key='value'"},
			want: "grep -F -- 'key='\"'\"'value'\"'\"'' '/etc/app.conf' >/dev/null",
		},
		{
			name: "command is direct profile command",
			step: Step{Type: StepCommandExitCode, Command: "test -r /etc/os-release"},
			want: "test -r /etc/os-release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := commandForStep(tt.step)
			if err != nil {
				t.Fatalf("commandForStep: %v", err)
			}
			if got != tt.want {
				t.Fatalf("command = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSSHRunnerRunsProfileAgainstSSHServer(t *testing.T) {
	t.Parallel()

	clientKey, clientPrivateKey := testPrivateKey(t)
	hostKey, _ := testPrivateKey(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	server := &testSSHServer{
		t:                t,
		listener:         listener,
		authorizedKey:    clientKey.PublicKey(),
		expectedCommands: map[string]int{"test -e '/etc/os-release'": 0},
	}
	server.start(hostKey)

	runner := NewSSHRunner(2*time.Second, 2*time.Second)
	results, err := runner.Run(context.Background(), Profile{
		ID:      "integration",
		Name:    "Integration",
		SSHUser: "ubuntu",
		Steps: []Step{{
			Sequence:       1,
			Name:           "OS release",
			Type:           StepFileExists,
			Path:           "/etc/os-release",
			TimeoutSeconds: 2,
		}},
	}, RemoteTarget{
		Host:       listener.Addr().(*net.TCPAddr).IP.String(),
		Port:       listener.Addr().(*net.TCPAddr).Port,
		User:       "ubuntu",
		PrivateKey: clientPrivateKey,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || !results[0].Passed || results[0].ExitCode != 0 {
		t.Fatalf("results = %#v", results)
	}
	server.assertHandled(t, "test -e '/etc/os-release'")
}

type testSSHServer struct {
	t                *testing.T
	listener         net.Listener
	authorizedKey    ssh.PublicKey
	expectedCommands map[string]int

	mu      sync.Mutex
	handled map[string]int
}

func (s *testSSHServer) start(hostSigner ssh.Signer) {
	s.handled = map[string]int{}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), s.authorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unauthorized key")
		},
	}
	config.AddHostKey(hostSigner)

	go func() {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		_, chans, reqs, err := ssh.NewServerConn(conn, config)
		if err != nil {
			s.t.Errorf("server conn: %v", err)
			return
		}
		go ssh.DiscardRequests(reqs)
		for ch := range chans {
			if ch.ChannelType() != "session" {
				_ = ch.Reject(ssh.UnknownChannelType, "unknown channel")
				continue
			}
			channel, requests, err := ch.Accept()
			if err != nil {
				s.t.Errorf("accept channel: %v", err)
				return
			}
			go s.handleSession(channel, requests)
		}
	}()
}

func (s *testSSHServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() {
		_ = channel.Close()
	}()
	for req := range requests {
		switch req.Type {
		case "exec":
			command := execCommand(req.Payload)
			status, ok := s.expectedCommands[command]
			if !ok {
				status = 127
			}
			s.mu.Lock()
			s.handled[command]++
			s.mu.Unlock()
			_, _ = io.WriteString(channel, "ok\n")
			_ = req.Reply(true, nil)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct {
				Status uint32
			}{Status: uint32(status)}))
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *testSSHServer) assertHandled(t *testing.T, command string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handled[command] != 1 {
		t.Fatalf("command %q handled %d times", command, s.handled[command])
	}
}

func execCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	length := binary.BigEndian.Uint32(payload[:4])
	if int(length) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+length])
}

func testPrivateKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return signer, pem.EncodeToMemory(block)
}
