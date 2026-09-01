//go:build linux

package monitor

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

var dockerCgroupID = regexp.MustCompile(`(?:docker-|/docker/)([0-9a-f]{64})`)

type GPUCompute struct {
	ByContainer map[string][]string
	HostGPUs    []string
}

func (c *LinuxCollector) GPUCompute() (GPUCompute, error) {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return GPUCompute{}, fmt.Errorf("GPU count: %s", nvml.ErrorString(ret))
	}
	byContainer := make(map[string][]string)
	hostSeen := make(map[string]struct{})
	for index := 0; index < count; index++ {
		device, ret := nvml.DeviceGetHandleByIndex(index)
		if ret != nvml.SUCCESS {
			return GPUCompute{}, fmt.Errorf("GPU %d: %s", index, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			return GPUCompute{}, fmt.Errorf("GPU %d UUID", index)
		}
		processes, ret := device.GetComputeRunningProcesses()
		if ret != nvml.SUCCESS {
			return GPUCompute{}, fmt.Errorf("GPU %d processes: %s", index, nvml.ErrorString(ret))
		}
		hostHit := false
		for _, process := range processes {
			if process.Pid == 0 {
				continue
			}
			containerID := containerIDFromPID(int(process.Pid))
			if containerID == "" {
				hostHit = true
				continue
			}
			byContainer[containerID] = appendUnique(byContainer[containerID], uuid)
		}
		if hostHit {
			hostSeen[uuid] = struct{}{}
		}
	}
	host := make([]string, 0, len(hostSeen))
	for uuid := range hostSeen {
		host = append(host, uuid)
	}
	return GPUCompute{ByContainer: byContainer, HostGPUs: host}, nil
}

func (c *LinuxCollector) HostOccupiedGPUs() ([]string, error) {
	compute, err := c.GPUCompute()
	if err != nil {
		return nil, err
	}
	return compute.HostGPUs, nil
}

func containerIDFromPID(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	match := dockerCgroupID.FindStringSubmatch(string(data))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func appendUnique(values []string, item string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

func MatchContainerID(claimID, processID string) bool {
	claimID = strings.TrimPrefix(claimID, "sha256:")
	if claimID == processID {
		return true
	}
	return strings.HasPrefix(claimID, processID) || strings.HasPrefix(processID, claimID)
}
