package controller

import (
	"sync"
	"time"

	"gpu-scheduler/internal/domain"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan domain.StreamEvent]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[chan domain.StreamEvent]struct{}{}} }

func (h *Hub) Subscribe() chan domain.StreamEvent {
	ch := make(chan domain.StreamEvent, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan domain.StreamEvent) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Publish(eventType string, payload any) {
	event := domain.StreamEvent{Type: eventType, At: time.Now().UTC().Format(time.RFC3339), Payload: payload}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
		}
	}
}
