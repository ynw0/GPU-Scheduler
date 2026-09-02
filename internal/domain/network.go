package domain

import (
	"fmt"
	"strings"
)

func ValidateDockerNetwork(network string) error {
	name := strings.TrimSpace(network)
	if name == "" {
		return nil
	}
	lower := strings.ToLower(name)
	if lower == "host" || strings.HasPrefix(lower, "container:") || strings.HasPrefix(lower, "ns:") {
		return fmt.Errorf("禁止网络 %s", name)
	}
	return nil
}
