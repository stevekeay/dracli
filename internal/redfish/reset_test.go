package redfish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestResetToDefaultsPreservesUsersAndNetwork(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		wantPath := "/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/DellManager.ResetToDefaults"
		if request.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", request.URL.Path, wantPath)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["ResetType"] != "Default" {
			t.Fatalf("payload = %s", body)
		}
		return jsonResponse(http.StatusOK, `{}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ResetToDefaults(context.Background(), "iDRAC.Embedded.1"); err != nil {
		t.Fatal(err)
	}
}
