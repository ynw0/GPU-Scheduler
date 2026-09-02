//go:build linux

package mig

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

func Apply(ctx context.Context, gpuUUID string, memoryBytes uint64, profiles []string) error {
	catalog := CatalogForMemory(memoryBytes)
	if _, err := Evaluate(catalog, profiles); err != nil {
		return err
	}
	if err := runSMI(ctx, "-i", gpuUUID, "-mig", "1"); err != nil {
		return fmt.Errorf("enable MIG: %w", err)
	}
	_ = runSMI(ctx, "--gpu-reset", "-i", gpuUUID)
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dci"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("destroy compute instances: %w", err)
	}
	if err := runSMI(ctx, "mig", "-i", gpuUUID, "-dgi"); err != nil && !ignoreMissing(err) {
		return fmt.Errorf("destroy GPU instances: %w", err)
	}
	if len(profiles) == 0 {
		if err := runSMI(ctx, "-i", gpuUUID, "-mig", "0"); err != nil {
			return fmt.Errorf("disable MIG: %w", err)
		}
		_ = runSMI(ctx, "--gpu-reset", "-i", gpuUUID)
		return nil
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
		return fmt.Errorf("create instances: %w", err)
	}
	return nil
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
