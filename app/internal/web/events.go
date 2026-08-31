package web

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type stateEventHub struct {
	revision atomic.Uint64
	mu       sync.Mutex
	nextID   uint64
	clients  map[uint64]chan uint64
}

func newStateEventHub() *stateEventHub {
	return &stateEventHub{clients: make(map[uint64]chan uint64)}
}

func (h *stateEventHub) publish() uint64 {
	revision := h.revision.Add(1)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, client := range h.clients {
		select {
		case client <- revision:
		default:
		}
	}
	return revision
}

func (h *stateEventHub) subscribe() (uint64, <-chan uint64, func()) {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	client := make(chan uint64, 1)
	h.clients[id] = client
	h.mu.Unlock()

	return id, client, func() {
		h.mu.Lock()
		if current, ok := h.clients[id]; ok {
			delete(h.clients, id)
			close(current)
		}
		h.mu.Unlock()
	}
}

func (s *Server) handleStateEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming is unavailable"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "event: ready\ndata: {\"revision\":%d}\n\n", s.events.revision.Load())
	flusher.Flush()

	_, updates, unsubscribe := s.events.subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case revision, open := <-updates:
			if !open {
				return
			}
			_, _ = fmt.Fprintf(w, "event: state\ndata: {\"revision\":%d}\n\n", revision)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type mutationStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *mutationStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *mutationStatusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (s *Server) publishSuccessfulMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		writer := &mutationStatusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= 200 && status < 300 {
			s.events.publish()
		}
	})
}
