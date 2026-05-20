package ki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyber-deploy-hub/internal/config"
)

func TestClientClusterStatAuthenticatesProjectSession(t *testing.T) {
	var loginCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/login":
			loginCalls++
			if r.Method != http.MethodPost {
				t.Fatalf("login method = %s", r.Method)
			}
			if got := r.Header.Get("x-session-id"); got != "1" {
				t.Fatalf("login x-session-id = %q, want 1", got)
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode login payload: %v", err)
			}
			if payload["domain"] != "Hackhaton" || payload["username"] != "student" || payload["password"] != "secret" {
				t.Fatalf("unexpected login payload: %#v", payload)
			}
			http.SetCookie(w, &http.Cookie{Name: "session1", Value: "test-session", Path: "/"})
			writeJSON(t, w, map[string]string{"token": "user-token"})
		case "/api/v2/accounts/projects/project-1/auth/":
			if _, err := r.Cookie("session1"); err != nil {
				t.Fatalf("project auth missing session cookie: %v", err)
			}
			if got := r.Header.Get("x-session-id"); got != "1" {
				t.Fatalf("project auth x-session-id = %q, want 1", got)
			}
			writeJSON(t, w, map[string]string{"token": "project-token"})
		case "/api/v2/compute/cluster/stat":
			if _, err := r.Cookie("session1"); err != nil {
				t.Fatalf("cluster stat missing session cookie: %v", err)
			}
			if got := r.Header.Get("x-auth-token"); got != "project-1" {
				t.Fatalf("cluster stat x-auth-token = %q, want project id", got)
			}
			if got := r.Header.Get("x-session-id"); got != "1" {
				t.Fatalf("cluster stat x-session-id = %q, want 1", got)
			}
			writeJSON(t, w, map[string]any{
				"datetime": "2026-05-20T23:09:44Z",
				"compute": map[string]any{
					"vcpus":                16,
					"vcpus_free":           1904,
					"vm_mem_capacity":      2245392130048,
					"vm_mem_free":          1023473934336,
					"block_capacity":       229780750336,
					"block_usage":          1579155456,
					"cpu_allocation_ratio": 8,
					"ram_allocation_ratio": 1,
					"cpu_usage":            0.02,
				},
				"servers": map[string]any{
					"count":       2,
					"running":     2,
					"error":       0,
					"in_progress": 0,
					"stopped":     0,
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(config.KIConfig{
		APIBaseURL: server.URL,
		ProjectID:  "project-1",
		SessionID:  "1",
		Username:   "student",
		Password:   "secret",
		DomainName: "Hackhaton",
		Timeout:    time.Second,
	})

	stat, err := client.ClusterStat(context.Background())
	if err != nil {
		t.Fatalf("ClusterStat: %v", err)
	}
	if stat.Compute.VCPUs != 16 || stat.Compute.VCPUsFree != 1904 {
		t.Fatalf("unexpected compute stats: %#v", stat.Compute)
	}
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
