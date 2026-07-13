package desktop

import "mcp-devdesk/internal/model"

type Callbacks struct {
	Open    func()
	Start   func()
	Stop    func()
	Restart func()
}

type Controller interface {
	Start() error
	Open() error
	PickFolder(initialPath, title string) (path string, canceled bool, err error)
	Status() model.DesktopStatus
	SetStartup(enabled bool) error
	Done() <-chan struct{}
	Close() error
}
