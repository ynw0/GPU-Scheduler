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
	"gpu-scheduler/internal/quota"
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
	Created      bool
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
	if err := s.checkQuota(ctx, input, aggregate); err != nil {
		return ReservationResult{Plan: domain.JobPlan{Conflicts: []string{err.Error()}}}, err
	}
	existing, err := s.store.FindJobByIdempotency(ctx, input.OwnerID, input.IdempotencyKey)
	if err == nil {
		return ReservationResult{Job: existing, Plan: aggregate, ServicePlans: servicePlans}, nil
	}
	if !store.IsNotFound(err) {
		return ReservationResult{}, err
	}
	now := time.Now().UTC()
	job := domain.Job{
		ID: uuid.NewString(), Name: input.Spec.Name, State: domain.JobQueued,
		Priority: input.Spec.Priority, OwnerID: input.OwnerID, TeamID: input.TeamID,
		Spec: input.Spec, IdempotencyKey: input.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
	}
	if aggregate.Schedulable {
		job.State = domain.JobReserved
	}
	var teamID any
	if job.TeamID != "" {
		teamID = job.TeamID
	}
	err = s.store.Immediate(ctx, func(conn *sql.Conn) error {
		specJSON, err := json.Marshal(input.Spec)
		if err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, `INSERT INTO jobs
            (id,name,owner_id,team_id,state,priority,spec_json,idempotency_key,bypass_count,created_at,updated_at)
            VALUES(?,?,?,?,?,?,?,?,0,?,?)`,
			job.ID, job.Name, job.OwnerID, teamID, job.State, job.Priority,
			string(specJSON), job.IdempotencyKey, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if job.State != domain.JobReserved {
			return nil
		}
		for serviceName, plan := range servicePlans {
			request := resourceRequestForService(input.Spec, serviceName)
			if err := reservePlan(ctx, conn, job.ID, serviceName, plan, request, input.Topology, now); err != nil {
				return err
			}
		}
		return nil
	})
	return ReservationResult{Job: job, Plan: aggregate, ServicePlans: servicePlans, Created: true}, err
}

func (s *ReservationService) Promote(ctx context.Context, job domain.Job, topology domain.Topology, availability domain.Availability) (ReservationResult, error) {
	aggregate, servicePlans := s.planWorkload(topology, availability, job.Spec)
	if !aggregate.Schedulable {
		return ReservationResult{Job: job, Plan: aggregate, ServicePlans: servicePlans}, fmt.Errorf("resource plan is not schedulable")
	}
	if err := s.checkQuota(ctx, CreateInput{OwnerID: job.OwnerID, TeamID: job.TeamID, Spec: job.Spec, Topology: topology}, aggregate); err != nil {
		return ReservationResult{Job: job, Plan: aggregate}, err
	}
	now := time.Now().UTC()
	err := s.store.Immediate(ctx, func(conn *sql.Conn) error {
		var state string
		if err := conn.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", job.ID).Scan(&state); err != nil {
			return err
		}
		if state != string(domain.JobQueued) {
			return fmt.Errorf("job %s is %s, not QUEUED", job.ID, state)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE jobs SET state=?,bypass_count=0,updated_at=? WHERE id=? AND state=?",
			domain.JobReserved, now.Format(time.RFC3339Nano), job.ID, domain.JobQueued); err != nil {
			return err
		}
		for serviceName, plan := range servicePlans {
			request := resourceRequestForService(job.Spec, serviceName)
			if err := reservePlan(ctx, conn, job.ID, serviceName, plan, request, topology, now); err != nil {
				return err
			}
		}
		return nil
	})
	job.State = domain.JobReserved
	job.UpdatedAt = now
	return ReservationResult{Job: job, Plan: aggregate, ServicePlans: servicePlans}, err
}

func (s *ReservationService) checkQuota(ctx context.Context, input CreateInput, plan domain.JobPlan) error {
	userLimit, _, err := s.store.QuotaFor(ctx, "user", input.OwnerID)
	if err != nil {
		return err
	}
	teamLimit := quota.Limit{GPUCount: -1, CPUThreadCount: -1}
	if input.TeamID != "" {
		teamLimit, _, err = s.store.QuotaFor(ctx, "team", input.TeamID)
		if err != nil {
			return err
		}
	}
	userUsage, teamUsage, err := s.store.QuotaUsage(ctx, input.OwnerID, input.TeamID)
	if err != nil {
		return err
	}
	userUsage.GPUCount = GPUUnits(input.Topology, userUsage.GPUIds)
	teamUsage.GPUCount = GPUUnits(input.Topology, teamUsage.GPUIds)
	threads := len(plan.CPUIds)
	if threads == 0 {
		threads = requestedCPUThreads(input.Spec)
	}
	requestedGPU := GPUUnits(input.Topology, plan.GPUUUIDs)
	if requestedGPU == 0 {
		requestedGPU = requestedGPUUnits(input.Spec)
	}
	var memoryBytes uint64
	if input.Spec.Workload.Kind == domain.WorkloadCompose {
		for _, service := range input.Spec.Workload.Services {
			memoryBytes += service.Resources.MemoryBytes
		}
	} else {
		memoryBytes = input.Spec.Resources.MemoryBytes
	}
	conflicts := quota.Check(requestedGPU, threads, memoryBytes, teamLimit, userLimit, teamUsage, userUsage)
	if len(conflicts) > 0 {
		return fmt.Errorf("%s", conflicts[0])
	}
	return nil
}

