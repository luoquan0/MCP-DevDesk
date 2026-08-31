package web

import (
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
