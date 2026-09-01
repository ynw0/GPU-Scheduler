//go:build linux

package monitor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"gpu-scheduler/internal/cgroupv1"
	"gpu-scheduler/internal/domain"
)

type cpuCounters struct{ total, idle uint64 }

type containerCPUCounters struct {
	sampledAt time.Time
	usageByID map[int]uint64
}

type LinuxCollector struct {
	mu         sync.Mutex
	previous   map[int]cpuCounters
	containers map[string]containerCPUCounters
	nvmlReady  bool
}

func NewLinuxCollector() (*LinuxCollector, error) {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return nil, fmt.Errorf("initialize NVML metrics: %s", nvml.ErrorString(ret))
	}
	return &LinuxCollector{
		previous: make(map[int]cpuCounters), containers: make(map[string]containerCPUCounters), nvmlReady: true,
	}, nil
}

func (c *LinuxCollector) SampleContainer(containerID string, pid int) (domain.ContainerMetricSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := cgroupv1.Read(pid)
	if err != nil {
		return domain.ContainerMetricSnapshot{}, err
	}
	now := time.Now().UTC()
	percentages := make(map[int]float64)
	previous, exists := c.containers[containerID]
	elapsed := now.Sub(previous.sampledAt)
	if exists && elapsed > 0 {
		for id, current := range raw.CPUUsageNSByID {
			prior, ok := previous.usageByID[id]
			if ok && current >= prior {
				percentages[id] = float64(current-prior) * 100 / float64(elapsed.Nanoseconds())
			}
		}
	}
	c.containers[containerID] = containerCPUCounters{sampledAt: now, usageByID: raw.CPUUsageNSByID}
	return domain.ContainerMetricSnapshot{
		SampledAt: now, ContainerID: containerID, CPUPercentByID: percentages,
		CPUUsageNS: raw.CPUUsageNS, CPUQuotaMicros: raw.CPUQuotaMicros,
		CPUPeriodMicros: raw.CPUPeriodMicros, CPUQuotaCores: raw.CPUQuotaCores,
		MemoryUsedBytes: raw.MemoryUsedBytes, MemoryLimitBytes: raw.MemoryLimitBytes,
		MemoryFailCount: raw.MemoryFailCount, OOMUnder: raw.OOMUnder,
		CPUIds: raw.CPUIds, NUMANodes: raw.NUMANodes,
		BlockIOReadBytes: raw.BlockIOReadBytes, BlockIOWriteBytes: raw.BlockIOWriteBytes,
	}, nil
}

func (c *LinuxCollector) Close() {
	if c.nvmlReady {
		_ = nvml.Shutdown()
	}
}

func (c *LinuxCollector) Sample() (domain.HostMetricSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpus, err := readCPUCounters("/proc/stat")
	if err != nil {
		return domain.HostMetricSnapshot{}, err
	}
	percentages := make(map[int]float64)
	for id, current := range cpus {
		previous, exists := c.previous[id]
		if exists && current.total > previous.total {
			totalDelta := current.total - previous.total
			idleDelta := current.idle - previous.idle
			percentages[id] = float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
		}
	}
	c.previous = cpus
	memory, err := readMemory("/proc/meminfo")
	if err != nil {
		return domain.HostMetricSnapshot{}, err
	}
	gpus, err := readGPUMetrics()
	if err != nil {
		return domain.HostMetricSnapshot{}, err
	}
	return domain.HostMetricSnapshot{
		SampledAt: time.Now().UTC(), CPUPercentByID: percentages,
		MemoryUsedBytes: memory.total - memory.available, MemoryTotalBytes: memory.total,
		SwapUsedBytes: memory.swapTotal - memory.swapFree, SwapTotalBytes: memory.swapTotal, GPUs: gpus,
	}, nil
}

func readCPUCounters(path string) (map[int]cpuCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make(map[int]cpuCounters)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") || fields[0] == "cpu" {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
		if err != nil {
			continue
		}
		var values []uint64
		for _, field := range fields[1:] {
			value, parseErr := strconv.ParseUint(field, 10, 64)
			if parseErr != nil {
				return nil, parseErr
			}
			values = append(values, value)
		}
		var total uint64
		for _, value := range values {
			total += value
		}
		result[id] = cpuCounters{total: total, idle: values[3] + values[4]}
	}
	return result, scanner.Err()
}

type memoryCounters struct{ total, available, swapTotal, swapFree uint64 }

func readMemory(path string) (memoryCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return memoryCounters{}, err
	}
	defer file.Close()
	values := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return memoryCounters{}, err
		}
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	return memoryCounters{
		total: values["MemTotal"], available: values["MemAvailable"],
		swapTotal: values["SwapTotal"], swapFree: values["SwapFree"],
	}, scanner.Err()
}

func readGPUMetrics() ([]domain.GPUMetric, error) {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("GPU count: %s", nvml.ErrorString(ret))
	}
	result := make([]domain.GPUMetric, 0, count)
	for index := 0; index < count; index++ {
		device, ret := nvml.DeviceGetHandleByIndex(index)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d: %s", index, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d UUID", index)
		}
		utilization, ret := device.GetUtilizationRates()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d utilization", index)
		}
		memory, ret := device.GetMemoryInfo()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d memory", index)
		}
		temperature, ret := device.GetTemperature(nvml.TEMPERATURE_GPU)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d temperature", index)
		}
		power, ret := device.GetPowerUsage()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("GPU %d power", index)
		}
		result = append(result, domain.GPUMetric{
			UUID: uuid, UtilizationPercent: utilization.Gpu, MemoryUsedBytes: memory.Used,
			MemoryTotalBytes: memory.Total, TemperatureCelsius: temperature, PowerWatts: float64(power) / 1000,
		})
	}
	return result, nil
}
