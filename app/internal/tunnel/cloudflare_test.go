package tunnel

import "testing"

func TestRouteAlreadyConfigured(t *testing.T) {
	for _, input := range []string{
		"DNS record already configured",
		"Added CNAME mcp.example.com",
		"mcp.example.com will route to your tunnel",
	} {
		if !routeAlreadyConfigured(input) {
			t.Fatalf("expected success output: %s", input)
		}
	}
}
