//go:build !windows

package selfupdate

import "time"

func waitForProcessExit(_ int, _ time.Duration) error { return nil }
