//go:build windows

package mcpcore

import "testing"

func TestNormalizeCommandOutputPreservesUTF8(t *testing.T) {
	input := []byte("中文 UTF-8 output ✓")
	if got := normalizeCommandOutputWithCodePage(input, 936); got != string(input) {
		t.Fatalf("valid UTF-8 changed: got %q want %q", got, string(input))
	}
}

func TestNormalizeCommandOutputDecodesCP936(t *testing.T) {
	// "中文输出" encoded as Windows code page 936 / GBK.
	input := []byte{0xD6, 0xD0, 0xCE, 0xC4, 0xCA, 0xE4, 0xB3, 0xF6}
	if got := normalizeCommandOutputWithCodePage(input, 936); got != "中文输出" {
		t.Fatalf("CP936 decode mismatch: got %q", got)
	}
}

func TestNormalizeCommandOutputPreservesASCII(t *testing.T) {
	input := []byte("plain ASCII output\r\n")
	if got := normalizeCommandOutputWithCodePage(input, 437); got != string(input) {
		t.Fatalf("ASCII changed: got %q want %q", got, string(input))
	}
}
