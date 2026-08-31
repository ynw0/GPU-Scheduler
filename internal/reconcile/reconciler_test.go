package reconcile

import (
	"testing"

	"gpu-scheduler/internal/domain"
)

func TestUnmanagedUnboundedContainerBlocksHost(t *testing.T) {
	_, cpus, gpus := Compare([]domain.ContainerClaim{{
		ContainerID: "external", CPUUnbounded: true, GPUUnbounded: true,
	}}, nil, []string{"0", "1", "2"}, []string{"GPU-0", "GPU-1"})
	if len(cpus) != 3 || len(gpus) != 2 {
		t.Fatalf("blocked CPUs=%v GPUs=%v", cpus, gpus)
	}
}
