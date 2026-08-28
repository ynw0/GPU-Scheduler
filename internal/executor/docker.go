package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/inventory"
)

type Docker struct {
	client *client.Client
}

func NewDocker() (*Docker, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil { return nil, fmt.Errorf("create Docker client: %w", err) }
	return &Docker{client: cli}, nil
}

func (d *Docker) Close() error { return d.client.Close() }

func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.client.Ping(ctx)
	return err
}

func (d *Docker) ListImages(ctx context.Context) ([]domain.ImageInfo, error) {
	response, err := d.client.ImageList(ctx, client.ImageListOptions{})
	if err != nil { return nil, err }
	result := make([]domain.ImageInfo, 0)
	for _, item := range response.Items {
		for _, tagged := range item.RepoTags {
			repository, imageTag := tagged, ""
			if separator := strings.LastIndex(tagged, ":"); separator >= 0 {
				repository, imageTag = tagged[:separator], tagged[separator+1:]
			}
			result = append(result, domain.ImageInfo{
				ID: item.ID, Repository: repository, Tag: imageTag,
				SizeBytes: item.Size, CreatedAt: time.Unix(item.Created, 0).UTC(),
			})
		}
	}
	return result, nil
}

func (d *Docker) CreateImage(ctx context.Context, request ImageCreateRequest) (RuntimeRef, error) {
	mounts := make([]mount.Mount, 0, len(request.Workload.Mounts))
	for _, item := range request.Workload.Mounts {
		mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: item.Source, Target: item.Target, ReadOnly: item.ReadOnly})
	}
	environment := make([]string, 0, len(request.Workload.Environment))
	for key, value := range request.Workload.Environment { environment = append(environment, key+"="+value) }
	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			CpusetCpus: inventory.FormatCPUList(request.Plan.CPUIds),
			CpusetMems: inventory.FormatCPUList(request.Plan.NUMANodes),
			Memory: int64(request.Plan.MemoryBytes), MemorySwap: int64(request.Plan.MemoryBytes),
		},
		Mounts: mounts,
		Init: new(true),
	}
	if len(request.Plan.GPUUUIDs) > 0 {
		hostConfig.DeviceRequests = []container.DeviceRequest{{
			Driver: "nvidia", DeviceIDs: request.Plan.GPUUUIDs, Capabilities: [][]string{{"gpu"}},
		}}
	}
	response, err := d.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: request.Name,
		Config: &container.Config{
			Image: request.Workload.Image, Cmd: request.Workload.Command,
			Entrypoint: request.Workload.Entrypoint, WorkingDir: request.Workload.WorkingDirectory,
			Env: environment,
			Labels: map[string]string{LabelManaged: "true", LabelJobID: request.JobID, LabelAttemptID: request.AttemptID},
		},
		HostConfig: hostConfig,
	})
	if err != nil { return RuntimeRef{}, fmt.Errorf("create container: %w", err) }
	if err := d.verifyResources(ctx, response.ID, request.Plan); err != nil {
		_, _ = d.client.ContainerRemove(ctx, response.ID, client.ContainerRemoveOptions{Force: true})
		return RuntimeRef{}, err
	}
	if _, err := d.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = d.client.ContainerRemove(ctx, response.ID, client.ContainerRemoveOptions{Force: true})
		return RuntimeRef{}, fmt.Errorf("start container: %w", err)
	}
	return RuntimeRef{Kind: "image", ContainerIDs: map[string]string{"main": response.ID}}, nil
}

func (d *Docker) Stop(ctx context.Context, ref RuntimeRef, timeoutSeconds int) error {
	for _, id := range ref.ContainerIDs {
		timeout := timeoutSeconds
		if _, err := d.client.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout}); err != nil { return err }
	}
	return nil
}

func (d *Docker) RemoveAttempt(ctx context.Context, attemptID string) error {
	filter := client.Filters{}.Add("label", LabelAttemptID+"="+attemptID)
	response, err := d.client.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filter})
	if err != nil { return err }
	for _, item := range response.Items {
		if _, err := d.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true}); err != nil { return err }
	}
	return nil
}

func (d *Docker) RemoveContainer(ctx context.Context, containerID string) error {
	_, err := d.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
	return err
}

func (d *Docker) ContainersForProject(ctx context.Context, project string) (map[string]string, error) {
	filter := client.Filters{}.Add("label", "com.docker.compose.project="+project)
	response, err := d.client.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filter})
	if err != nil { return nil, err }
	result := make(map[string]string)
	for _, item := range response.Items {
		service := item.Labels["com.docker.compose.service"]
		if service != "" { result[service] = item.ID }
	}
	return result, nil
}

func (d *Docker) UpdateNUMA(ctx context.Context, containerID string, numaNodes []int) error {
	resources := &container.Resources{CpusetMems: inventory.FormatCPUList(numaNodes)}
	_, err := d.client.ContainerUpdate(ctx, containerID, client.ContainerUpdateOptions{Resources: resources})
	return err
}

func (d *Docker) verifyResources(ctx context.Context, containerID string, plan ServicePlan) error {
	response, err := d.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil { return err }
	info := response.Container
	wantCPU := inventory.FormatCPUList(plan.CPUIds)
	wantNUMA := inventory.FormatCPUList(plan.NUMANodes)
	if info.HostConfig.CpusetCpus != wantCPU {
		return fmt.Errorf("container CPU mismatch: want %s, got %s", wantCPU, info.HostConfig.CpusetCpus)
	}
	if info.HostConfig.CpusetMems != wantNUMA {
		return fmt.Errorf("container NUMA memory mismatch: want %s, got %s", wantNUMA, info.HostConfig.CpusetMems)
	}
	if info.HostConfig.Memory != int64(plan.MemoryBytes) {
		return fmt.Errorf("container memory mismatch: want %d, got %d", plan.MemoryBytes, info.HostConfig.Memory)
	}
	if len(plan.GPUUUIDs) > 0 {
		actual := make(map[string]struct{})
		for _, deviceRequest := range info.HostConfig.DeviceRequests {
			for _, id := range deviceRequest.DeviceIDs { actual[id] = struct{}{} }
		}
		for _, uuid := range plan.GPUUUIDs {
			if _, ok := actual[uuid]; !ok { return fmt.Errorf("container missing GPU %s", uuid) }
		}
	}
	return nil
}

