package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/store"
)

type ReservationService struct {
	store  *store.Store
	engine *Engine
}

type CreateInput struct {
	OwnerID        string
	TeamID         string
	IdempotencyKey string
	Spec           domain.JobSpec
	Topology       domain.Topology
	Availability   domain.Availability
}

type ReservationResult struct {
	Job          domain.Job
	Plan         domain.JobPlan
	ServicePlans map[string]domain.JobPlan
}

func NewReservationService(store *store.Store, engine *Engine) *ReservationService {
	return &ReservationService{store: store, engine: engine}
}

func (s *ReservationService) Preview(topology domain.Topology, availability domain.Availability, spec domain.JobSpec) domain.JobPlan {
	plan, _ := s.planWorkload(topology, availability, spec)
	return plan
}

func (s *ReservationService) Create(ctx context.Context, input CreateInput) (ReservationResult, error) {
	aggregate, servicePlans := s.planWorkload(input.Topology, input.Availability, input.Spec)
	if !aggregate.Schedulable { return ReservationResult{Plan: aggregate}, fmt.Errorf("resource plan is not schedulable") }
	now := time.Now().UTC()
	job := domain.Job{
		ID: uuid.NewString(), Name: input.Spec.Name, State: domain.JobReserved,
		Priority: input.Spec.Priority, OwnerID: input.OwnerID, TeamID: input.TeamID,
		Spec: input.Spec, IdempotencyKey: input.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
	}
	err := s.store.Immediate(ctx, func(conn *sql.Conn) error {
		var existing string
		err := conn.QueryRowContext(ctx,
			"SELECT id FROM jobs WHERE owner_id=? AND idempotency_key=?", input.OwnerID, input.IdempotencyKey).Scan(&existing)
		if err == nil { return fmt.Errorf("idempotency key already belongs to job %s", existing) }
		if err != sql.ErrNoRows { return err }
		specJSON, err := json.Marshal(input.Spec)
		if err != nil { return err }
		_, err = conn.ExecContext(ctx, `INSERT INTO jobs
            (id,name,owner_id,team_id,state,priority,spec_json,idempotency_key,bypass_count,created_at,updated_at)
            VALUES(?,?,?,?,?,?,?,?,0,?,?)`,
			job.ID, job.Name, job.OwnerID, nullable(job.TeamID), job.State, job.Priority,
			string(specJSON), job.IdempotencyKey, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil { return err }
		for serviceName, plan := range servicePlans {
			request := resourceRequestForService(input.Spec, serviceName)
			if err := reservePlan(ctx, conn, job.ID, serviceName, plan, request, input.Topology, now); err != nil { return err }
		}
		return nil
	})
	return ReservationResult{Job: job, Plan: aggregate, ServicePlans: servicePlans}, err
}

func (s *ReservationService) planWorkload(topology domain.Topology, availability domain.Availability, spec domain.JobSpec) (domain.JobPlan, map[string]domain.JobPlan) {
	plans := make(map[string]domain.JobPlan)
	if spec.Workload.Kind != domain.WorkloadCompose {
		plan := s.engine.Plan(topology, availability, spec.Resources)
		plans["main"] = plan
		return plan, plans
	}
	remaining := cloneAvailability(availability)
	aggregate := domain.JobPlan{Schedulable: true, Conflicts: []string{}, Warnings: []string{}}
	for _, service := range spec.Workload.Services {
		plan := s.engine.Plan(topology, remaining, service.Resources)
		plans[service.Name] = plan
		if !plan.Schedulable {
			aggregate.Schedulable = false
			for _, conflict := range plan.Conflicts { aggregate.Conflicts = append(aggregate.Conflicts, service.Name+": "+conflict) }
			continue
		}
		aggregate.CPUIds = append(aggregate.CPUIds, plan.CPUIds...)
		aggregate.GPUUUIDs = append(aggregate.GPUUUIDs, plan.GPUUUIDs...)
		aggregate.NUMANodes = appendUniqueInts(aggregate.NUMANodes, plan.NUMANodes...)
		consumeAvailability(&remaining, plan, service.Resources.MemoryBytes)
	}
	if len(spec.Workload.Services) == 0 {
		aggregate.Schedulable = false
		aggregate.Conflicts = append(aggregate.Conflicts, "Compose 至少需要一个已分类服务")
	}
	return aggregate, plans
}

func reservePlan(ctx context.Context, conn *sql.Conn, jobID, serviceName string, plan domain.JobPlan, request domain.ResourceRequest, topology domain.Topology, now time.Time) error {
	for _, cpuID := range plan.CPUIds {
		if err := insertAllocation(ctx, conn, jobID, serviceName, "cpu", strconv.Itoa(cpuID), 0, now); err != nil { return err }
	}
	for _, gpuUUID := range plan.GPUUUIDs {
		if err := insertAllocation(ctx, conn, jobID, serviceName, "gpu", gpuUUID, 0, now); err != nil { return err }
	}
	memoryPerNode := uint64(0)
	if len(plan.NUMANodes) > 0 { memoryPerNode = (request.MemoryBytes + uint64(len(plan.NUMANodes)) - 1) / uint64(len(plan.NUMANodes)) }
	for _, nodeID := range plan.NUMANodes {
		capacity := nodeCapacity(topology, nodeID)
		var used uint64
		if err := conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_bytes),0) FROM allocations
            WHERE resource_type='memory_node' AND resource_id=? AND state IN ('RESERVED','ACTIVE','RELEASING')`, strconv.Itoa(nodeID)).Scan(&used); err != nil { return err }
		if used+memoryPerNode > capacity { return fmt.Errorf("NUMA node %d memory reservation conflict", nodeID) }
		if err := insertAllocation(ctx, conn, jobID, serviceName, "memory_node", strconv.Itoa(nodeID), memoryPerNode, now); err != nil { return err }
	}
	return nil
}

func insertAllocation(ctx context.Context, conn *sql.Conn, jobID, serviceName, resourceType, resourceID string, amountBytes uint64, now time.Time) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO allocations
        (id,job_id,service_name,resource_type,resource_id,amount_bytes,state,created_at)
        VALUES(?,?,?,?,?,?,'RESERVED',?)`,
		uuid.NewString(), jobID, serviceName, resourceType, resourceID, amountBytes, now.Format(time.RFC3339Nano))
	return err
}

