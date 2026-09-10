//go:build !windows

package mcpcore

import "io"

// RunScreenCaptureWorker is available only on Windows.
func RunScreenCaptureWorker(_ []string, _ io.Reader, _ io.Writer) (bool, int) {
	return false, 0
}
