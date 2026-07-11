package process

type PortOwner struct {
	Occupied    bool
	PID         int
	ParentPID   int
	ProcessName string
	ProcessPath string
}
