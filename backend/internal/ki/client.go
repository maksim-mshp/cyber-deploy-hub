package ki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"cyber-deploy-hub/internal/config"
)

type Client struct {
	cfg          config.KIConfig
	httpClient   *http.Client
	sessionMutex sync.Mutex
	sessionReady bool
}

type ClusterStat struct {
	DateTime string `json:"datetime"`
	Compute  struct {
		CPUUsage      float64 `json:"cpu_usage"`
		VCPUs         int     `json:"vcpus"`
		VCPUsFree     int     `json:"vcpus_free"`
		VMMemReserved int64   `json:"vm_mem_reserved"`
		VMMemCapacity int64   `json:"vm_mem_capacity"`
		VMMemFree     int64   `json:"vm_mem_free"`
		BlockCapacity int64   `json:"block_capacity"`
		BlockUsage    int64   `json:"block_usage"`
		CPUAllocRatio float64 `json:"cpu_allocation_ratio"`
		RAMAllocRatio float64 `json:"ram_allocation_ratio"`
	} `json:"compute"`
	Servers struct {
		Count      int `json:"count"`
		Running    int `json:"running"`
		Error      int `json:"error"`
		InProgress int `json:"in_progress"`
		Stopped    int `json:"stopped"`
	} `json:"servers"`
}

func NewClient(cfg config.KIConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	jar, _ := cookiejar.New(nil)
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout, Jar: jar},
	}
}

func (c *Client) Configured() bool {
	return c.cfg.Configured()
}

func (c *Client) ClusterStat(ctx context.Context) (*ClusterStat, error) {
	if !c.Configured() {
		return nil, errors.New("ki api client is not configured")
	}
	if err := c.ensureProjectSession(ctx, false); err != nil {
		return nil, err
	}

	resp, err := c.clusterStat(ctx)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.canLogin() {
		_ = resp.Body.Close()
		c.resetSession()
		if err := c.ensureProjectSession(ctx, true); err != nil {
			return nil, err
		}
		resp, err = c.clusterStat(ctx)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ki api returned status %d", resp.StatusCode)
	}

	var stat ClusterStat
	if err := json.NewDecoder(resp.Body).Decode(&stat); err != nil {
		return nil, err
	}
	return &stat, nil
}

func (c *Client) clusterStat(ctx context.Context) (*http.Response, error) {
	url := c.apiURL("/api/v2/compute/cluster/stat")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req)
	if token := c.projectToken(); token != "" {
		req.Header.Set("x-auth-token", token)
	}
	if cookie := strings.TrimSpace(c.cfg.SessionCookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	return c.httpClient.Do(req)
}

func (c *Client) ensureProjectSession(ctx context.Context, force bool) error {
	if strings.TrimSpace(c.cfg.SessionCookie) != "" || !c.canLogin() {
		return nil
	}

	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()
	if c.sessionReady && !force {
		return nil
	}
	if err := c.login(ctx); err != nil {
		return err
	}
	if err := c.authenticateProject(ctx); err != nil {
		return err
	}
	c.sessionReady = true
	return nil
}

func (c *Client) login(ctx context.Context) error {
	payload := map[string]string{
		"domain":   c.cfg.DomainName,
		"username": c.cfg.Username,
		"password": c.cfg.Password,
	}
	return c.postJSON(ctx, "/api/v2/login", payload)
}

func (c *Client) authenticateProject(ctx context.Context) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/v2/accounts/projects/%s/auth/", c.cfg.ProjectID), map[string]any{})
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(path), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	c.setCommonHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ki api %s returned status %d", path, resp.StatusCode)
	}
	return nil
}

func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if sessionID := strings.TrimSpace(c.cfg.SessionID); sessionID != "" {
		req.Header.Set("x-session-id", sessionID)
	}
}

func (c *Client) apiURL(path string) string {
	return strings.TrimRight(c.cfg.APIBaseURL, "/") + path
}

func (c *Client) projectToken() string {
	projectID := strings.TrimSpace(c.cfg.ProjectID)
	if projectID != "" && (strings.TrimSpace(c.cfg.SessionCookie) != "" || c.canLogin()) {
		return projectID
	}
	if authToken := strings.TrimSpace(c.cfg.AuthToken); authToken != "" {
		return authToken
	}
	return projectID
}

func (c *Client) canLogin() bool {
	return strings.TrimSpace(c.cfg.ProjectID) != "" &&
		strings.TrimSpace(c.cfg.Username) != "" &&
		strings.TrimSpace(c.cfg.Password) != "" &&
		strings.TrimSpace(c.cfg.DomainName) != ""
}

func (c *Client) resetSession() {
	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()
	c.sessionReady = false
}
