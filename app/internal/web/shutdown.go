package web

// BeginShutdown releases long-lived handlers shared by the desktop and LAN
// control servers before either HTTP server enters graceful shutdown. SSE
// connections otherwise keep Shutdown waiting for its deadline.
func (s *Server) BeginShutdown() {
	if s.events != nil {
		s.events.stop()
	}
}
