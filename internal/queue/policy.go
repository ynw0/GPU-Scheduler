package queue

import (
	"sort"

	"gpu-scheduler/internal/domain"
)

const maxBypasses = 5

type Candidate struct {
	Job         domain.Job
	Schedulable bool
}

func Next(candidates []Candidate) (domain.Job, bool) {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Job.Priority != ordered[j].Job.Priority {
			return ordered[i].Job.Priority < ordered[j].Job.Priority
		}
		return ordered[i].Job.CreatedAt.Before(ordered[j].Job.CreatedAt)
	})
	for index, candidate := range ordered {
		if index > 0 && candidate.Job.Priority != ordered[0].Job.Priority {
			return domain.Job{}, false
		}
		if candidate.Schedulable {
			return candidate.Job, true
		}
		if candidate.Job.BypassCount >= maxBypasses {
			return domain.Job{}, false
		}
	}
	return domain.Job{}, false
}
