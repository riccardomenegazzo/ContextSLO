package operator

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRefreshStatusTracksCompletedJob(t *testing.T) {
	var status map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if got := r.URL.Query().Get("labelSelector"); got != "contextslo.io/slo=orders-context,contextslo.io/slo-namespace=shop" {
				t.Errorf("selector = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"older","creationTimestamp":"2026-01-01T00:00:00Z"},"status":{"failed":1}},{"metadata":{"name":"latest","creationTimestamp":"2026-01-02T00:00:00Z"},"status":{"succeeded":1,"completionTime":"2026-01-02T00:00:05Z"}}]}`))
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
				t.Errorf("decode status: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("method = %s", r.Method)
		}
	}))
	defer api.Close()
	tokenFile := t.TempDir() + "/token"
	if err := os.WriteFile(tokenFile, []byte("kube-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller, err := New(Config{KubernetesURL: api.URL, TokenFile: tokenFile, CanaryNamespace: "contextslo-system"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.refreshStatus(context.Background(), "shop", "orders-context"); err != nil {
		t.Fatal(err)
	}
	patched := status["status"].(map[string]any)
	if patched["phase"] != "Succeeded" || patched["lastJob"] != "latest" || patched["lastCompletedAt"] == nil {
		t.Fatalf("status = %#v", patched)
	}
}