func requestedCPUThreads(spec domain.JobSpec) int {
	if spec.Workload.Kind == domain.WorkloadCompose {
		total := 0
		for _, service := range spec.Workload.Services {
			if service.Resources.CPUMode == domain.CPUManualThreads {
				total += len(service.Resources.CPUIds)
			} else {
				total += service.Resources.CPUCount
			}
		}
		return total
	}
	if spec.Resources.CPUMode == domain.CPUManualThreads {
		return len(spec.Resources.CPUIds)
	}
	return spec.Resources.CPUCount
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
			for _, conflict := range plan.Conflicts {
				aggregate.Conflicts = append(aggregate.Conflicts, service.Name+": "+conflict)
			}
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
		if err := insertAllocation(ctx, conn, jobID, serviceName, "cpu", strconv.Itoa(cpuID), 0, now); err != nil {
			return err
		}
	}
	for _, gpuUUID := range plan.GPUUUIDs {
		if err := insertAllocation(ctx, conn, jobID, serviceName, "gpu", gpuUUID, 0, now); err != nil {
			return err
		}
	}
	memoryPerNode := uint64(0)
	if len(plan.NUMANodes) > 0 {
		memoryPerNode = (request.MemoryBytes + uint64(len(plan.NUMANodes)) - 1) / uint64(len(plan.NUMANodes))
	}
	for _, nodeID := range plan.NUMANodes {
		capacity := nodeCapacity(topology, nodeID)
		var used uint64
		if err := conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_bytes),0) FROM allocations
            WHERE resource_type='memory_node' AND resource_id=? AND state IN ('RESERVED','ACTIVE','RELEASING')`, strconv.Itoa(nodeID)).Scan(&used); err != nil {
			return err
		}
		if used+memoryPerNode > capacity {
			return fmt.Errorf("NUMA node %d memory reservation conflict", nodeID)
		}
		if err := insertAllocation(ctx, conn, jobID, serviceName, "memory_node", strconv.Itoa(nodeID), memoryPerNode, now); err != nil {
			return err
		}
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
	for node, memory := range value.MemoryByNUMA {
		result.MemoryByNUMA[node] = memory
	}
	return result
}

func consumeAvailability(value *domain.Availability, plan domain.JobPlan, memory uint64) {
	usedCPU, usedGPU := intSet(plan.CPUIds), stringSet(plan.GPUUUIDs)
	value.CPUIds = filterInts(value.CPUIds, usedCPU)
	value.GPUUUIDs = filterStrings(value.GPUUUIDs, usedGPU)
	if len(plan.NUMANodes) > 0 {
		perNode := (memory + uint64(len(plan.NUMANodes)) - 1) / uint64(len(plan.NUMANodes))
		for _, node := range plan.NUMANodes {
			if value.MemoryByNUMA[node] > perNode {
				value.MemoryByNUMA[node] -= perNode
			} else {
				value.MemoryByNUMA[node] = 0
			}
		}
	}
}

func filterInts(values []int, blocked map[int]struct{}) []int {
	result := values[:0]
	for _, value := range values {
		if _, exists := blocked[value]; !exists {
			result = append(result, value)
		}
	}
	return result
}

func filterStrings(values []string, blocked map[string]struct{}) []string {
	result := values[:0]
	for _, value := range values {
		if _, exists := blocked[value]; !exists {
			result = append(result, value)
		}
	}
	return result
}

func appendUniqueInts(values []int, additional ...int) []int {
	seen := intSet(values)
	for _, value := range additional {
		if _, exists := seen[value]; !exists {
			values = append(values, value)
			seen[value] = struct{}{}
		}
	}
	return values
}

func resourceRequestForService(spec domain.JobSpec, serviceName string) domain.ResourceRequest {
	if serviceName == "main" {
		return spec.Resources
	}
	for _, service := range spec.Workload.Services {
		if service.Name == serviceName {
			return service.Resources
		}
	}
	return domain.ResourceRequest{}
}

func nodeCapacity(topology domain.Topology, nodeID int) uint64 {
	for _, node := range topology.NUMANodes {
		if node.ID == nodeID {
			return node.MemoryBytes
		}
	}
	return 0
}

func IntersectPool(availability domain.Availability, pool domain.ResourcePool, gpus []domain.GPU) domain.Availability {
	cpuAllow, gpuAllow := intSet(pool.CPUIds), stringSet(pool.GPUUUIDs)
	parentOf := make(map[string]string, len(gpus))
	for _, gpu := range gpus {
		if gpu.ParentUUID != "" {
			parentOf[gpu.UUID] = gpu.ParentUUID
		}
	}
	result := domain.Availability{MemoryByNUMA: make(map[int]uint64)}
	for _, id := range availability.CPUIds {
		if _, ok := cpuAllow[id]; ok {
			result.CPUIds = append(result.CPUIds, id)
		}
	}
	for _, uuid := range availability.GPUUUIDs {
		if _, ok := gpuAllow[uuid]; ok {
			result.GPUUUIDs = append(result.GPUUUIDs, uuid)
			continue
		}
		if parent, ok := parentOf[uuid]; ok {
			if _, ok := gpuAllow[parent]; ok {
				result.GPUUUIDs = append(result.GPUUUIDs, uuid)
			}
		}
	}
	if len(pool.NUMANodes) == 0 {
		result.MemoryByNUMA = availability.MemoryByNUMA
		return result
	}
	nodeAllow := intSet(pool.NUMANodes)
	for node, memory := range availability.MemoryByNUMA {
		if _, ok := nodeAllow[node]; ok {
			result.MemoryByNUMA[node] = memory
		}
	}
	return result
}
