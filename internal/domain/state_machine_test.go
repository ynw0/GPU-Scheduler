package domain

import "testing"

func TestRetryFromFailedIsAllowed(t *testing.T) {
	if err := ValidateTransition(JobFailed, JobQueued); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTransition(JobSucceeded, JobQueued); err == nil {
		t.Fatal("succeeded must remain terminal")
	}
}
