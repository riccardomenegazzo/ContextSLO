package operator

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCreateJobUsesSLOSnapshotAndSystemNamespace(t *testing.T) {
	t.Helper()
	var path string
	var job map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer kube-token" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			t.Errorf("decode job: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer api.Close()

	tokenFile := t.TempDir() + "/token"
	if err := os.WriteFile(tokenFile, []byte("kube-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller, err := New(Config{KubernetesURL: api.URL, TokenFile: tokenFile, Image: "contextslo:test", ContextSLOURL: "http://contextslo:8080", Cluster: "eu", CanaryNamespace: "contextslo-system", TokenSecretName: "contextslo-api"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var spec sloSpec
	spec.Service = "orders"
	spec.Target = 99.9
	spec.Latency.P95 = "3s"
	spec.RequiredContext = []string{"process", "network"}
	if err := controller.createJob(context.Background(), "shop", "orders-context", spec); err != nil {
		t.Fatal(err)
	}
	if path != "/apis/batch/v1/namespaces/contextslo-system/jobs" {
		t.Fatalf("path = %q", path)
	}
	metadata := job["metadata"].(map[string]any)
	if metadata["namespace"] != "contextslo-system" {
		t.Fatalf("namespace = %v", metadata["namespace"])
	}
	specMap := job["spec"].(map[string]any)
	template := specMap["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	container := podSpec["containers"].([]any)[0].(map[string]any)
	args := container["args"].([]any)
	joined := make([]string, len(args))
	for i := range args {
		joined[i] = args[i].(string)
	}
	command := strings.Join(joined, " ")
	for _, expected := range []string{"--service orders", "--target 99.9", "--latency 3s", "--required-context process,network"} {
		if !strings.Contains(command, expected) {
			t.Errorf("args %q missing %q", command, expected)
		}
	}
	env := container["env"].([]any)
	encoded, _ := json.Marshal(env)
	if strings.Contains(string(encoded), "kube-token") || !strings.Contains(string(encoded), "contextslo-api") {
		t.Fatalf("unexpected token configuration: %s", encoded)
	}
}
