//go:build !windows

package process

func FindTCPListener(_ int) (PortOwner, error) { return PortOwner{}, nil }
func KillPortOwner(_ PortOwner) error          { return nil }
