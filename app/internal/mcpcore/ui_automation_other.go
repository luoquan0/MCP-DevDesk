//go:build !windows

package mcpcore

import "errors"

func platformReadUIAutomationTree(window screenWindow, maxDepth, maxNodes int) ([]uiAutomationNode, bool, error) {
	return nil, false, errors.New("Windows UI Automation semantic reading is only available on Windows")
}
