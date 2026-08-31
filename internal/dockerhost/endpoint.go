package dockerhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	APIVersion = "1.43"
	SocketPath = "/var/run/docker.sock"
)

func URL(socketPath string) (string, error) {
	if socketPath != SocketPath {
		return "", fmt.Errorf("Docker socket must be %s, got %s", SocketPath, socketPath)
	}
	return "unix://" + socketPath, nil
}

func Command(ctx context.Context, socketPath string, args ...string) (*exec.Cmd, error) {
	host, err := URL(socketPath)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, "docker", args...)
	command.Env = dockerEnvironment(host)
	return command, nil
}

func dockerEnvironment(host string) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "DOCKER_HOST", "DOCKER_API_VERSION", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH":
			continue
		}
		environment = append(environment, item)
	}
	return append(environment, "DOCKER_HOST="+host, "DOCKER_API_VERSION="+APIVersion)
}
