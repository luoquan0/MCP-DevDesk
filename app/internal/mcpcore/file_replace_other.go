//go:build !windows

package mcpcore

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
