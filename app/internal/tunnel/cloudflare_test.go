package tunnel

import (
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
