package web

import (
    "context"
    "net/http"
    "time"
)

func (s *Server) handleUnbindInstanceCloudflare(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
    defer cancel()
    result, err := s.app.UnbindInstanceCloudflare(ctx, r.PathValue("id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCheckCloudflaredUpdate(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
    defer cancel()
    result, err := s.app.CheckCloudflaredUpdate(ctx)
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleInstallCloudflaredUpdate(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
    defer cancel()
    result, err := s.app.InstallCloudflaredUpdate(ctx)
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}
