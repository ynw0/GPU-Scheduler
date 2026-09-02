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
	JobDraft     JobState = "DRAFT"
	JobQueued    JobState = "QUEUED"
	JobReserved  JobState = "RESERVED"
	JobCreating  JobState = "CREATING"
	JobStarting  JobState = "STARTING"
	JobRunning   JobState = "RUNNING"
	JobStopping  JobState = "STOPPING"
	JobSucceeded JobState = "SUCCEEDED"
	JobFailed    JobState = "FAILED"
	JobCanceled  JobState = "CANCELED"
	JobDrifted   JobState = "DRIFTED"
	JobOrphaned  JobState = "ORPHANED"
	JobDegraded  JobState = "DEGRADED"
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
	CPUAutoCores     CPUMode = "auto-cores"
	CPUManualThreads CPUMode = "manual-threads"
)

type GPUMode string

const (
	GPUAuto   GPUMode = "auto"
	GPUManual GPUMode = "manual"
)

type GPUKind string

const (
	GPUKindWhole GPUKind = "whole"
	GPUKindMIG   GPUKind = "mig"
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
	ID          int    `json:"id"`
	MemoryBytes uint64 `json:"memoryBytes"`
	CPUIds      []int  `json:"cpuIds"`
}

type GPU struct {
	UUID               string `json:"uuid"`
	Index              int    `json:"index"`
	Name               string `json:"name"`
	PCIBusID           string `json:"pciBusId"`
	NUMANode           int    `json:"numaNode"`
	MemoryBytes        uint64 `json:"memoryBytes"`
	MemoryUsedBytes    uint64 `json:"memoryUsedBytes"`
	Utilization        uint32 `json:"utilization"`
	TemperatureCelsius uint32 `json:"temperatureCelsius"`
	Healthy            bool   `json:"healthy"`
	MigEnabled         bool   `json:"migEnabled,omitempty"`
	ParentUUID         string `json:"parentUuid,omitempty"`
	Profile            string `json:"profile,omitempty"`
}

type MIGLayout struct {
	GPUUUID  string   `json:"gpuUuid"`
	Enabled  bool     `json:"enabled"`
	Profiles []string `json:"profiles"`
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
	CPUMode     CPUMode  `json:"cpuMode"`
	CPUCount    int      `json:"cpuCount"`
	CPUIds      []int    `json:"cpuIds,omitempty"`
	GPUKind     GPUKind  `json:"gpuKind,omitempty"`
	GPUMode     GPUMode  `json:"gpuMode"`
	GPUCount    int      `json:"gpuCount"`
	GPUUUIDs    []string `json:"gpuUuids,omitempty"`
	MigProfile  string   `json:"migProfile,omitempty"`
	MemoryBytes uint64   `json:"memoryBytes"`
	NUMAPolicy  string   `json:"numaPolicy"`
}

func (r ResourceRequest) EffectiveGPUKind() GPUKind {
	if r.GPUKind == GPUKindMIG {
		return GPUKindMIG
	}
	return GPUKindWhole
}

type JobSpec struct {
	Name             string          `json:"name"`
	Priority         Priority        `json:"priority"`
	Workload         WorkloadSpec    `json:"workload"`
	Resources        ResourceRequest `json:"resources"`
	ResourcePoolID   string          `json:"resourcePoolId,omitempty"`
	StopGraceSeconds int             `json:"stopGraceSeconds"`
}

