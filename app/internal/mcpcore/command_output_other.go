//go:build !windows

package mcpcore

func normalizeCommandOutput(data []byte) string {
	return string(data)
}
