//go:build linux

package mig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

var ErrRebootRequired = errors.New("MIG 模式已写入，重启后生效")

func Apply(ctx context.Context, gpuUUID string, memoryBytes uint64, profiles []string) error {
	catalog := CatalogForMemory(memoryBytes)
	if _, err := Evaluate(catalog, profiles); err != nil {
		return err
	}
	if len(profiles) == 0 {
		return disableMIG(ctx, gpuUUID)
	}
	if err := enableMIG(ctx, gpuUUID); err != nil {
		return err
	}
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dci"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("销毁 Compute Instance: %w", err)
	}
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dgi"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("销毁 GPU Instance: %w", err)
	}
	ordered := append([]string(nil), profiles...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, _ := catalog.ByName(ordered[i])
		b, _ := catalog.ByName(ordered[j])
		return a.G > b.G
	})
	ids, err := GIIDs(catalog, ordered)
	if err != nil {
		return err
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-cgi", strings.Join(parts, ","), "-C"); err != nil {
		return fmt.Errorf("创建实例: %w", err)
	}
	return nil
}

func enableMIG(ctx context.Context, gpuUUID string) error {
	current, _, err := queryMIGMode(ctx, gpuUUID)
	if err != nil {
		return err
	}
	if modeEnabled(current) {
		return nil
	}
	if err := runSMI(ctx, "-i", gpuUUID, "-mig", "1"); err != nil {
		return fmt.Errorf("开启 MIG 模式: %w", err)
	}
	current, _, err = queryMIGMode(ctx, gpuUUID)
	if err != nil {
		return err
	}
	if !modeEnabled(current) {
		return ErrRebootRequired
	}
	return nil
}

func disableMIG(ctx context.Context, gpuUUID string) error {
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dci"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("销毁 Compute Instance: %w", err)
	}
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dgi"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("销毁 GPU Instance: %w", err)
	}
	current, pending, err := queryMIGMode(ctx, gpuUUID)
	if err != nil {
		return err
	}
	if !modeEnabled(current) && !modeEnabled(pending) {
		return nil
	}
	if err := runSMI(ctx, "-i", gpuUUID, "-mig", "0"); err != nil {
		return fmt.Errorf("关闭 MIG 模式: %w", err)
	}
	current, _, err = queryMIGMode(ctx, gpuUUID)
	if err != nil {
		return err
	}
	if modeEnabled(current) {
		return ErrRebootRequired
	}
	return nil
}

func queryMIGMode(ctx context.Context, gpuUUID string) (current, pending string, err error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-i", gpuUUID, "--query-gpu=mig.mode.current,mig.mode.pending", "--format=csv,noheader")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		text := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if text == "" {
			text = runErr.Error()
		}
		return "", "", fmt.Errorf("读取 MIG 模式: %s", text)
	}
	line := strings.TrimSpace(stdout.String())
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("读取 MIG 模式: 无法解析 %q", line)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func modeEnabled(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "Enabled")
}

func runSMI(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "nvidia-smi", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("nvidia-smi %s: %s", strings.Join(args, " "), text)
	}
	return nil
}

func ignoreMissing(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no") && (strings.Contains(text, "instance") || strings.Contains(text, "device"))
}
