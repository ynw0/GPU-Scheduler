package quota

import (
	"fmt"

	"gpu-scheduler/internal/domain"
)

type Limit struct {
	GPUCount       int
	CPUThreadCount int
	MemoryBytes    uint64
}

type Usage struct {
	GPUCount       int
	CPUThreadCount int
	MemoryBytes    uint64
}

func Check(request domain.ResourceRequest, plannedCPUThreads int, teamLimit, userLimit Limit, teamUsage, userUsage Usage) []string {
	requested := Usage{GPUCount: request.GPUCount, CPUThreadCount: plannedCPUThreads, MemoryBytes: request.MemoryBytes}
	var conflicts []string
	conflicts = append(conflicts, checkScope("团队", teamLimit, teamUsage, requested)...)
	conflicts = append(conflicts, checkScope("用户", userLimit, userUsage, requested)...)
	return conflicts
}

func checkScope(scope string, limit Limit, usage, request Usage) []string {
	var conflicts []string
	if limit.GPUCount >= 0 && usage.GPUCount+request.GPUCount > limit.GPUCount {
		conflicts = append(conflicts, fmt.Sprintf("%s GPU 配额不足", scope))
	}
	if limit.CPUThreadCount >= 0 && usage.CPUThreadCount+request.CPUThreadCount > limit.CPUThreadCount {
		conflicts = append(conflicts, fmt.Sprintf("%s CPU 配额不足", scope))
	}
	if limit.MemoryBytes > 0 && usage.MemoryBytes+request.MemoryBytes > limit.MemoryBytes {
		conflicts = append(conflicts, fmt.Sprintf("%s内存配额不足", scope))
	}
	return conflicts
}
