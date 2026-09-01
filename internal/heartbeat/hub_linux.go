//go:build linux

package heartbeat

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gpu-scheduler/internal/domain"
)

const (
	socketName     = "heartbeat.sock"
	dirPermission  = 0o750
	sockPermission = 0o666
)

type Hub struct {
	stateDir string
	mu       sync.Mutex
	items    map[string]*attemptServer
	latest   map[string]domain.TrainingHeartbeatReport
}

type attemptServer struct {
	server   *http.Server
	listener net.Listener
	dir      string
}

func NewHub(stateDir string) *Hub {
	return &Hub{
		stateDir: stateDir,
		items:    make(map[string]*attemptServer),
		latest:   make(map[string]domain.TrainingHeartbeatReport),
	}
}

func (h *Hub) Ensure(attemptID string) (string, error) {
	if attemptID == "" {
		return "", fmt.Errorf("attempt id is required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if item, ok := h.items[attemptID]; ok {
		return item.dir, nil
	}
	dir := Dir(h.stateDir, attemptID)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return "", err
	}
	sock := filepath.Join(dir, socketName)
	_ = os.Remove(sock)
	listener, err := net.Listen("unix", sock)
	if err != nil {
		return "", fmt.Errorf("listen heartbeat socket: %w", err)
	}
	if err := os.Chmod(sock, sockPermission); err != nil {
		_ = listener.Close()
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		h.handleReport(w, r, attemptID)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	h.items[attemptID] = &attemptServer{server: server, listener: listener, dir: dir}
	go func() { _ = server.Serve(listener) }()
	return dir, nil
}

func (h *Hub) CloseAttempt(attemptID string) error {
	h.mu.Lock()
	item, ok := h.items[attemptID]
	if ok {
		delete(h.items, attemptID)
		for key, report := range h.latest {
			if report.AttemptID == attemptID {
				delete(h.latest, key)
			}
		}
	}
	h.mu.Unlock()
	if !ok {
		return os.RemoveAll(Dir(h.stateDir, attemptID))
	}
	_ = item.server.Shutdown(context.Background())
	_ = item.listener.Close()
	return os.RemoveAll(item.dir)
}

func (h *Hub) Close() {
	h.mu.Lock()
	items := h.items
	h.items = make(map[string]*attemptServer)
	h.mu.Unlock()
	for _, item := range items {
		_ = item.server.Shutdown(context.Background())
		_ = item.listener.Close()
	}
}

func (h *Hub) Latest() []domain.TrainingHeartbeatReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]domain.TrainingHeartbeatReport, 0, len(h.latest))
	for _, report := range h.latest {
		result = append(result, report)
	}
	return result
}

func (h *Hub) handleReport(w http.ResponseWriter, r *http.Request, attemptID string) {
	var body struct {
		ServiceName string   `json:"serviceName"`
		Sequence    int      `json:"sequence"`
		Phase       string   `json:"phase"`
		Epoch       *int     `json:"epoch"`
		Step        *int     `json:"step"`
		Progress    *float64 `json:"progress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if body.Phase != "TRAINING" && body.Phase != "CHECKPOINTING" && body.Phase != "EVALUATING" {
		http.Error(w, "invalid phase", http.StatusUnprocessableEntity)
		return
	}
	if body.ServiceName == "" {
		body.ServiceName = "main"
	}
	report := domain.TrainingHeartbeatReport{
		AttemptID: attemptID, ServiceName: body.ServiceName, Sequence: body.Sequence,
		Phase: body.Phase, Epoch: body.Epoch, Step: body.Step, Progress: body.Progress,
		ReceivedAt: time.Now().UTC(),
	}
	key := attemptID + "\x00" + body.ServiceName
	h.mu.Lock()
	previous, exists := h.latest[key]
	if exists && body.Sequence > 0 && body.Sequence < previous.Sequence {
		h.mu.Unlock()
		http.Error(w, "stale sequence", http.StatusConflict)
		return
	}
	h.latest[key] = report
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
