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

func ComposeCommand(ctx context.Context, socketPath string, args ...string) (*exec.Cmd, error) {
	host, err := URL(socketPath)
	if err != nil {
		return nil, err
	}
	plugin, err := composePlugin()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, plugin, args...)
	command.Env = dockerEnvironment(host)
	return command, nil
}

func composePlugin() (string, error) {
	for _, path := range []string{
		"/usr/libexec/docker/cli-plugins/docker-compose",
		"/usr/lib/docker/cli-plugins/docker-compose",
		"/usr/local/lib/docker/cli-plugins/docker-compose",
	} {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("Docker Compose plugin not found")
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
