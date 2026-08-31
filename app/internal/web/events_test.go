package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStateEventHubBroadcastsRevision(t *testing.T) {
	hub := newStateEventHub()
	_, updates, unsubscribe := hub.subscribe()
	defer unsubscribe()

	want := hub.publish()
	select {
	case got := <-updates:
		if got != want {
			t.Fatalf("revision = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("state event was not delivered to subscriber")
	}
}

func TestStateEventsExitWhenShutdownBegins(t *testing.T) {
	server := &Server{events: newStateEventHub()}
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	response := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		server.handleStateEvents(response, request)
		close(done)
	}()

	server.BeginShutdown()
	server.BeginShutdown() // shutdown must be safe to request more than once.

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("state event stream did not exit after shutdown began")
	}
}
