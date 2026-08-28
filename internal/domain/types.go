package domain

import "time"

type Role string

const (
	RoleViewer       Role = "Viewer"
	RoleUser         Role = "User"
	RoleTeamOperator Role = "TeamOperator"
	RoleAdmin        Role = "Admin"
	RoleAuditor      Role = "Auditor"
)

type Priority string

const (
	PriorityEmergency Priority = "P0"
	PriorityHigh      Priority = "P1"
	PriorityNormal    Priority = "P2"
	PriorityLow       Priority = "P3"
)

type JobState string

const (
	JobDraft      JobState = "DRAFT"
	JobQueued     JobState = "QUEUED"
	JobReserved   JobState = "RESERVED"
	JobCreating   JobState = "CREATING"
	JobStarting   JobState = "STARTING"
	JobRunning    JobState = "RUNNING"
	JobStopping   JobState = "STOPPING"
	JobSucceeded  JobState = "SUCCEEDED"
	JobFailed     JobState = "FAILED"
	JobCanceled   JobState = "CANCELED"
	JobDrifted    JobState = "DRIFTED"
	JobOrphaned   JobState = "ORPHANED"
	JobDegraded   JobState = "DEGRADED"
)

type AllocationState string

const (
	AllocationReserved  AllocationState = "RESERVED"
	AllocationActive    AllocationState = "ACTIVE"
	AllocationReleasing AllocationState = "RELEASING"
	AllocationReleased  AllocationState = "RELEASED"
)

type WorkloadKind string

const (
	WorkloadImage   WorkloadKind = "image"
	WorkloadCompose WorkloadKind = "compose"
)

type CPUMode string

const (
	CPUAutoCores    CPUMode = "auto-cores"
	CPUManualThreads CPUMode = "manual-threads"
)

type GPUMode string

const (
	GPUAuto   GPUMode = "auto"
	GPUManual GPUMode = "manual"
)

type LogicalCPU struct {
	ID          int   `json:"id"`
	CoreID      int   `json:"coreId"`
	SocketID    int   `json:"socketId"`
	NUMANode    int   `json:"numaNode"`
	Siblings    []int `json:"siblings"`
	Allocatable bool  `json:"allocatable"`
}

type NUMANode struct {
	ID          int     `json:"id"`
	MemoryBytes uint64  `json:"memoryBytes"`
	CPUIds      []int   `json:"cpuIds"`
}

type GPU struct {
	UUID        string `json:"uuid"`
	Index       int    `json:"index"`
	Name        string `json:"name"`
	PCIBusID    string `json:"pciBusId"`
	NUMANode    int    `json:"numaNode"`
	MemoryBytes uint64 `json:"memoryBytes"`
	Healthy     bool   `json:"healthy"`
}

type ImageInfo struct {
	ID         string    `json:"id"`
	Repository string    `json:"repository"`
	Tag        string    `json:"tag"`
	SizeBytes  int64     `json:"sizeBytes"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Topology struct {
	Hostname   string       `json:"hostname"`
	CPUs       []LogicalCPU `json:"cpus"`
	NUMANodes  []NUMANode   `json:"numaNodes"`
	GPUs       []GPU        `json:"gpus"`
	ObservedAt time.Time    `json:"observedAt"`
}

type Availability struct {
	CPUIds       []int          `json:"cpuIds"`
	GPUUUIDs     []string       `json:"gpuUuids"`
	MemoryByNUMA map[int]uint64 `json:"memoryByNuma"`
}

type ResourceRequest struct {
	CPUMode     CPUMode `json:"cpuMode"`
	CPUCount    int     `json:"cpuCount"`
	CPUIds      []int   `json:"cpuIds,omitempty"`
	GPUMode     GPUMode `json:"gpuMode"`
	GPUCount    int     `json:"gpuCount"`
	GPUUUIDs    []string `json:"gpuUuids,omitempty"`
	MemoryBytes uint64   `json:"memoryBytes"`
	NUMAPolicy  string   `json:"numaPolicy"`
}

type JobSpec struct {
	Name             string          `json:"name"`
	Priority         Priority        `json:"priority"`
	Workload         WorkloadSpec    `json:"workload"`
	Resources        ResourceRequest `json:"resources"`
	StopGraceSeconds int             `json:"stopGraceSeconds"`
}

type WorkloadSpec struct {
	Kind             WorkloadKind          `json:"kind"`
	Image            string                `json:"image,omitempty"`
	Command          []string              `json:"command,omitempty"`
	Entrypoint       []string              `json:"entrypoint,omitempty"`
	WorkingDirectory string                `json:"workingDirectory,omitempty"`
	Environment      map[string]string     `json:"environment,omitempty"`
	Mounts           []Mount               `json:"mounts,omitempty"`
	ComposeSourceID  string                `json:"composeSourceId,omitempty"`
	Services         []ComposeService      `json:"services,omitempty"`
}

func (w WorkloadSpec) AsImage() ImageWorkload {
	return ImageWorkload{
		Image: w.Image, Command: w.Command, Entrypoint: w.Entrypoint,
		WorkingDirectory: w.WorkingDirectory, Environment: w.Environment, Mounts: w.Mounts,
	}
}

type ImageWorkload struct {
	Image            string            `json:"image"`
	Command          []string          `json:"command"`
	Entrypoint       []string          `json:"entrypoint,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Mounts           []Mount           `json:"mounts,omitempty"`
}

type ComposeWorkload struct {
	SourceID string           `json:"composeSourceId"`
	Services []ComposeService `json:"services"`
}

type ComposeService struct {
	Name      string          `json:"name"`
	Class     string          `json:"class"`
	Resources ResourceRequest `json:"resources"`
}

type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

type JobPlan struct {
	Schedulable bool     `json:"schedulable"`
	CPUIds      []int    `json:"cpuIds"`
	GPUUUIDs    []string `json:"gpuUuids"`
	NUMANodes   []int    `json:"numaNodes"`
	Conflicts   []string `json:"conflicts"`
	Warnings    []string `json:"warnings"`
}

type Job struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	State           JobState  `json:"state"`
	Priority        Priority  `json:"priority"`
	OwnerID         string    `json:"ownerId"`
	TeamID          string    `json:"teamId,omitempty"`
	Spec            JobSpec   `json:"spec"`
	BypassCount     int       `json:"bypassCount"`
	IdempotencyKey  string    `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Allocation struct {
	ID           string          `json:"id"`
	JobID        string          `json:"jobId"`
	ServiceName  string          `json:"serviceName,omitempty"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	AmountBytes  uint64          `json:"amountBytes,omitempty"`
	State        AllocationState `json:"state"`
}
