package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"gpu-scheduler/internal/domain"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(socketPath string) *Client {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{httpClient: &http.Client{Transport: transport, Timeout: 5 * time.Second}}
}

func (c *Client) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/internal/v1/health", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent health returned %s", resp.Status)
	}
	return nil
}

func (c *Client) Topology(ctx context.Context) (domain.Topology, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/internal/v1/topology", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.Topology{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.Topology{}, fmt.Errorf("agent topology returned %s", resp.Status)
	}
	var topology domain.Topology
	if err := json.NewDecoder(resp.Body).Decode(&topology); err != nil {
		return domain.Topology{}, err
	}
	return topology, nil
}

