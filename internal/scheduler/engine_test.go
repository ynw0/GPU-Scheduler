package scheduler

import (
	"reflect"
	"testing"

	"gpu-scheduler/internal/domain"
)

func TestPlanPrefersSingleNUMAAndCompleteCores(t *testing.T) {
	topology, availability := schedulingFixture()
	plan := New().Plan(topology, availability, domain.ResourceRequest{
		CPUMode: domain.CPUAutoCores, CPUCount: 2,
		GPUMode: domain.GPUAuto, GPUCount: 2,
		MemoryBytes: 64 << 30, NUMAPolicy: "strict",
	})
	if !plan.Schedulable {
		t.Fatalf("unexpected conflicts: %v", plan.Conflicts)
	}
	if !reflect.DeepEqual(plan.GPUUUIDs, []string{"GPU-0", "GPU-1"}) {
		t.Fatalf("gpus: %v", plan.GPUUUIDs)
	}
	if !reflect.DeepEqual(plan.CPUIds, []int{0, 1, 4, 5}) {
		t.Fatalf("cpus: %v", plan.CPUIds)
	}
	if !reflect.DeepEqual(plan.NUMANodes, []int{0}) {
		t.Fatalf("nodes: %v", plan.NUMANodes)
	}
}

func TestPlanRejectsRemoteManualCPU(t *testing.T) {
	topology, availability := schedulingFixture()
	plan := New().Plan(topology, availability, domain.ResourceRequest{
		CPUMode: domain.CPUManualThreads, CPUCount: 2, CPUIds: []int{2, 6},
		GPUMode: domain.GPUManual, GPUCount: 1, GPUUUIDs: []string{"GPU-0"},
		MemoryBytes: 1 << 30, NUMAPolicy: "strict",
	})
	if plan.Schedulable {
		t.Fatal("expected strict NUMA conflict")
	}
}

func TestPlanSelectsMIGInstanceByProfile(t *testing.T) {
	topology, availability := migFixture()
	plan := New().Plan(topology, availability, domain.ResourceRequest{
		CPUMode: domain.CPUAutoCores, CPUCount: 1,
		GPUKind: domain.GPUKindMIG, GPUMode: domain.GPUAuto, GPUCount: 1, MigProfile: "2g.20gb",
		MemoryBytes: 1 << 30, NUMAPolicy: "strict",
	})
	if !plan.Schedulable {
		t.Fatalf("unexpected conflicts: %v", plan.Conflicts)
	}
	if !reflect.DeepEqual(plan.GPUUUIDs, []string{"MIG-0-2g"}) {
		t.Fatalf("gpus: %v", plan.GPUUUIDs)
	}
}

func TestPlanRejectsWholeCardOnMIGParent(t *testing.T) {
	topology, availability := migFixture()
	plan := New().Plan(topology, availability, domain.ResourceRequest{
		CPUMode: domain.CPUAutoCores, CPUCount: 1,
		GPUMode: domain.GPUManual, GPUCount: 1, GPUUUIDs: []string{"GPU-0"},
		MemoryBytes: 1 << 30, NUMAPolicy: "strict",
	})
	if plan.Schedulable {
		t.Fatal("expected MIG parent to be rejected as a whole card")
	}
}

func schedulingFixture() (domain.Topology, domain.Availability) {
	cpus := []domain.LogicalCPU{
		{ID: 0, CoreID: 0, SocketID: 0, NUMANode: 0, Siblings: []int{0, 4}, Allocatable: true},
		{ID: 1, CoreID: 1, SocketID: 0, NUMANode: 0, Siblings: []int{1, 5}, Allocatable: true},
		{ID: 2, CoreID: 2, SocketID: 1, NUMANode: 1, Siblings: []int{2, 6}, Allocatable: true},
		{ID: 3, CoreID: 3, SocketID: 1, NUMANode: 1, Siblings: []int{3, 7}, Allocatable: true},
		{ID: 4, CoreID: 0, SocketID: 0, NUMANode: 0, Siblings: []int{0, 4}, Allocatable: true},
		{ID: 5, CoreID: 1, SocketID: 0, NUMANode: 0, Siblings: []int{1, 5}, Allocatable: true},
		{ID: 6, CoreID: 2, SocketID: 1, NUMANode: 1, Siblings: []int{2, 6}, Allocatable: true},
		{ID: 7, CoreID: 3, SocketID: 1, NUMANode: 1, Siblings: []int{3, 7}, Allocatable: true},
	}
	gpus := []domain.GPU{
		{UUID: "GPU-0", Index: 0, NUMANode: 0, Healthy: true},
		{UUID: "GPU-1", Index: 1, NUMANode: 0, Healthy: true},
		{UUID: "GPU-2", Index: 2, NUMANode: 1, Healthy: true},
		{UUID: "GPU-3", Index: 3, NUMANode: 1, Healthy: true},
	}
	return domain.Topology{CPUs: cpus, GPUs: gpus}, domain.Availability{
		CPUIds: []int{0, 1, 2, 3, 4, 5, 6, 7}, GPUUUIDs: []string{"GPU-0", "GPU-1", "GPU-2", "GPU-3"},
		MemoryByNUMA: map[int]uint64{0: 128 << 30, 1: 128 << 30},
	}
}

func migFixture() (domain.Topology, domain.Availability) {
	cpus := []domain.LogicalCPU{
		{ID: 0, CoreID: 0, SocketID: 0, NUMANode: 0, Siblings: []int{0, 1}, Allocatable: true},
		{ID: 1, CoreID: 0, SocketID: 0, NUMANode: 0, Siblings: []int{0, 1}, Allocatable: true},
	}
	gpus := []domain.GPU{
		{UUID: "GPU-0", Index: 0, NUMANode: 0, Healthy: true, MigEnabled: true, MemoryBytes: 80 << 30},
		{UUID: "MIG-0-2g", Index: 0, NUMANode: 0, Healthy: true, ParentUUID: "GPU-0", Profile: "2g.20gb"},
		{UUID: "MIG-0-1g", Index: 0, NUMANode: 0, Healthy: true, ParentUUID: "GPU-0", Profile: "1g.10gb"},
		{UUID: "GPU-1", Index: 1, NUMANode: 0, Healthy: true, MemoryBytes: 80 << 30},
	}
	return domain.Topology{CPUs: cpus, GPUs: gpus}, domain.Availability{
		CPUIds: []int{0, 1}, GPUUUIDs: []string{"MIG-0-2g", "MIG-0-1g", "GPU-1"},
		MemoryByNUMA: map[int]uint64{0: 128 << 30},
	}
}
