package domain

import "fmt"

var allowedTransitions = map[JobState]map[JobState]struct{}{
	JobDraft:     {JobQueued: {}, JobReserved: {}, JobCanceled: {}},
	JobQueued:    {JobReserved: {}, JobCanceled: {}},
	JobReserved:  {JobCreating: {}, JobQueued: {}, JobFailed: {}, JobCanceled: {}},
	JobCreating:  {JobStarting: {}, JobFailed: {}, JobCanceled: {}},
	JobStarting:  {JobRunning: {}, JobFailed: {}, JobDegraded: {}, JobCanceled: {}},
	JobRunning:   {JobStopping: {}, JobSucceeded: {}, JobFailed: {}, JobDrifted: {}, JobDegraded: {}},
	JobStopping:  {JobSucceeded: {}, JobFailed: {}, JobCanceled: {}},
	JobDrifted:   {JobRunning: {}, JobStopping: {}, JobFailed: {}, JobOrphaned: {}},
	JobDegraded:  {JobRunning: {}, JobStopping: {}, JobFailed: {}, JobOrphaned: {}},
	JobOrphaned:  {JobFailed: {}, JobCanceled: {}},
	JobSucceeded: {},
	JobFailed:    {JobQueued: {}},
	JobCanceled:  {},
}

func ValidateTransition(from, to JobState) error {
	if _, ok := allowedTransitions[from][to]; !ok {
		return fmt.Errorf("job state transition %s -> %s is not allowed", from, to)
	}
	return nil
}

