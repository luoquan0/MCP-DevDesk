package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStartupCommandsProduceValidJSON(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "startup-on", want: true},
		{command: "startup-off", want: false},
	}

	for _, test := range tests {
		method, path, body, err := commandRequest(test.command)
		if err != nil {
			t.Fatalf("commandRequest(%q): %v", test.command, err)
		}
		if method != http.MethodPut || path != "/api/system/startup" {
			t.Fatalf("unexpected request for %q: %s %s", test.command, method, path)
		}
		var payload struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			t.Fatalf("decode %q body: %v", test.command, err)
		}
		if payload.Enabled != test.want {
			t.Fatalf("%q enabled = %v, want %v", test.command, payload.Enabled, test.want)
		}
	}
}

func TestDesktopCommand(t *testing.T) {
	method, path, body, err := commandRequest("desktop")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/api/system/desktop" || body != nil {
		t.Fatalf("unexpected desktop command: %s %s %#v", method, path, body)
	}
}
