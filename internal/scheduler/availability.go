package scheduler

import (
	"strconv"

	"gpu-scheduler/internal/domain"
)

func BuildAvailability(topology domain.Topology, allocations []domain.Allocation, reservedCPUIds []int, hostGPUProcesses []string) domain.Availability {
	blockedCPUs := intSet(reservedCPUIds)
	blockedGPUs := stringSet(hostGPUProcesses)
	usedMemory := make(map[int]uint64)
	for _, allocation := range allocations {
		if allocation.State == domain.AllocationReleased {
			continue
		}
		switch allocation.ResourceType {
		case "cpu":
			if id, err := strconv.Atoi(allocation.ResourceID); err == nil { blockedCPUs[id] = struct{}{} }
		case "gpu":
			blockedGPUs[allocation.ResourceID] = struct{}{}
		case "memory_node":
			if id, err := strconv.Atoi(allocation.ResourceID); err == nil { usedMemory[id] += allocation.AmountBytes }
		}
	}
	availability := domain.Availability{MemoryByNUMA: make(map[int]uint64)}
	for _, cpu := range topology.CPUs {
		_, blocked := blockedCPUs[cpu.ID]
		if cpu.Allocatable && !blocked {
			availability.CPUIds = append(availability.CPUIds, cpu.ID)
		}
	}
	for _, gpu := range topology.GPUs {
		_, blocked := blockedGPUs[gpu.UUID]
		if gpu.Healthy && !blocked {
			availability.GPUUUIDs = append(availability.GPUUUIDs, gpu.UUID)
		}
	}
	for _, node := range topology.NUMANodes {
		if usedMemory[node.ID] >= node.MemoryBytes {
			availability.MemoryByNUMA[node.ID] = 0
		} else {
			availability.MemoryByNUMA[node.ID] = node.MemoryBytes - usedMemory[node.ID]
		}
	}
	return availability
}
