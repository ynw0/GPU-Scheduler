//go:build linux

package cgroupv1

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gpu-scheduler/internal/inventory"
)

type Snapshot struct {
	CPUUsageNS        uint64
	CPUUsageNSByID    map[int]uint64
	CPUQuotaMicros    int64
	CPUPeriodMicros   uint64
	CPUQuotaCores     float64
	MemoryUsedBytes   uint64
	MemoryLimitBytes  uint64
	MemoryFailCount   uint64
	OOMUnder          bool
	CPUIds            []int
	NUMANodes         []int
	BlockIOReadBytes  uint64
	BlockIOWriteBytes uint64
}

func Read(pid int) (Snapshot, error) {
	return reader{procRoot: "/proc", cgroupRoot: "/sys/fs/cgroup"}.read(pid)
}

type reader struct {
	procRoot   string
	cgroupRoot string
}

func (r reader) read(pid int) (Snapshot, error) {
	paths, err := r.controllerPaths(pid)
	if err != nil {
		return Snapshot{}, err
	}
	cpuRoot, err := controllerDirectory(filepath.Join(r.cgroupRoot, "cpu,cpuacct"), paths["cpuacct"])
	if err != nil {
		return Snapshot{}, err
	}
	cpusetRoot, err := controllerDirectory(filepath.Join(r.cgroupRoot, "cpuset"), paths["cpuset"])
	if err != nil {
		return Snapshot{}, err
	}
	memoryRoot, err := controllerDirectory(filepath.Join(r.cgroupRoot, "memory"), paths["memory"])
	if err != nil {
		return Snapshot{}, err
	}
	blkioRoot, err := controllerDirectory(filepath.Join(r.cgroupRoot, "blkio"), paths["blkio"])
	if err != nil {
		return Snapshot{}, err
	}
	cpuIDs, err := readCPUList(filepath.Join(cpusetRoot, "cpuset.cpus"))
	if err != nil {
		return Snapshot{}, err
	}
	numaNodes, err := readCPUList(filepath.Join(cpusetRoot, "cpuset.mems"))
	if err != nil {
		return Snapshot{}, err
	}
	usageByID, err := readPerCPUUsage(filepath.Join(cpuRoot, "cpuacct.usage_percpu"), cpuIDs)
	if err != nil {
		return Snapshot{}, err
	}
	cpuUsage, err := readUint(filepath.Join(cpuRoot, "cpuacct.usage"))
	if err != nil {
		return Snapshot{}, err
	}
	cpuQuota, err := readInt64(filepath.Join(cpuRoot, "cpu.cfs_quota_us"))
	if err != nil {
		return Snapshot{}, err
	}
	cpuPeriod, err := readUint(filepath.Join(cpuRoot, "cpu.cfs_period_us"))
	if err != nil {
		return Snapshot{}, err
	}
	var cpuQuotaCores float64
	if cpuQuota >= 0 && cpuPeriod > 0 {
		cpuQuotaCores = float64(cpuQuota) / float64(cpuPeriod)
	}
	memoryUsed, err := readUint(filepath.Join(memoryRoot, "memory.usage_in_bytes"))
	if err != nil {
		return Snapshot{}, err
	}
	memoryLimit, err := readUint(filepath.Join(memoryRoot, "memory.limit_in_bytes"))
	if err != nil {
		return Snapshot{}, err
	}
	if memoryLimit == 0 || memoryLimit >= 1<<62 {
		memoryLimit = 0
	}
	memoryFailCount, err := readUint(filepath.Join(memoryRoot, "memory.failcnt"))
	if err != nil {
		return Snapshot{}, err
	}
	underOOM, err := readOOM(filepath.Join(memoryRoot, "memory.oom_control"))
	if err != nil {
		return Snapshot{}, err
	}
	readBytes, writeBytes, err := readBlockIO(filepath.Join(blkioRoot, "blkio.throttle.io_service_bytes_recursive"))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		CPUUsageNS: cpuUsage, CPUUsageNSByID: usageByID,
		CPUQuotaMicros: cpuQuota, CPUPeriodMicros: cpuPeriod, CPUQuotaCores: cpuQuotaCores,
		MemoryUsedBytes: memoryUsed, MemoryLimitBytes: memoryLimit,
		MemoryFailCount: memoryFailCount, OOMUnder: underOOM, CPUIds: cpuIDs, NUMANodes: numaNodes,
		BlockIOReadBytes: readBytes, BlockIOWriteBytes: writeBytes,
	}, nil
}

func (r reader) controllerPaths(pid int) (map[string]string, error) {
	file, err := os.Open(filepath.Join(r.procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 3)
		if len(parts) != 3 || parts[1] == "" {
			continue
		}
		for _, controller := range strings.Split(parts[1], ",") {
			result[controller] = parts[2]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, controller := range []string{"cpuacct", "cpuset", "memory", "blkio"} {
		if result[controller] == "" {
			return nil, fmt.Errorf("process %d has no %s cgroup v1 controller", pid, controller)
		}
	}
	return result, nil
}

func controllerDirectory(root, cgroupPath string) (string, error) {
	relative := strings.TrimPrefix(filepath.Clean(cgroupPath), string(filepath.Separator))
	path := filepath.Join(root, relative)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cgroup path escapes controller root: %s", cgroupPath)
	}
	return path, nil
}

func readCPUList(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return inventory.ParseCPUList(strings.TrimSpace(string(data)))
}

func readPerCPUUsage(path string, allowed []int) (map[int]uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := strings.Fields(string(data))
	result := make(map[int]uint64, len(allowed))
	for _, id := range allowed {
		if id < 0 || id >= len(values) {
			return nil, fmt.Errorf("cpuacct usage has no CPU %d", id)
		}
		value, err := strconv.ParseUint(values[id], 10, 64)
		if err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, nil
}

func readUint(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func readInt64(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

func readOOM(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	fields := strings.Fields(string(data))
	for index := 0; index+1 < len(fields); index += 2 {
		if fields[index] == "under_oom" {
			return fields[index+1] == "1", nil
		}
	}
	return false, fmt.Errorf("under_oom is missing from %s", path)
}

func readBlockIO(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var readBytes, writeBytes uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			continue
		}
		value, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		switch strings.ToLower(fields[1]) {
		case "read":
			readBytes += value
		case "write":
			writeBytes += value
		}
	}
	return readBytes, writeBytes, scanner.Err()
}