type WorkloadSpec struct {
	Kind             WorkloadKind      `json:"kind"`
	Image            string            `json:"image,omitempty"`
	Command          []string          `json:"command,omitempty"`
	Entrypoint       []string          `json:"entrypoint,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Mounts           []Mount           `json:"mounts,omitempty"`
	User             string            `json:"user,omitempty"`
	ShmSizeBytes     uint64            `json:"shmSizeBytes,omitempty"`
	PublishedPorts   []PortPublish     `json:"publishedPorts,omitempty"`
	Network          string            `json:"network,omitempty"`
	ComposeSourceID  string            `json:"composeSourceId,omitempty"`
	Services         []ComposeService  `json:"services,omitempty"`
}

func (w WorkloadSpec) AsImage() ImageWorkload {
	return ImageWorkload{
		Image: w.Image, Command: w.Command, Entrypoint: w.Entrypoint,
		WorkingDirectory: w.WorkingDirectory, Environment: w.Environment, Mounts: w.Mounts,
		User: w.User, ShmSizeBytes: w.ShmSizeBytes, PublishedPorts: w.PublishedPorts, Network: w.Network,
	}
}

type ImageWorkload struct {
	Image            string            `json:"image"`
	Command          []string          `json:"command,omitempty"`
	Entrypoint       []string          `json:"entrypoint,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Mounts           []Mount           `json:"mounts,omitempty"`
	User             string            `json:"user,omitempty"`
	ShmSizeBytes     uint64            `json:"shmSizeBytes,omitempty"`
	PublishedPorts   []PortPublish     `json:"publishedPorts,omitempty"`
	Network          string            `json:"network,omitempty"`
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

type PortPublish struct {
	HostPort      uint16 `json:"hostPort"`
	ContainerPort uint16 `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
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
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	State          JobState  `json:"state"`
	Priority       Priority  `json:"priority"`
	OwnerID        string    `json:"ownerId"`
	TeamID         string    `json:"teamId,omitempty"`
	Spec           JobSpec   `json:"spec"`
	BypassCount    int       `json:"bypassCount"`
	LastError      string    `json:"lastError,omitempty"`
	IdempotencyKey string    `json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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

type GPUMetric struct {
	UUID               string  `json:"uuid"`
	UtilizationPercent uint32  `json:"utilizationPercent"`
	MemoryUsedBytes    uint64  `json:"memoryUsedBytes"`
	MemoryTotalBytes   uint64  `json:"memoryTotalBytes"`
	TemperatureCelsius uint32  `json:"temperatureCelsius"`
	PowerWatts         float64 `json:"powerWatts"`
}

type HostMetricSnapshot struct {
	SampledAt        time.Time       `json:"sampledAt"`
	CPUPercentByID   map[int]float64 `json:"cpuPercentById"`
	MemoryUsedBytes  uint64          `json:"memoryUsedBytes"`
	MemoryTotalBytes uint64          `json:"memoryTotalBytes"`
	SwapUsedBytes    uint64          `json:"swapUsedBytes"`
	SwapTotalBytes   uint64          `json:"swapTotalBytes"`
	GPUs             []GPUMetric     `json:"gpus"`
}

type ContainerMetricSnapshot struct {
	SampledAt         time.Time       `json:"sampledAt"`
	ContainerID       string          `json:"containerId"`
	CPUPercentByID    map[int]float64 `json:"cpuPercentById"`
	CPUUsageNS        uint64          `json:"cpuUsageNs"`
	CPUQuotaMicros    int64           `json:"cpuQuotaMicros"`
	CPUPeriodMicros   uint64          `json:"cpuPeriodMicros"`
	CPUQuotaCores     float64         `json:"cpuQuotaCores"`
	MemoryUsedBytes   uint64          `json:"memoryUsedBytes"`
	MemoryLimitBytes  uint64          `json:"memoryLimitBytes"`
	MemoryFailCount   uint64          `json:"memoryFailCount"`
	OOMUnder          bool            `json:"oomUnder"`
	CPUIds            []int           `json:"cpuIds"`
	NUMANodes         []int           `json:"numaNodes"`
	BlockIOReadBytes  uint64          `json:"blockIoReadBytes"`
	BlockIOWriteBytes uint64          `json:"blockIoWriteBytes"`
}

type JobMetricSnapshot struct {
	SampledAt         time.Time                 `json:"sampledAt"`
	CPUPercentByID    map[int]float64           `json:"cpuPercentById"`
	MemoryUsedBytes   uint64                    `json:"memoryUsedBytes"`
	MemoryLimitBytes  uint64                    `json:"memoryLimitBytes"`
	MemoryFailCount   uint64                    `json:"memoryFailCount"`
	OOMUnder          bool                      `json:"oomUnder"`
	BlockIOReadBytes  uint64                    `json:"blockIoReadBytes"`
	BlockIOWriteBytes uint64                    `json:"blockIoWriteBytes"`
	GPUs              []GPUMetric               `json:"gpus"`
	Containers        []ContainerMetricSnapshot `json:"containers"`
}

type ContainerClaim struct {
	ContainerID        string    `json:"containerId"`
	Name               string    `json:"name"`
	Image              string    `json:"image"`
	State              string    `json:"state"`
	ExitCode           int       `json:"exitCode,omitempty"`
	ManagedJobID       string    `json:"managedJobId,omitempty"`
	CPUIds             []int     `json:"cpuIds"`
	GPUUUIDs           []string  `json:"gpuUuids"`
	CPUUnbounded       bool      `json:"cpuUnbounded"`
	GPUUnbounded       bool      `json:"gpuUnbounded"`
	TrainingGPUUUIDs   []string  `json:"trainingGpuUuids,omitempty"`
	PID                int       `json:"pid,omitempty"`
	RuntimeHeartbeatAt time.Time `json:"runtimeHeartbeatAt"`
	DockerHealth       string    `json:"dockerHealth"`
	CPUActivity        bool      `json:"cpuActivity"`
	GPUActivity        bool      `json:"gpuActivity"`
	OOMUnder           bool      `json:"oomUnder"`
	HeartbeatMounted   bool      `json:"heartbeatMounted,omitempty"`
	MemoryUsedBytes    uint64    `json:"memoryUsedBytes,omitempty"`
	MemoryLimitBytes   uint64    `json:"memoryLimitBytes,omitempty"`
	CPUPercent         float64   `json:"cpuPercent"`
}

type DiscoveredContainer struct {
	ContainerID    string
	Name           string
	Image          string
	ManagedJobID   string
	State          string
	CPUIds         []int
	GPUUUIDs       []string
	CPUUnbounded   bool
	GPUUnbounded   bool
	Priority       Priority
	PriorityLocked bool
	LastTrainingAt *time.Time
	FirstSeenAt    time.Time
	ObservedAt     time.Time
}

type HostContainer struct {
	ContainerID      string      `json:"containerId"`
	Name             string      `json:"name"`
	Image            string      `json:"image"`
	State            string      `json:"state"`
	CPUIds           []int       `json:"cpuIds"`
	GPUUUIDs         []string    `json:"gpuUuids"`
	CPUUnbounded     bool        `json:"cpuUnbounded"`
	GPUUnbounded     bool        `json:"gpuUnbounded"`
	Training         bool        `json:"training"`
	TrainingGPUUUIDs []string    `json:"trainingGpuUuids"`
	MemoryUsedBytes  uint64      `json:"memoryUsedBytes,omitempty"`
	MemoryLimitBytes uint64      `json:"memoryLimitBytes,omitempty"`
	GPUs             []GPUMetric `json:"gpus,omitempty"`
	DockerHealth     string      `json:"dockerHealth"`
	CPUActivity      bool        `json:"cpuActivity"`
	GPUActivity      bool        `json:"gpuActivity"`
	CPUPercent       float64     `json:"cpuPercent"`
	OOMUnder         bool        `json:"oomUnder"`
	ManagedJobID     string      `json:"managedJobId,omitempty"`
	JobName          string      `json:"jobName,omitempty"`
	Priority         Priority    `json:"priority"`
	PriorityLocked   bool        `json:"priorityLocked"`
	LastTrainingAt   *time.Time  `json:"lastTrainingAt,omitempty"`
	FirstSeenAt      time.Time   `json:"firstSeenAt"`
	SilentSeconds    int64       `json:"silentSeconds"`
}

type JobAttempt struct {
	ID              string            `json:"id"`
	JobID           string            `json:"jobId"`
	AttemptNumber   int               `json:"attemptNumber"`
	State           string            `json:"state"`
	Kind            string            `json:"kind,omitempty"`
	Project         string            `json:"project,omitempty"`
	ContainerIDs    map[string]string `json:"containerIds,omitempty"`
	HeartbeatMisses int               `json:"heartbeatMisses,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	FinishedAt      *time.Time        `json:"finishedAt,omitempty"`
}

type RuntimeHeartbeat struct {
	ObservedAt   time.Time `json:"observedAt"`
	DockerHealth string    `json:"dockerHealth"`
	CPUActivity  bool      `json:"cpuActivity"`
	GPUActivity  bool      `json:"gpuActivity"`
	OOMUnder     bool      `json:"oomUnder"`
}

type JobDetail struct {
	Job
	Allocations []Allocation      `json:"allocations"`
	Attempt     *JobAttempt       `json:"attempt,omitempty"`
	Heartbeat   *TrainingStatus   `json:"heartbeat,omitempty"`
	Runtime     *RuntimeHeartbeat `json:"runtime,omitempty"`
}

type TrainingStatus struct {
	AttemptID   string    `json:"attemptId,omitempty"`
	ServiceName string    `json:"serviceName,omitempty"`
	Sequence    int       `json:"sequence,omitempty"`
	Phase       string    `json:"phase"`
	Epoch       *int      `json:"epoch,omitempty"`
	Step        *int      `json:"step,omitempty"`
	Progress    *float64  `json:"progress,omitempty"`
	ReportedAt  time.Time `json:"reportedAt"`
	AgeSeconds  int64     `json:"ageSeconds"`
}

type TrainingHeartbeatReport struct {
	AttemptID   string    `json:"attemptId"`
	ServiceName string    `json:"serviceName"`
	Sequence    int       `json:"sequence"`
	Phase       string    `json:"phase"`
	Epoch       *int      `json:"epoch,omitempty"`
	Step        *int      `json:"step,omitempty"`
	Progress    *float64  `json:"progress,omitempty"`
	ReceivedAt  time.Time `json:"receivedAt"`
}

type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Quota struct {
	ID               string `json:"id"`
	ScopeType        string `json:"scopeType"`
	ScopeID          string `json:"scopeId"`
	GPULimit         int    `json:"gpuLimit"`
	CPUThreadLimit   int    `json:"cpuThreadLimit"`
	MemoryBytesLimit uint64 `json:"memoryBytesLimit"`
}

type ComposeSource struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"rootPath"`
	EntryFile string    `json:"entryFile"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"ownerId"`
	Spec      JobSpec   `json:"spec"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ResourcePool struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CPUIds    []int     `json:"cpuIds"`
	GPUUUIDs  []string  `json:"gpuUuids"`
	NUMANodes []int     `json:"numaNodes"`
	Shared    bool      `json:"shared"`
	CreatedAt time.Time `json:"createdAt"`
}

type AuditEvent struct {
	ID         string    `json:"id"`
	ActorID    string    `json:"actorUserId,omitempty"`
	Action     string    `json:"action"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetId"`
	Detail     string    `json:"detail,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Notification struct {
	ID        string     `json:"id"`
	UserID    string     `json:"userId,omitempty"`
	Severity  string     `json:"severity"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type UserAccount struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	Role      Role      `json:"role"`
	TeamID    string    `json:"teamId,omitempty"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type StreamEvent struct {
	Type    string `json:"type"`
	At      string `json:"at"`
	Payload any    `json:"payload,omitempty"`
}
