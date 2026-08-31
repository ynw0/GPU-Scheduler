//go:build linux

package cgroupv1

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestReaderReadsNestedControllerPaths(t *testing.T) {
	fixture := newFixture(t, "/docker/parent/container-a", "0-3,8", -1, 100000)
	fixture.writeCPUUsage([]uint64{10, 20, 30, 40, 0, 0, 0, 0, 90})
	fixture.write("cpu,cpuacct", "cpuacct.usage", "190")
	fixture.write("cpuset", "cpuset.mems", "0-1")
	fixture.write("memory", "memory.usage_in_bytes", "1073741824")
	fixture.write("memory", "memory.limit_in_bytes", "2147483648")
	fixture.write("memory", "memory.failcnt", "3")
	fixture.write("memory", "memory.oom_control", "oom_kill_disable 0\nunder_oom 1\noom_kill 2\n")
	fixture.write("blkio", "blkio.throttle.io_service_bytes_recursive", "8:0 Read 4096\n8:0 Write 8192\n8:0 Sync 1024\nTotal 13312\n")

	snapshot, err := fixture.reader().read(fixture.pid)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CPUUsageNS != 190 || snapshot.CPUQuotaMicros != -1 || snapshot.CPUQuotaCores != 0 {
		t.Fatalf("unexpected CPU totals: %+v", snapshot)
	}
	if !reflect.DeepEqual(snapshot.CPUIds, []int{0, 1, 2, 3, 8}) {
		t.Fatalf("unexpected cpuset: %v", snapshot.CPUIds)
	}
	if !reflect.DeepEqual(snapshot.NUMANodes, []int{0, 1}) {
		t.Fatalf("unexpected NUMA nodes: %v", snapshot.NUMANodes)
	}
	if snapshot.CPUUsageNSByID[8] != 90 {
		t.Fatalf("unexpected CPU 8 usage: %d", snapshot.CPUUsageNSByID[8])
	}
	if snapshot.MemoryUsedBytes != 1073741824 || snapshot.MemoryLimitBytes != 2147483648 || snapshot.MemoryFailCount != 3 || !snapshot.OOMUnder {
		t.Fatalf("unexpected memory metrics: %+v", snapshot)
	}
	if snapshot.BlockIOReadBytes != 4096 || snapshot.BlockIOWriteBytes != 8192 {
		t.Fatalf("unexpected blkio metrics: %+v", snapshot)
	}
}

func TestReaderReportsOneCoreQuotaWithWideCpuset(t *testing.T) {
	fixture := newFixture(t, "/docker/container-b", "0-63", 100000, 100000)
	usage := make([]uint64, 64)
	for index := range usage {
		usage[index] = uint64(index + 1)
	}
	fixture.writeCPUUsage(usage)
	fixture.write("cpu,cpuacct", "cpuacct.usage", "2080")
	fixture.write("cpuset", "cpuset.mems", "0")
	fixture.write("memory", "memory.usage_in_bytes", "1")
	fixture.write("memory", "memory.limit_in_bytes", "2")
	fixture.write("memory", "memory.failcnt", "0")
	fixture.write("memory", "memory.oom_control", "oom_kill_disable 0\nunder_oom 0\n")
	fixture.write("blkio", "blkio.throttle.io_service_bytes_recursive", "")

	snapshot, err := fixture.reader().read(fixture.pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CPUIds) != 64 {
		t.Fatalf("expected 64 allowed CPUs, got %d", len(snapshot.CPUIds))
	}
	if snapshot.CPUQuotaCores != 1 {
		t.Fatalf("expected one CPU quota core, got %v", snapshot.CPUQuotaCores)
	}
}

func TestControllerDirectoryRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := controllerDirectory(root, "../../outside"); err == nil {
		t.Fatal("expected escaping cgroup path to be rejected")
	}
}

type fixture struct {
	t          *testing.T
	pid        int
	procRoot   string
	cgroupRoot string
	path       string
}

func newFixture(t *testing.T, cgroupPath, cpus string, quota int64, period uint64) fixture {
	t.Helper()
	root := t.TempDir()
	item := fixture{
		t: t, pid: 4321, procRoot: filepath.Join(root, "proc"),
		cgroupRoot: filepath.Join(root, "cgroup"), path: cgroupPath,
	}
	cgroup := strings.Join([]string{
		"10:cpu,cpuacct:" + cgroupPath,
		"9:cpuset:" + cgroupPath,
		"5:memory:" + cgroupPath,
		"3:blkio:" + cgroupPath,
		"0::" + cgroupPath,
	}, "\n") + "\n"
	writeFile(t, filepath.Join(item.procRoot, strconv.Itoa(item.pid), "cgroup"), cgroup)
	item.write("cpu,cpuacct", "cpu.cfs_quota_us", strconv.FormatInt(quota, 10))
	item.write("cpu,cpuacct", "cpu.cfs_period_us", strconv.FormatUint(period, 10))
	item.write("cpuset", "cpuset.cpus", cpus)
	return item
}

func (f fixture) reader() reader {
	return reader{procRoot: f.procRoot, cgroupRoot: f.cgroupRoot}
}

func (f fixture) write(controller, name, value string) {
	f.t.Helper()
	path := filepath.Join(f.cgroupRoot, controller, strings.TrimPrefix(f.path, "/"), name)
	writeFile(f.t, path, value)
}

func (f fixture) writeCPUUsage(values []uint64) {
	f.t.Helper()
	fields := make([]string, len(values))
	for index, value := range values {
		fields[index] = strconv.FormatUint(value, 10)
	}
	f.write("cpu,cpuacct", "cpuacct.usage_percpu", strings.Join(fields, " "))
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o640); err != nil {
		t.Fatal(fmt.Errorf("write %s: %w", path, err))
	}
}
