package queue

import (
	"testing"
	"time"

	"gpu-scheduler/internal/domain"
)

func TestNextBackfillsWithinPriority(t *testing.T) {
	now := time.Now()
	job, ok := Next([]Candidate{
		{Job: domain.Job{ID: "large", Priority: domain.PriorityNormal, CreatedAt: now}, Schedulable: false},
		{Job: domain.Job{ID: "small", Priority: domain.PriorityNormal, CreatedAt: now.Add(time.Second)}, Schedulable: true},
	})
	if !ok || job.ID != "small" { t.Fatalf("got %q, ok=%v", job.ID, ok) }
}

func TestNextHonorsBypassBarrier(t *testing.T) {
	job, ok := Next([]Candidate{
		{Job: domain.Job{ID: "large", Priority: domain.PriorityNormal, BypassCount: maxBypasses}, Schedulable: false},
		{Job: domain.Job{ID: "small", Priority: domain.PriorityNormal}, Schedulable: true},
	})
	if ok { t.Fatalf("expected barrier, got %q", job.ID) }
}

