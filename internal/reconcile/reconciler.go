package reconcile

import (
	"fmt"

	"gpu-scheduler/internal/domain"
)

type Drift struct {
	Kind        string `json:"kind"`
	ContainerID string `json:"containerId"`
	JobID       string `json:"jobId,omitempty"`
	Detail      string `json:"detail"`
}

func Compare(claims []domain.ContainerClaim, allocations []domain.Allocation, allCPUIds, allGPUUUIDs []string) ([]Drift, []string, []string) {
	expectedCPU := make(map[string]string)
	expectedGPU := make(map[string]string)
	for _, allocation := range allocations {
		if allocation.State == domain.AllocationReleased { continue }
		if allocation.ResourceType == "cpu" { expectedCPU[allocation.ResourceID] = allocation.JobID }
		if allocation.ResourceType == "gpu" { expectedGPU[allocation.ResourceID] = allocation.JobID }
	}
	blockedCPU := make(map[string]struct{})
	blockedGPU := make(map[string]struct{})
	var drifts []Drift
	for _, claim := range claims {
		if claim.ManagedJobID == "" {
			if claim.CPUUnbounded { for _, id := range allCPUIds { blockedCPU[id] = struct{}{} } }
			if claim.GPUUnbounded { for _, uuid := range allGPUUUIDs { blockedGPU[uuid] = struct{}{} } }
			for _, id := range claim.CPUIds { blockedCPU[fmt.Sprint(id)] = struct{}{} }
			for _, uuid := range claim.GPUUUIDs { blockedGPU[uuid] = struct{}{} }
			continue
		}
		for _, id := range claim.CPUIds {
			if owner := expectedCPU[fmt.Sprint(id)]; owner != claim.ManagedJobID {
				drifts = append(drifts, Drift{Kind: "cpu", ContainerID: claim.ContainerID, JobID: claim.ManagedJobID, Detail: fmt.Sprintf("CPU %d is not allocated to job", id)})
			}
		}
		for _, uuid := range claim.GPUUUIDs {
			if owner := expectedGPU[uuid]; owner != claim.ManagedJobID {
				drifts = append(drifts, Drift{Kind: "gpu", ContainerID: claim.ContainerID, JobID: claim.ManagedJobID, Detail: fmt.Sprintf("GPU %s is not allocated to job", uuid)})
			}
		}
	}
	return drifts, keys(blockedCPU), keys(blockedGPU)
}

func keys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values { result = append(result, value) }
	return result
}

