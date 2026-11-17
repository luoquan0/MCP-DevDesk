//go:build linux

package application

import (
	"runtime"
	"testing"
)

func TestLinuxCloudflaredAssetMatchesNativeArchitecture(t *testing.T) {
	name, err := cloudflaredAssetName()
	switch runtime.GOARCH {
	case "amd64", "arm64":
		if err != nil {
			t.Fatal(err)
		}
		if want := "cloudflared-linux-" + runtime.GOARCH; name != want {
			t.Fatalf("asset = %q, want %q", name, want)
		}
	default:
		if err == nil {
			t.Fatal("unsupported architecture must not select a Windows asset")
		}
	}
}