func cloneAvailability(value domain.Availability) domain.Availability {
	result := domain.Availability{
		CPUIds: append([]int(nil), value.CPUIds...), GPUUUIDs: append([]string(nil), value.GPUUUIDs...), MemoryByNUMA: make(map[int]uint64),
	}
	for node, memory := range value.MemoryByNUMA { result.MemoryByNUMA[node] = memory }
	return result
}

func consumeAvailability(value *domain.Availability, plan domain.JobPlan, memory uint64) {
	usedCPU, usedGPU := intSet(plan.CPUIds), stringSet(plan.GPUUUIDs)
	value.CPUIds = filterInts(value.CPUIds, usedCPU)
	value.GPUUUIDs = filterStrings(value.GPUUUIDs, usedGPU)
	if len(plan.NUMANodes) > 0 {
		perNode := (memory + uint64(len(plan.NUMANodes)) - 1) / uint64(len(plan.NUMANodes))
		for _, node := range plan.NUMANodes {
			if value.MemoryByNUMA[node] > perNode { value.MemoryByNUMA[node] -= perNode } else { value.MemoryByNUMA[node] = 0 }
		}
	}
}

func filterInts(values []int, blocked map[int]struct{}) []int {
	result := values[:0]
	for _, value := range values { if _, exists := blocked[value]; !exists { result = append(result, value) } }
	return result
}

func filterStrings(values []string, blocked map[string]struct{}) []string {
	result := values[:0]
	for _, value := range values { if _, exists := blocked[value]; !exists { result = append(result, value) } }
	return result
}

func appendUniqueInts(values []int, additional ...int) []int {
	seen := intSet(values)
	for _, value := range additional { if _, exists := seen[value]; !exists { values = append(values, value); seen[value] = struct{}{} } }
	return values
}

func resourceRequestForService(spec domain.JobSpec, serviceName string) domain.ResourceRequest {
	if serviceName == "main" { return spec.Resources }
	for _, service := range spec.Workload.Services { if service.Name == serviceName { return service.Resources } }
	return domain.ResourceRequest{}
}

func nodeCapacity(topology domain.Topology, nodeID int) uint64 {
	for _, node := range topology.NUMANodes { if node.ID == nodeID { return node.MemoryBytes } }
	return 0
}

func nullable(value string) any { if value == "" { return nil }; return value }
