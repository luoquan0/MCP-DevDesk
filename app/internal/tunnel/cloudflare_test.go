package tunnel

import (
	"errors"
	"reflect"
	"testing"
)

func TestDNSRouteArgumentsOverwriteExistingRecord(t *testing.T) {
	const tunnelID = "11111111-2222-3333-4444-555555555555"
	const domain = "mcp2.example.com"

	got := dnsRouteArguments(tunnelID, domain)
	want := []string{
		"tunnel",
		"route",
		"dns",
		"--overwrite-dns",
		tunnelID,
		domain,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dns route arguments mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestLegacyDNSRouteArguments(t *testing.T) {
	const tunnelID = "11111111-2222-3333-4444-555555555555"
	const domain = "mcp2.example.com"
	got := legacyDNSRouteArguments(tunnelID, domain)
	want := []string{"tunnel", "route", "dns", tunnelID, domain}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy dns route arguments mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestOverwriteDNSFlagUnsupported(t *testing.T) {
	err := errors.New("exit status 1")
	if !overwriteDNSFlagUnsupported("Incorrect Usage: flag provided but not defined: -overwrite-dns", err) {
		t.Fatal("expected old cloudflared overwrite flag failure to use legacy fallback")
	}
	if overwriteDNSFlagUnsupported("failed to create route: permission denied", err) {
		t.Fatal("unrelated route errors must not silently fall back")
	}
}
