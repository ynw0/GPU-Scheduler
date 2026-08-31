//go:build linux

package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"gpu-scheduler/internal/dockerhost"
)

const (
	DockerVersion  = "24.0.5"
	DockerAPI      = dockerhost.APIVersion
	CgroupVersion  = "1"
	CgroupDriver   = "cgroupfs"
	ComposeVersion = "2.24.5"
)

type Runtime struct {
	DockerVersion      string   `json:"dockerVersion"`
	DockerAPI          string   `json:"dockerApi"`
	CgroupVersion      string   `json:"cgroupVersion"`
	CgroupDriver       string   `json:"cgroupDriver"`
	ComposeVersion     string   `json:"composeVersion"`
	SwapEnabled        bool     `json:"swapEnabled"`
	SwapTotalBytes     uint64   `json:"swapTotalBytes"`
	SwapLimitSupported bool     `json:"swapLimitSupported"`
	Warnings           []string `json:"warnings"`
}

func Validate(ctx context.Context, socketPath string, info system.Info, version client.ServerVersionResult) (Runtime, error) {
	composeVersion, err := readComposeVersion(ctx, socketPath)
	if err != nil {
		return Runtime{}, err
	}
	swapTotal, err := readSwapTotal("/proc/meminfo")
	if err != nil {
		return Runtime{}, err
	}
	runtime := Runtime{
		DockerVersion: version.Version, DockerAPI: version.APIVersion,
		CgroupVersion: info.CgroupVersion, CgroupDriver: info.CgroupDriver,
		ComposeVersion: composeVersion, SwapEnabled: swapTotal > 0, SwapTotalBytes: swapTotal,
		SwapLimitSupported: info.SwapLimit, Warnings: make([]string, 0),
	}
	if !info.SwapLimit {
		runtime.Warnings = append(runtime.Warnings, "不支持每任务 Swap 限制")
	}
	if version.Version != DockerVersion {
		return Runtime{}, fmt.Errorf("Docker Engine must be %s, got %s", DockerVersion, version.Version)
	}
	if version.APIVersion != DockerAPI {
		return Runtime{}, fmt.Errorf("Docker API must be %s, got %s", DockerAPI, version.APIVersion)
	}
	if info.CgroupVersion != CgroupVersion || info.CgroupDriver != CgroupDriver {
		return Runtime{}, fmt.Errorf("Docker cgroup must be version %s with %s driver, got version %s with %s driver", CgroupVersion, CgroupDriver, info.CgroupVersion, info.CgroupDriver)
	}
	if !info.CPUSet || !info.MemoryLimit {
		return Runtime{}, fmt.Errorf("Docker must support cpuset and memory limits")
	}
	if composeVersion != ComposeVersion {
		return Runtime{}, fmt.Errorf("Docker Compose must be %s, got %s", ComposeVersion, composeVersion)
	}
	for _, path := range []string{
		"/sys/fs/cgroup/cpu,cpuacct",
		"/sys/fs/cgroup/cpuset",
		"/sys/fs/cgroup/memory",
		"/sys/fs/cgroup/blkio",
	} {
		if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
			return Runtime{}, fmt.Errorf("required cgroup v1 controller is unavailable: %s", path)
		}
	}
	return runtime, nil
}

func readComposeVersion(ctx context.Context, socketPath string) (string, error) {
	command, err := dockerhost.Command(ctx, socketPath, "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read Docker Compose version: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimPrefix(strings.TrimSpace(string(output)), "v"), nil
}

func readSwapTotal(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("read host memory: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "SwapTotal:" {
			kib, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse SwapTotal: %w", err)
			}
			return kib * 1024, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("SwapTotal is missing from %s", path)
}
