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
	OwnerID         string
	TeamID          string
	IdempotencyKey  string
	Spec            domain.JobSpec
	Topology        domain.Topology
	Availability    domain.Availability
}

func NewReservationService(store *store.Store, engine *Engine) *ReservationService {
	return &ReservationService{store: store, engine: engine}
}

func (s *ReservationService) Create(ctx context.Context, input CreateInput) (domain.Job, domain.JobPlan, error) {
	plan := s.engine.Plan(input.Topology, input.Availability, input.Spec.Resources)
	if !plan.Schedulable {
		return domain.Job{}, plan, fmt.Errorf("resource plan is not schedulable")
	}
	now := time.Now().UTC()
	job := domain.Job{
		ID: uuid.NewString(), Name: input.Spec.Name, State: domain.JobReserved,
		Priority: input.Spec.Priority, OwnerID: input.OwnerID, TeamID: input.TeamID,
		Spec: input.Spec, IdempotencyKey: input.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
	}
	err := s.store.Immediate(ctx, func(conn *sql.Conn) error {
		var existing string
		err := conn.QueryRowContext(ctx,
			"SELECT id FROM jobs WHERE owner_id = ? AND idempotency_key = ?", input.OwnerID, input.IdempotencyKey,
		).Scan(&existing)
		if err == nil {
			return fmt.Errorf("idempotency key already belongs to job %s", existing)
		}
		if err != sql.ErrNoRows {
			return err
		}
		specJSON, err := json.Marshal(input.Spec)
		if err != nil { return err }
		_, err = conn.ExecContext(ctx, `INSERT INTO jobs
            (id,name,owner_id,team_id,state,priority,spec_json,idempotency_key,bypass_count,created_at,updated_at)
            VALUES(?,?,?,?,?,?,?,?,0,?,?)`,
			job.ID, job.Name, job.OwnerID, nullable(job.TeamID), job.State, job.Priority,
			string(specJSON), job.IdempotencyKey, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil { return err }
		for _, cpuID := range plan.CPUIds {
			if err := insertAllocation(ctx, conn, job.ID, "cpu", strconv.Itoa(cpuID), 0, now); err != nil { return err }
		}
		for _, gpuUUID := range plan.GPUUUIDs {
			if err := insertAllocation(ctx, conn, job.ID, "gpu", gpuUUID, 0, now); err != nil { return err }
		}
		memoryPerNode := uint64(0)
		if len(plan.NUMANodes) > 0 {
			memoryPerNode = (input.Spec.Resources.MemoryBytes + uint64(len(plan.NUMANodes)) - 1) / uint64(len(plan.NUMANodes))
		}
		for _, nodeID := range plan.NUMANodes {
			capacity := nodeCapacity(input.Topology, nodeID)
			var used uint64
			if err := conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_bytes),0) FROM allocations
                WHERE resource_type='memory_node' AND resource_id=? AND state IN ('RESERVED','ACTIVE','RELEASING')`,
				strconv.Itoa(nodeID)).Scan(&used); err != nil { return err }
			if used+memoryPerNode > capacity {
				return fmt.Errorf("NUMA node %d memory reservation conflict", nodeID)
			}
			if err := insertAllocation(ctx, conn, job.ID, "memory_node", strconv.Itoa(nodeID), memoryPerNode, now); err != nil { return err }
		}
		return nil
	})
	return job, plan, err
}

func insertAllocation(ctx context.Context, conn *sql.Conn, jobID, resourceType, resourceID string, amountBytes uint64, now time.Time) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO allocations
        (id,job_id,service_name,resource_type,resource_id,amount_bytes,state,created_at)
        VALUES(?,?,?,?,?,?,'RESERVED',?)`,
		uuid.NewString(), jobID, "", resourceType, resourceID, amountBytes, now.Format(time.RFC3339Nano))
	return err
}

func nodeCapacity(topology domain.Topology, nodeID int) uint64 {
	for _, node := range topology.NUMANodes {
		if node.ID == nodeID { return node.MemoryBytes }
	}
	return 0
}

func nullable(value string) any {
	if value == "" { return nil }
	return value
}
