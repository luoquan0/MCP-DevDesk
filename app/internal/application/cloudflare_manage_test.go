package application

import "testing"

func TestCompareCloudflaredVersions(t *testing.T) {
    cases := []struct {
        left string
        right string
        want int
    }{
        {"2026.8.2", "2026.8.3", -1},
        {"2026.8.3", "2026.8.3", 0},
        {"2026.9.0", "2026.8.3", 1},
        {"v2026.8.3", "2026.8.3", 0},
    }
    for _, tc := range cases {
        got := compareCloudflaredVersions(tc.left, tc.right)
        if got != tc.want {
            t.Fatalf("compareCloudflaredVersions(%q, %q)=%d want %d", tc.left, tc.right, got, tc.want)
        }
    }
}

func TestNormalizeCloudflareCNAMEContent(t *testing.T) {
    got := normalizeCloudflareCNAMEContent("  ABCD.cfargotunnel.com. ")
    if got != "abcd.cfargotunnel.com" {
        t.Fatalf("normalizeCloudflareCNAMEContent()=%q", got)
    }
}

func TestCloudflaredVersionPattern(t *testing.T) {
    match := cloudflaredVersionPattern.FindStringSubmatch("cloudflared version 2026.8.3 (built 2026-08-31)")
    if len(match) < 2 || match[1] != "2026.8.3" {
        t.Fatalf("unexpected version match: %#v", match)
    }
}
