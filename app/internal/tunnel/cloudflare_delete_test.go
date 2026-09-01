package tunnel

import (
    "reflect"
    "testing"
)

func TestDeleteTunnelArgumentsForceCascade(t *testing.T) {
    got := deleteTunnelArguments("12345678-1234-1234-1234-123456789abc")
    want := []string{"tunnel", "delete", "--force", "12345678-1234-1234-1234-123456789abc"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("deleteTunnelArguments()=%v want %v", got, want)
    }
}
