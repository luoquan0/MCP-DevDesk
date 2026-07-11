package process

import "testing"

func TestParseCloudflaredCommandLine(t *testing.T) {
	command := `"C:\Program Files\cloudflared.exe" tunnel run --credentials-file "C:\Users\demo\.cloudflared\abcd-1234.json" --protocol http2 --url http://127.0.0.1:9876 mcp-devdesk`
	process := parseCloudflaredCommandLine(command)
	if process.TunnelName != "mcp-devdesk" {
		t.Fatalf("tunnel name = %q", process.TunnelName)
	}
	if process.TunnelID != "abcd-1234" {
		t.Fatalf("tunnel id = %q", process.TunnelID)
	}
	if process.LocalHost != "127.0.0.1" || process.LocalPort != 9876 {
		t.Fatalf("local target = %s:%d", process.LocalHost, process.LocalPort)
	}
	if process.CredentialsPath != `C:\Users\demo\.cloudflared\abcd-1234.json` {
		t.Fatalf("credentials = %q", process.CredentialsPath)
	}
}

func TestParseCloudflaredEqualsFlags(t *testing.T) {
	command := `cloudflared.exe tunnel run --credentials-file=C:\cf\id.json --url=http://localhost:8765 named-tunnel`
	process := parseCloudflaredCommandLine(command)
	if process.LocalPort != 8765 || process.TunnelID != "id" || process.TunnelName != "named-tunnel" {
		t.Fatalf("unexpected process: %#v", process)
	}
}

func TestCloudflaredTokenIsRedacted(t *testing.T) {
	process := parseCloudflaredCommandLine(`cloudflared.exe tunnel run --token super-secret-token`)
	if process.CommandLine != "cloudflared.exe tunnel run --token ***" {
		t.Fatalf("command line was not redacted: %q", process.CommandLine)
	}
}

func TestCloudflaredTunnelCommandDetection(t *testing.T) {
	for _, command := range []string{
		`cloudflared.exe tunnel run mcp-devdesk`,
		`cloudflared.exe tunnel --url http://127.0.0.1:8765`,
	} {
		if !isCloudflaredTunnelCommand(command) {
			t.Fatalf("expected tunnel command: %q", command)
		}
	}
	for _, command := range []string{
		`cloudflared.exe tunnel login`,
		`cloudflared.exe tunnel list`,
		`cloudflared.exe --version`,
	} {
		if isCloudflaredTunnelCommand(command) {
			t.Fatalf("unexpected tunnel process command: %q", command)
		}
	}
}
