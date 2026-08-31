package scheduler

import (
	"cmp"
	"fmt"
	"slices"
	"sort"

	"gpu-scheduler/internal/domain"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Plan(topology domain.Topology, availability domain.Availability, request domain.ResourceRequest) domain.JobPlan {
	plan := domain.JobPlan{Conflicts: []string{}, Warnings: []string{}}
	if request.NUMAPolicy != "strict" {
		plan.Conflicts = append(plan.Conflicts, "只支持 strict NUMA 策略")
		return plan
	}
	availableGPU := stringSet(availability.GPUUUIDs)
	selectedGPUs, conflicts := selectGPUs(topology.GPUs, availableGPU, request)
	plan.Conflicts = append(plan.Conflicts, conflicts...)
	selectedNodes := nodeSetFromGPUs(selectedGPUs)

	availableCPU := intSet(availability.CPUIds)
	selectedCPUs, cpuNodes, conflicts := selectCPUs(topology.CPUs, availableCPU, selectedNodes, request)
	plan.Conflicts = append(plan.Conflicts, conflicts...)
	if len(selectedNodes) == 0 {
		selectedNodes = cpuNodes
	}
	if request.MemoryBytes > 0 {
		var availableMemory uint64
		for node := range selectedNodes {
			availableMemory += availability.MemoryByNUMA[node]
		}
		if availableMemory < request.MemoryBytes {
			plan.Conflicts = append(plan.Conflicts,
				fmt.Sprintf("NUMA 本地可用内存不足：需要 %d 字节，可用 %d 字节", request.MemoryBytes, availableMemory))
		}
	}
	for _, gpu := range selectedGPUs {
		plan.GPUUUIDs = append(plan.GPUUUIDs, gpu.UUID)
	}
	plan.CPUIds = selectedCPUs
	for node := range selectedNodes {
		plan.NUMANodes = append(plan.NUMANodes, node)
	}
	sort.Ints(plan.CPUIds)
	sort.Ints(plan.NUMANodes)
	plan.Schedulable = len(plan.Conflicts) == 0
	return plan
}

func selectGPUs(gpus []domain.GPU, available map[string]struct{}, request domain.ResourceRequest) ([]domain.GPU, []string) {
	if request.GPUCount == 0 {
		return nil, nil
	}
	byUUID := make(map[string]domain.GPU, len(gpus))
	for _, gpu := range gpus {
		byUUID[gpu.UUID] = gpu
	}
	if request.GPUMode == domain.GPUManual {
		if len(request.GPUUUIDs) != request.GPUCount {
			return nil, []string{"手动 GPU 数量与 gpuCount 不一致"}
		}
		selected := make([]domain.GPU, 0, len(request.GPUUUIDs))
		for _, uuid := range request.GPUUUIDs {
			gpu, exists := byUUID[uuid]
			if !exists {
				return nil, []string{fmt.Sprintf("GPU %s 不存在", uuid)}
			}
			if _, free := available[uuid]; !free || !gpu.Healthy {
				return nil, []string{fmt.Sprintf("GPU %s 当前不可分配", uuid)}
			}
			selected = append(selected, gpu)
		}
		return selected, nil
	}
	candidates := make([]domain.GPU, 0)
	for _, gpu := range gpus {
		_, free := available[gpu.UUID]
		if free && gpu.Healthy {
			candidates = append(candidates, gpu)
		}
	}
	if len(candidates) < request.GPUCount {
		return nil, []string{fmt.Sprintf("可用 GPU 不足：需要 %d，可用 %d", request.GPUCount, len(candidates))}
	}
	combinations := combinations(candidates, request.GPUCount)
	slices.SortFunc(combinations, func(a, b []domain.GPU) int {
		if value := cmp.Compare(len(nodeSetFromGPUs(a)), len(nodeSetFromGPUs(b))); value != 0 {
			return value
		}
		for i := range a {
			if value := cmp.Compare(a[i].Index, b[i].Index); value != 0 {
				return value
			}
		}
		return 0
	})
	return combinations[0], nil
}

func selectCPUs(cpus []domain.LogicalCPU, available map[int]struct{}, allowedNodes map[int]struct{}, request domain.ResourceRequest) ([]int, map[int]struct{}, []string) {
	byID := make(map[int]domain.LogicalCPU, len(cpus))
	for _, cpu := range cpus {
		byID[cpu.ID] = cpu
	}
	if request.CPUMode == domain.CPUManualThreads {
		if len(request.CPUIds) != request.CPUCount {
			return nil, nil, []string{"手动 CPU 线程数量与 cpuCount 不一致"}
		}
		nodes := make(map[int]struct{})
		for _, id := range request.CPUIds {
			cpu, exists := byID[id]
			if !exists || !cpu.Allocatable {
				return nil, nil, []string{fmt.Sprintf("CPU %d 不存在或不可用", id)}
			}
			if _, free := available[id]; !free {
				return nil, nil, []string{fmt.Sprintf("CPU %d 已被占用", id)}
			}
			if len(allowedNodes) > 0 {
				if _, local := allowedNodes[cpu.NUMANode]; !local {
					return nil, nil, []string{fmt.Sprintf("CPU %d 不属于所选 GPU 的 NUMA 节点", id)}
				}
			}
			nodes[cpu.NUMANode] = struct{}{}
		}
		return append([]int(nil), request.CPUIds...), nodes, nil
	}
	if request.CPUCount == 0 {
		return nil, map[int]struct{}{}, nil
	}
	type core struct {
		socket int
		id     int
		node   int
		cpus   []int
	}
	coresByKey := make(map[[2]int]*core)
	for _, cpu := range cpus {
		if !cpu.Allocatable {
			continue
		}
		if len(allowedNodes) > 0 {
			if _, local := allowedNodes[cpu.NUMANode]; !local {
				continue
			}
		}
		if _, free := available[cpu.ID]; !free {
			continue
		}
		key := [2]int{cpu.SocketID, cpu.CoreID}
		entry := coresByKey[key]
		if entry == nil {
			entry = &core{socket: cpu.SocketID, id: cpu.CoreID, node: cpu.NUMANode}
			coresByKey[key] = entry
		}
		entry.cpus = append(entry.cpus, cpu.ID)
	}
	cores := make([]core, 0, len(coresByKey))
	for key, candidate := range coresByKey {
		complete := true
		for _, cpu := range cpus {
			if cpu.SocketID == key[0] && cpu.CoreID == key[1] {
				if _, free := available[cpu.ID]; !free || !cpu.Allocatable {
					complete = false
				}
			}
		}
		if complete {
			sort.Ints(candidate.cpus)
			cores = append(cores, *candidate)
		}
	}
	sort.Slice(cores, func(i, j int) bool {
		if cores[i].node != cores[j].node {
			return cores[i].node < cores[j].node
		}
		if cores[i].socket != cores[j].socket {
			return cores[i].socket < cores[j].socket
		}
		return cores[i].id < cores[j].id
	})
	if len(cores) < request.CPUCount {
		return nil, nil, []string{fmt.Sprintf("完整可用物理核不足：需要 %d，可用 %d", request.CPUCount, len(cores))}
	}
	selected := make([]int, 0)
	nodes := make(map[int]struct{})
	for _, candidate := range cores[:request.CPUCount] {
		selected = append(selected, candidate.cpus...)
		nodes[candidate.node] = struct{}{}
	}
	return selected, nodes, nil
}

func combinations(values []domain.GPU, count int) [][]domain.GPU {
	var result [][]domain.GPU
	var visit func(int, []domain.GPU)
	visit = func(start int, current []domain.GPU) {
		if len(current) == count {
			result = append(result, append([]domain.GPU(nil), current...))
			return
		}
		for i := start; i <= len(values)-(count-len(current)); i++ {
			visit(i+1, append(current, values[i]))
		}
	}
	visit(0, nil)
	return result
}

func nodeSetFromGPUs(gpus []domain.GPU) map[int]struct{} {
	nodes := make(map[int]struct{})
	for _, gpu := range gpus {
		nodes[gpu.NUMANode] = struct{}{}
	}
	return nodes
}

func intSet(values []int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
