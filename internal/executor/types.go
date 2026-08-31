package executor

import "gpu-scheduler/internal/domain"

const (
	LabelManaged   = "io.gpu-scheduler.managed"
	LabelJobID     = "io.gpu-scheduler.job-id"
	LabelAttemptID = "io.gpu-scheduler.attempt-id"
)

type ServicePlan struct {
	Name        string
	CPUIds      []int
	GPUUUIDs    []string
	NUMANodes   []int
	MemoryBytes uint64
	Shared      bool
}

type ImageCreateRequest struct {
	JobID       string
	AttemptID   string
	Name        string
	Workload    domain.ImageWorkload
	Plan        ServicePlan
	StopTimeout int
}

type RuntimeRef struct {
	Kind         string            `json:"kind"`
	Project      string            `json:"project,omitempty"`
	ContainerIDs map[string]string `json:"containerIds"`
}

type ComposeSource struct {
	ID        string
	RootPath  string
	EntryFile string
}

type ComposeCreateRequest struct {
	JobID       string
	AttemptID   string
	ProjectName string
	Source      ComposeSource
	Plans       map[string]ServicePlan
}
