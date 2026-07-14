//go:build !windows

package process

func FindTCPListener(_ int) (PortOwner, error) { return PortOwner{}, nil }
func FindTCPListeners(ports []int) (map[int]PortOwner, error) {
	result := make(map[int]PortOwner, len(ports))
	for _, port := range ports {
		result[port] = PortOwner{}
	}
	return result, nil
}
func KillPortOwner(_ PortOwner) error { return nil }
