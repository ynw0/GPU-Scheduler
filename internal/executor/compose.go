package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gpu-scheduler/internal/dockerhost"
)

type Compose struct {
	docker     *Docker
	validator  *SecurityValidator
	stateDir   string
	socketPath string
}

type EffectiveCompose struct {
	Path             string
	SourceDigest     string
	NormalizedDigest string
}

func NewCompose(docker *Docker, validator *SecurityValidator, stateDir, socketPath string) *Compose {
	return &Compose{docker: docker, validator: validator, stateDir: stateDir, socketPath: socketPath}
}

func (c *Compose) Prepare(ctx context.Context, request ComposeCreateRequest) (EffectiveCompose, error) {
	root, entry, err := resolveComposeSource(request.Source)
	if err != nil {
		return EffectiveCompose{}, err
	}
	sourceBytes, err := os.ReadFile(entry)
	if err != nil {
		return EffectiveCompose{}, err
	}
	command, err := dockerhost.Command(ctx, c.socketPath, "compose", "--project-directory", root, "-f", entry, "config", "--format", "json")
	if err != nil {
		return EffectiveCompose{}, err
	}
	normalized, err := command.Output()
	if err != nil {
		return EffectiveCompose{}, fmt.Errorf("normalize compose: %w", err)
	}
	var model map[string]any
	if err := json.Unmarshal(normalized, &model); err != nil {
		return EffectiveCompose{}, fmt.Errorf("decode normalized compose: %w", err)
	}
	if err := c.validator.ValidateCompose(model); err != nil {
		return EffectiveCompose{}, err
	}
	services, ok := model["services"].(map[string]any)
	if !ok {
		return EffectiveCompose{}, fmt.Errorf("normalized compose has no services")
	}
	for name := range services {
		plan, exists := request.Plans[name]
		if !exists {
			return EffectiveCompose{}, fmt.Errorf("service %s has no resource classification", name)
		}
		service, ok := services[name].(map[string]any)
		if !ok {
			return EffectiveCompose{}, fmt.Errorf("service %s is malformed", name)
		}
		applyServicePlan(service, plan, request.JobID, request.AttemptID)
	}
	for name := range request.Plans {
		if _, exists := services[name]; !exists {
			return EffectiveCompose{}, fmt.Errorf("resource plan references unknown service %s", name)
		}
	}
	model["name"] = request.ProjectName
	effectiveBytes, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return EffectiveCompose{}, err
	}
	directory := filepath.Join(c.stateDir, "effective-specs", request.AttemptID)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return EffectiveCompose{}, err
	}
	path := filepath.Join(directory, "compose.json")
	if err := os.WriteFile(path, effectiveBytes, 0o640); err != nil {
		return EffectiveCompose{}, err
	}
	return EffectiveCompose{Path: path, SourceDigest: digest(sourceBytes), NormalizedDigest: digest(effectiveBytes)}, nil
}

func (c *Compose) CreateAndStart(ctx context.Context, request ComposeCreateRequest, effective EffectiveCompose) (RuntimeRef, error) {
	create, err := dockerhost.Command(ctx, c.socketPath, "compose", "-p", request.ProjectName, "-f", effective.Path, "create")
	if err != nil {
		return RuntimeRef{}, err
	}
	if output, err := create.CombinedOutput(); err != nil {
		_ = c.cleanupProject(ctx, request.ProjectName)
		return RuntimeRef{}, fmt.Errorf("compose create: %w: %s", err, strings.TrimSpace(string(output)))
	}
	containers, err := c.docker.ContainersForProject(ctx, request.ProjectName)
	if err != nil {
		_ = c.cleanupProject(ctx, request.ProjectName)
		return RuntimeRef{}, err
	}
	if len(containers) != len(request.Plans) {
		_ = c.cleanupProject(ctx, request.ProjectName)
		return RuntimeRef{}, fmt.Errorf("compose created %d services, expected %d", len(containers), len(request.Plans))
	}
	for service, containerID := range containers {
		plan := request.Plans[service]
		if err := c.docker.UpdateNUMA(ctx, containerID, plan.NUMANodes); err != nil {
			_ = c.cleanupProject(ctx, request.ProjectName)
			return RuntimeRef{}, err
		}
		if err := c.docker.verifyResources(ctx, containerID, plan); err != nil {
			_ = c.cleanupProject(ctx, request.ProjectName)
			return RuntimeRef{}, err
		}
	}
	start, err := dockerhost.Command(ctx, c.socketPath, "compose", "-p", request.ProjectName, "-f", effective.Path, "start")
	if err != nil {
		_ = c.cleanupProject(ctx, request.ProjectName)
		return RuntimeRef{}, err
	}
	if output, err := start.CombinedOutput(); err != nil {
		_ = c.cleanupProject(ctx, request.ProjectName)
		return RuntimeRef{}, fmt.Errorf("compose start: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return RuntimeRef{Kind: "compose", Project: request.ProjectName, ContainerIDs: containers}, nil
}

func (c *Compose) cleanupProject(ctx context.Context, project string) error {
	containers, err := c.docker.ContainersForProject(ctx, project)
	if err != nil {
		return err
	}
	for _, id := range containers {
		if err := c.docker.RemoveContainer(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func resolveComposeSource(source ComposeSource) (string, string, error) {
	root, err := filepath.Abs(source.RootPath)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	entry := filepath.Join(root, source.EntryFile)
	entry, err = filepath.EvalSymlinks(entry)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, entry)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("compose entry escapes registered root")
	}
	return root, entry, nil
}

func applyServicePlan(service map[string]any, plan ServicePlan, jobID, attemptID string) {
	service["cpuset"] = formatIntList(plan.CPUIds)
	service["mem_limit"] = plan.MemoryBytes
	delete(service, "memswap_limit")
	labels, _ := service["labels"].(map[string]any)
	if labels == nil {
		labels = make(map[string]any)
	}
	labels[LabelManaged] = "true"
	labels[LabelJobID] = jobID
	labels[LabelAttemptID] = attemptID
	service["labels"] = labels
	deploy, _ := service["deploy"].(map[string]any)
	if deploy == nil {
		deploy = make(map[string]any)
	}
	resources, _ := deploy["resources"].(map[string]any)
	if resources == nil {
		resources = make(map[string]any)
	}
	reservations, _ := resources["reservations"].(map[string]any)
	if reservations == nil {
		reservations = make(map[string]any)
	}
	if len(plan.GPUUUIDs) > 0 {
		reservations["devices"] = []any{map[string]any{
			"driver": "nvidia", "device_ids": plan.GPUUUIDs, "capabilities": []any{"gpu"},
		}}
	} else {
		delete(reservations, "devices")
	}
	resources["reservations"] = reservations
	deploy["resources"] = resources
	service["deploy"] = deploy
	delete(service, "gpus")
}

func formatIntList(values []int) string {
	values = append([]int(nil), values...)
	sort.Ints(values)
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
