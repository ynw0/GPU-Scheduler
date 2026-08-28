package agentapi

import (
	"context"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
)

type Client struct {
	httpClient *http.Client
}

func (c *Client) Images(ctx context.Context) ([]domain.ImageInfo, error) {
	var result []domain.ImageInfo
	if err := c.getJSON(ctx, "/internal/v1/images", &result); err != nil { return nil, err }
	return result, nil
}

func (c *Client) Claims(ctx context.Context) ([]domain.ContainerClaim, error) {
	var result []domain.ContainerClaim
	if err := c.getJSON(ctx, "/internal/v1/container-claims", &result); err != nil { return nil, err }
	return result, nil
}

func (c *Client) HostMetrics(ctx context.Context) (domain.HostMetricSnapshot, error) {
	var result domain.HostMetricSnapshot
	if err := c.getJSON(ctx, "/internal/v1/metrics/host", &result); err != nil { return result, err }
	return result, nil
}

func (c *Client) CreateImage(ctx context.Context, request executor.ImageCreateRequest) (executor.RuntimeRef, error) {
	var result executor.RuntimeRef
	if err := c.postJSON(ctx, "/internal/v1/workloads/image", request, &result); err != nil { return result, err }
	return result, nil
}

func (c *Client) CreateCompose(ctx context.Context, request executor.ComposeCreateRequest) (executor.RuntimeRef, executor.EffectiveCompose, error) {
	var response struct {
		Runtime   executor.RuntimeRef       `json:"runtime"`
		Effective executor.EffectiveCompose `json:"effective"`
	}
	if err := c.postJSON(ctx, "/internal/v1/workloads/compose", map[string]any{"request": request}, &response); err != nil {
		return executor.RuntimeRef{}, executor.EffectiveCompose{}, err
	}
	return response.Runtime, response.Effective, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("agent returned %s", resp.Status) }
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) postJSON(ctx context.Context, path string, value, target any) error {
	body, err := json.Marshal(value)
	if err != nil { return err }
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("agent returned %s", resp.Status) }
	return json.NewDecoder(resp.Body).Decode(target)
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
