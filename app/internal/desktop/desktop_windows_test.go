//go:build windows

package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenDashboardRequestsExistingNativeWindow(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		if request.URL.Path != "/api/ui/open" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		called <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	if err := OpenDashboard(server.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	default:
		t.Fatal("native window endpoint was not called")
	}
}
