package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
)

type Client struct {
	httpClient *http.Client
	longClient *http.Client
}

func NewClient(socketPath string) *Client {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: 5 * time.Second},
		longClient: &http.Client{Transport: transport, Timeout: 10 * time.Minute},
	}
}

func (c *Client) Images(ctx context.Context) ([]domain.ImageInfo, error) {
	var result []domain.ImageInfo
	if err := c.getJSON(ctx, "/internal/v1/images", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Claims(ctx context.Context) ([]domain.ContainerClaim, error) {
	var result []domain.ContainerClaim
	if err := c.getJSON(ctx, "/internal/v1/container-claims", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) HostMetrics(ctx context.Context) (domain.HostMetricSnapshot, error) {
	var result domain.HostMetricSnapshot
	if err := c.getJSON(ctx, "/internal/v1/metrics/host", &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) ContainerMetrics(ctx context.Context, containerID string) (domain.ContainerMetricSnapshot, error) {
	var result domain.ContainerMetricSnapshot
	path := "/internal/v1/metrics/container?containerId=" + url.QueryEscape(containerID)
	if err := c.getJSON(ctx, path, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) StopContainer(ctx context.Context, containerID string, timeoutSeconds int) error {
	return c.postJSON(ctx, c.longClient, "/internal/v1/containers/stop", map[string]any{
		"containerId": containerID, "timeoutSeconds": timeoutSeconds,
	}, nil)
}

func (c *Client) HostGPUProcesses(ctx context.Context) ([]string, error) {
	var result []string
	if err := c.getJSON(ctx, "/internal/v1/gpu-host-processes", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateImage(ctx context.Context, request executor.ImageCreateRequest) (executor.RuntimeRef, error) {
	var result executor.RuntimeRef
	if err := c.postJSON(ctx, c.longClient, "/internal/v1/workloads/image", request, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) CreateCompose(ctx context.Context, request executor.ComposeCreateRequest) (executor.RuntimeRef, executor.EffectiveCompose, error) {
	var response struct {
		Runtime   executor.RuntimeRef       `json:"runtime"`
		Effective executor.EffectiveCompose `json:"effective"`
	}
	if err := c.postJSON(ctx, c.longClient, "/internal/v1/workloads/compose", map[string]any{"request": request}, &response); err != nil {
		return executor.RuntimeRef{}, executor.EffectiveCompose{}, err
	}
	return response.Runtime, response.Effective, nil
}

func (c *Client) Stop(ctx context.Context, runtime executor.RuntimeRef, timeoutSeconds int) error {
	return c.postJSON(ctx, c.longClient, "/internal/v1/workloads/stop", map[string]any{
		"runtime": runtime, "timeoutSeconds": timeoutSeconds,
	}, nil)
}

func (c *Client) RemoveAttempt(ctx context.Context, attemptID string) error {
	return c.postJSON(ctx, c.longClient, "/internal/v1/workloads/remove", map[string]any{"attemptId": attemptID}, nil)
}

func (c *Client) Logs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error) {
	path := "/internal/v1/workloads/logs?containerId=" + containerID
	if follow {
		path += "&follow=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.longClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("agent returned %s", resp.Status)
	}
	return resp.Body, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agent returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) postJSON(ctx context.Context, client *http.Client, path string, value, target any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agent returned %s", resp.Status)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) Health(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/internal/v1/health", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent health returned %s", resp.Status)
	}
	var result struct {
		Platform struct {
			Warnings []string `json:"warnings"`
		} `json:"platform"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Platform.Warnings, nil
}

func (c *Client) Topology(ctx context.Context) (domain.Topology, error) {
	var topology domain.Topology
	if err := c.getJSON(ctx, "/internal/v1/topology", &topology); err != nil {
		return domain.Topology{}, err
	}
	return topology, nil
}
