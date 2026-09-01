package heartbeat

import "path/filepath"

const ContainerDir = "/run/gpu-scheduler-heartbeat"

func Dir(stateDir, attemptID string) string {
	return filepath.Join(stateDir, "heartbeats", attemptID)
}
