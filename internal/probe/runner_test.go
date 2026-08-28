package probe

import (
	"context"
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunnerGeneratesRealLocalTruth(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer endpoint.Close()
	runner := New()
	runner.ChildCommand = []string{"/bin/echo"}
	events, err := runner.Run(context.Background(), Request{Marker: "CTX-123456", Cluster: "test", CloudURL: endpoint.URL, MCPURL: endpoint.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(domain.DimensionOrder) {
		t.Fatalf("got %d events", len(events))
	}
	for _, event := range events {
		if !event.Success {
			t.Errorf("%s failed: %s", event.Dimension, event.Error)
		}
	}
}
