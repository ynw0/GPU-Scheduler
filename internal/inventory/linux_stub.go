//go:build linux

package inventory

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"gpu-scheduler/internal/domain"
)

type LinuxDiscoverer struct{}

func NewLinuxDiscoverer() *LinuxDiscoverer { return &LinuxDiscoverer{} }

func (d *LinuxDiscoverer) HandleTopology(w http.ResponseWriter, _ *http.Request) {
	hostname, _ := os.Hostname()
	topology := domain.Topology{Hostname: hostname, ObservedAt: time.Now().UTC()}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(topology)
}

