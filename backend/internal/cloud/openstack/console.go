package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/remoteconsoles"
)

type ConsoleProvider struct {
	client *Client
}

func NewConsoleProvider(client *Client) *ConsoleProvider {
	return &ConsoleProvider{client: client}
}

func (p *ConsoleProvider) ConsoleURL(ctx context.Context, serverID string) (string, error) {
	if p == nil || p.client == nil {
		return "", errors.New("openstack client is nil")
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return "", errors.New("server_id is required")
	}

	services, err := p.client.Services(ctx)
	if err != nil {
		return "", err
	}
	if services.Compute == nil {
		return "", errors.New("openstack compute service is unavailable")
	}

	legacyURL, legacyErr := legacyNoVNCConsoleURL(ctx, services.Compute, serverID)
	if legacyErr == nil {
		return legacyURL, nil
	}
	modernURL, modernErr := remoteNoVNCConsoleURL(ctx, services.Compute, serverID)
	if modernErr == nil {
		return modernURL, nil
	}
	return "", fmt.Errorf("legacy console: %w; remote console: %w", legacyErr, modernErr)
}

func legacyNoVNCConsoleURL(ctx context.Context, compute *gophercloud.ServiceClient, serverID string) (string, error) {
	var result struct {
		Console struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"console"`
	}
	body := map[string]any{
		"os-getVNCConsole": map[string]string{
			"type": "novnc",
		},
	}
	_, err := compute.Post(ctx, compute.ServiceURL("servers", serverID, "action"), body, &result, &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK},
	})
	if err != nil {
		return "", err
	}
	return validateConsoleURL(result.Console.URL)
}

func remoteNoVNCConsoleURL(ctx context.Context, compute *gophercloud.ServiceClient, serverID string) (string, error) {
	console, err := remoteconsoles.Create(ctx, compute, serverID, remoteconsoles.CreateOpts{
		Protocol: remoteconsoles.ConsoleProtocolVNC,
		Type:     remoteconsoles.ConsoleTypeNoVNC,
	}).Extract()
	if err != nil {
		return "", err
	}
	if console == nil {
		return "", errors.New("openstack returned empty remote console")
	}
	return validateConsoleURL(console.URL)
}

func validateConsoleURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("openstack returned empty console URL")
	}
	return rawURL, nil
}
