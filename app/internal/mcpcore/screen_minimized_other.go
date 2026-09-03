//go:build !windows

package mcpcore

func platformListScreenWindowsForVision() ([]screenWindow, error) {
	return platformListScreenWindows()
}

func platformCaptureScreenWindowForVision(window screenWindow) (screenCaptureFrame, error) {
	return platformCaptureScreenWindow(window)
}
