package executor

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"gpu-scheduler/internal/domain"
)

type SecurityValidator struct {
	allowedMountRoots []string
}

func NewSecurityValidator(allowedMountRoots []string) (*SecurityValidator, error) {
	resolved := make([]string, 0, len(allowedMountRoots))
	for _, root := range allowedMountRoots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, filepath.Clean(absolute))
	}
	return &SecurityValidator{allowedMountRoots: resolved}, nil
}

func (v *SecurityValidator) ValidateImageMounts(mounts []domain.Mount) error {
	for _, mount := range mounts {
		if err := v.validateVolume("image workload", map[string]any{
			"type": "bind", "source": mount.Source, "target": mount.Target,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (v *SecurityValidator) ValidateCompose(model map[string]any) error {
	services, ok := model["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return fmt.Errorf("compose services are required")
	}
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("service %s is malformed", name)
		}
		if truthy(service["privileged"]) || truthy(service["use_api_socket"]) {
			return fmt.Errorf("service %s requests privileged Docker access", name)
		}
		if _, exists := service["build"]; exists {
			return fmt.Errorf("service %s uses build; only local images are allowed", name)
		}
		for _, field := range []string{"pid", "ipc", "cgroup", "network_mode"} {
			if strings.EqualFold(stringValue(service[field]), "host") {
				return fmt.Errorf("service %s uses forbidden %s=host", name, field)
			}
		}
		for _, capability := range stringSlice(service["cap_add"]) {
			if slices.Contains([]string{"ALL", "SYS_ADMIN"}, strings.ToUpper(capability)) {
				return fmt.Errorf("service %s requests forbidden capability %s", name, capability)
			}
		}
		if devices := anySlice(service["devices"]); len(devices) > 0 {
			return fmt.Errorf("service %s maps host devices directly", name)
		}
		for _, rawVolume := range anySlice(service["volumes"]) {
			if err := v.validateVolume(name, rawVolume); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *SecurityValidator) validateVolume(service string, raw any) error {
	volume, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("service %s volume must use normalized long syntax", service)
	}
	if stringValue(volume["type"]) != "bind" {
		return nil
	}
	source := filepath.Clean(stringValue(volume["source"]))
	if source == "" {
		return fmt.Errorf("service %s bind mount has no source", service)
	}
	lower := strings.ToLower(filepath.ToSlash(source))
	if lower == "/" || strings.HasSuffix(lower, "/docker.sock") {
		return fmt.Errorf("service %s requests forbidden mount %s", service, source)
	}
	for _, root := range v.allowedMountRoots {
		relative, err := filepath.Rel(root, source)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("service %s mount %s is outside allowed roots", service, source)
}

func truthy(value any) bool        { result, _ := value.(bool); return result }
func stringValue(value any) string { result, _ := value.(string); return result }
func anySlice(value any) []any     { result, _ := value.([]any); return result }
func stringSlice(value any) []string {
	items := anySlice(value)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			result = append(result, value)
		}
	}
	return result
}
