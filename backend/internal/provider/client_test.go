package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListMachinesUsesRawAuthorizationAndToleratesNestedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lb/lightsail/page" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "raw-panel-token" {
			t.Fatalf("unexpected authorization: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"records": []any{map[string]any{"id": "1783916711346432", "publicIpAddress": "203.0.113.10", "regionName": "Tokyo"}}}})
	}))
	defer server.Close()

	client, err := New(server.URL, "raw-panel-token", "raw_authorization")
	if err != nil {
		t.Fatal(err)
	}
	machines, err := client.ListMachines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(machines) != 1 || machines[0].ID != "1783916711346432" {
		t.Fatalf("unexpected machines: %#v", machines)
	}
	if machines[0].PublicIPv4 != "203.0.113.10" {
		t.Fatalf("unexpected IP: %q", machines[0].PublicIPv4)
	}
}
