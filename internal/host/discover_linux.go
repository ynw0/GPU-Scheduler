//go:build linux

package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/inventory"
)

type LinuxDiscoverer struct {
	sysRoot string
}

func NewLinuxDiscoverer() *LinuxDiscoverer {
	return &LinuxDiscoverer{sysRoot: "/sys"}
}

func (d *LinuxDiscoverer) Discover() (domain.Topology, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return domain.Topology{}, fmt.Errorf("hostname: %w", err)
	}
	nodes, cpuToNode, err := d.discoverNUMA()
	if err != nil {
		return domain.Topology{}, err
	}
	cpus, err := d.discoverCPUs(cpuToNode)
	if err != nil {
		return domain.Topology{}, err
	}
	gpus, err := d.discoverGPUs()
	if err != nil {
		return domain.Topology{}, err
	}
	return domain.Topology{
		Hostname: hostname, CPUs: cpus, NUMANodes: nodes, GPUs: gpus, ObservedAt: time.Now().UTC(),
	}, nil
}

func (d *LinuxDiscoverer) HandleTopology(w http.ResponseWriter, _ *http.Request) {
	topology, err := d.Discover()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "about:blank", "title": "Topology discovery failed", "status": 503, "detail": err.Error(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(topology)
}

func (d *LinuxDiscoverer) discoverNUMA() ([]domain.NUMANode, map[int]int, error) {
	paths, err := filepath.Glob(filepath.Join(d.sysRoot, "devices/system/node/node[0-9]*"))
	if err != nil || len(paths) == 0 {
		return nil, nil, errors.New("no NUMA nodes found in sysfs")
	}
	nodes := make([]domain.NUMANode, 0, len(paths))
	cpuToNode := make(map[int]int)
	for _, path := range paths {
		id, err := suffixInt(filepath.Base(path), "node")
		if err != nil {
			continue
		}
		cpuList, err := readText(filepath.Join(path, "cpulist"))
		if err != nil {
			return nil, nil, fmt.Errorf("read node%d cpulist: %w", id, err)
		}
		cpuIDs, err := inventory.ParseCPUList(cpuList)
		if err != nil {
			return nil, nil, err
		}
		for _, cpuID := range cpuIDs {
			cpuToNode[cpuID] = id
		}
		memoryBytes, err := readNUMAMemory(filepath.Join(path, "meminfo"))
		if err != nil {
			return nil, nil, fmt.Errorf("read node%d memory: %w", id, err)
		}
		nodes = append(nodes, domain.NUMANode{ID: id, MemoryBytes: memoryBytes, CPUIds: cpuIDs})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, cpuToNode, nil
}

func (d *LinuxDiscoverer) discoverCPUs(cpuToNode map[int]int) ([]domain.LogicalCPU, error) {
	paths, err := filepath.Glob(filepath.Join(d.sysRoot, "devices/system/cpu/cpu[0-9]*"))
	if err != nil || len(paths) == 0 {
		return nil, errors.New("no CPUs found in sysfs")
	}
	cpus := make([]domain.LogicalCPU, 0, len(paths))
	for _, path := range paths {
		id, err := suffixInt(filepath.Base(path), "cpu")
		if err != nil {
			continue
		}
		coreID, err := readInt(filepath.Join(path, "topology/core_id"))
		if err != nil {
			return nil, fmt.Errorf("read cpu%d core id: %w", id, err)
		}
		socketID, err := readInt(filepath.Join(path, "topology/physical_package_id"))
		if err != nil {
			return nil, fmt.Errorf("read cpu%d socket id: %w", id, err)
		}
		siblingText, err := readText(filepath.Join(path, "topology/thread_siblings_list"))
		if err != nil {
			return nil, fmt.Errorf("read cpu%d siblings: %w", id, err)
		}
		siblings, err := inventory.ParseCPUList(siblingText)
		if err != nil {
			return nil, err
		}
		online := true
		if id != 0 {
			if value, readErr := readInt(filepath.Join(path, "online")); readErr == nil {
				online = value == 1
			}
		}
		node, ok := cpuToNode[id]
		if !ok {
			return nil, fmt.Errorf("cpu%d is not assigned to a NUMA node", id)
		}
		cpus = append(cpus, domain.LogicalCPU{
			ID: id, CoreID: coreID, SocketID: socketID, NUMANode: node, Siblings: siblings, Allocatable: online,
		})
	}
	sort.Slice(cpus, func(i, j int) bool { return cpus[i].ID < cpus[j].ID })
	return cpus, nil
}

func (d *LinuxDiscoverer) discoverGPUs() ([]domain.GPU, error) {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return nil, fmt.Errorf("initialize NVML: %s", nvml.ErrorString(ret))
	}
	defer nvml.Shutdown()
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("get NVML device count: %s", nvml.ErrorString(ret))
	}
	gpus := make([]domain.GPU, 0, count)
	for index := 0; index < count; index++ {
		device, ret := nvml.DeviceGetHandleByIndex(index)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("get GPU %d: %s", index, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("get GPU %d UUID: %s", index, nvml.ErrorString(ret))
		}
		name, ret := device.GetName()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("get GPU %d name: %s", index, nvml.ErrorString(ret))
		}
		pci, ret := device.GetPciInfo()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("get GPU %d PCI info: %s", index, nvml.ErrorString(ret))
		}
		memory, ret := device.GetMemoryInfo()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("get GPU %d memory: %s", index, nvml.ErrorString(ret))
		}
		pciBusID := fmt.Sprintf("%04x:%02x:%02x.0", pci.Domain, pci.Bus, pci.Device)
		numaNode, err := readInt(filepath.Join(d.sysRoot, "bus/pci/devices", pciBusID, "numa_node"))
		if err != nil || numaNode < 0 {
			return nil, fmt.Errorf("resolve NUMA node for GPU %s at %s", uuid, pciBusID)
		}
		gpus = append(gpus, domain.GPU{
			UUID: uuid, Index: index, Name: name, PCIBusID: pciBusID, NUMANode: numaNode,
			MemoryBytes: memory.Total, Healthy: true,
		})
	}
	return gpus, nil
}

func readText(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
}

func readInt(path string) (int, error) {
	value, err := readText(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func suffixInt(value, prefix string) (int, error) {
	return strconv.Atoi(strings.TrimPrefix(value, prefix))
}

func readNUMAMemory(path string) (uint64, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(value), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[2] == "MemTotal:" {
			kib, err := strconv.ParseUint(fields[3], 10, 64)
			return kib * 1024, err
		}
	}
	return 0, errors.New("MemTotal not found")
}
