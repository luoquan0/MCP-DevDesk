//go:build !windows

package mcpcore

import "errors"

var errScreenCaptureUnsupported = errors.New("Screen Vision is currently supported only on Windows")

func platformListScreenWindows() ([]screenWindow, error) {
	return nil, errScreenCaptureUnsupported
}

func platformActiveScreenWindow() (screenWindow, error) {
	return screenWindow{}, errScreenCaptureUnsupported
}

func platformCaptureScreenWindow(screenWindow) (screenCaptureFrame, error) {
	return screenCaptureFrame{}, errScreenCaptureUnsupported
}

func platformCaptureScreenDesktop() (screenCaptureFrame, error) {
	return screenCaptureFrame{}, errScreenCaptureUnsupported
}
