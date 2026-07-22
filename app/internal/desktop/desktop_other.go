//go:build !windows

package desktop

import (
	"errors"
	"os/exec"

	"mcp-devdesk/internal/model"
)

type otherController struct {
	url  string
	done chan struct{}
}

func New(url, _, _ string, _ Callbacks) Controller {
	return &otherController{url: url, done: make(chan struct{})}
}

func AcquireSingleInstance() (bool, func(), error) { return false, func() {}, nil }
func OpenDashboard(url string) error               { return exec.Command("xdg-open", url).Start() }
func SignalExistingInstance() bool                 { return false }
func (c *otherController) Start() error            { return nil }
func (c *otherController) Open() error             { return OpenDashboard(c.url) }
func (c *otherController) PickFolder(_, _ string) (string, bool, error) {
	return "", false, errors.New("folder selection is only available in the Windows desktop application")
}
func (c *otherController) Status() model.DesktopStatus {
	return model.DesktopStatus{Available: false, DashboardURL: c.url, WindowModeLabel: "系统默认浏览器"}
}
func (c *otherController) SetStartup(bool) error { return nil }
func (c *otherController) Done() <-chan struct{} { return c.done }
func (c *otherController) Close() error          { return nil }
