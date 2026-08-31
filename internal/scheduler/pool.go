package scheduler

import (
	"fmt"

	"gpu-scheduler/internal/domain"
)

func ValidatePool(topology domain.Topology, pool domain.ResourcePool, existing []domain.ResourcePool) error {
	if pool.Name == "" {
		return fmt.Errorf("resource pool name is required")
	}
	if len(pool.CPUIds) == 0 && len(pool.GPUUUIDs) == 0 {
		return fmt.Errorf("resource pool must include CPU or GPU")
	}
	cpuByID := map[int]domain.LogicalCPU{}
	for _, cpu := range topology.CPUs {
		cpuByID[cpu.ID] = cpu
	}
	gpuByUUID := map[string]domain.GPU{}
	for _, gpu := range topology.GPUs {
		gpuByUUID[gpu.UUID] = gpu
	}
	nodeOK := map[int]struct{}{}
	for _, node := range topology.NUMANodes {
		nodeOK[node.ID] = struct{}{}
	}
	listedCPUs := intSet(pool.CPUIds)
	for _, id := range pool.CPUIds {
		cpu, ok := cpuByID[id]
		if !ok {
			return fmt.Errorf("CPU %d is not in topology", id)
		}
		if !cpu.Allocatable {
			return fmt.Errorf("CPU %d is not allocatable", id)
		}
		for _, sibling := range cpu.Siblings {
			if _, listed := listedCPUs[sibling]; !listed {
				return fmt.Errorf("CPU %d is missing sibling thread %d", id, sibling)
			}
		}
	}
	for _, uuid := range pool.GPUUUIDs {
		gpu, ok := gpuByUUID[uuid]
		if !ok {
			return fmt.Errorf("GPU %s is not in topology", uuid)
		}
		if !gpu.Healthy {
			return fmt.Errorf("GPU %s is not healthy", uuid)
		}
	}
	for _, node := range pool.NUMANodes {
		if _, ok := nodeOK[node]; !ok {
			return fmt.Errorf("NUMA node %d is not in topology", node)
		}
	}
	allowedNodes := intSet(pool.NUMANodes)
	if len(allowedNodes) == 0 {
		for _, cpu := range pool.CPUIds {
			allowedNodes[cpuByID[cpu].NUMANode] = struct{}{}
		}
		for _, uuid := range pool.GPUUUIDs {
			allowedNodes[gpuByUUID[uuid].NUMANode] = struct{}{}
		}
	}
	for _, id := range pool.CPUIds {
		if _, ok := allowedNodes[cpuByID[id].NUMANode]; !ok {
			return fmt.Errorf("CPU %d is outside pool NUMA nodes", id)
		}
	}
	for _, uuid := range pool.GPUUUIDs {
		if _, ok := allowedNodes[gpuByUUID[uuid].NUMANode]; !ok {
			return fmt.Errorf("GPU %s is outside pool NUMA nodes", uuid)
		}
	}
	if pool.Shared {
		return nil
	}
	cpuSet, gpuSet := intSet(pool.CPUIds), stringSet(pool.GPUUUIDs)
	for _, other := range existing {
		if other.ID == pool.ID || other.Shared {
			continue
		}
		for _, id := range other.CPUIds {
			if _, ok := cpuSet[id]; ok {
				return fmt.Errorf("CPU %d already belongs to pool %s", id, other.Name)
			}
		}
		for _, uuid := range other.GPUUUIDs {
			if _, ok := gpuSet[uuid]; ok {
				return fmt.Errorf("GPU %s already belongs to pool %s", uuid, other.Name)
			}
		}
	}
	return nil
}
