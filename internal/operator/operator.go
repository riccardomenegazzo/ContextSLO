package operator

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	KubernetesURL, TokenFile, CAFile, Image, ContextSLOURL, Cluster, CanaryNamespace, TokenSecretName string
	PollInterval                                                                                      time.Duration
}

type Controller struct {
	config Config
	client *http.Client
	logger *slog.Logger
	mu     sync.Mutex
	last   map[string]time.Time
}

type sloSpec struct {
	Service string  `json:"service"`
	Target  float64 `json:"target"`
	Window  string  `json:"window"`
	Tier    string  `json:"tier"`
	Latency struct {
		P95 string `json:"p95"`
	} `json:"latency"`
	RequiredContext []string `json:"requiredContext"`
}

type sloList struct {
	Items []struct {
		Metadata struct {
			Name, Namespace string
			Annotations     map[string]string
		}
		Spec sloSpec `json:"spec"`
	} `json:"items"`
}
type jobList struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			CreationTimestamp time.Time         `json:"creationTimestamp"`
			Labels            map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			Active         int        `json:"active"`
			Succeeded      int        `json:"succeeded"`
			Failed         int        `json:"failed"`
			CompletionTime *time.Time `json:"completionTime"`
		} `json:"status"`
	} `json:"items"`
}

func New(config Config, logger *slog.Logger) (*Controller, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.TokenFile == "" {
		config.TokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}
	if config.CAFile == "" {
		config.CAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	}
	if config.CanaryNamespace == "" {
		config.CanaryNamespace = "contextslo-system"
	}
	if config.TokenSecretName == "" {
		config.TokenSecretName = "contextslo-api"
	}
	if config.KubernetesURL == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
		if port == "" {
			port = "443"
		}
		if host == "" {
			return nil, fmt.Errorf("Kubernetes API URL is required")
		}
		config.KubernetesURL = "https://" + host + ":" + port
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if ca, err := os.ReadFile(config.CAFile); err == nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &Controller{config: config, client: &http.Client{Transport: transport, Timeout: 20 * time.Second}, logger: logger, last: map[string]time.Time{}}, nil
}

func (c *Controller) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()
	for {
		if err := c.reconcile(ctx); err != nil {
			c.logger.Error("reconcile failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) error {
	var list sloList
	if err := c.kube(ctx, http.MethodGet, "/apis/contextslo.io/v1alpha1/securitycontextsloes", nil, &list); err != nil {
		return err
	}
	for _, item := range list.Items {
		key := item.Metadata.Namespace + "/" + item.Metadata.Name
		if err := c.refreshStatus(ctx, item.Metadata.Namespace, item.Metadata.Name); err != nil {
			c.logger.Warn("refresh canary status", "slo", key, "error", err)
		}
		interval := parseDuration(item.Metadata.Annotations["contextslo.io/interval"], 5*time.Minute)
		c.mu.Lock()
		due := time.Since(c.last[key]) >= interval
		if due {
			c.last[key] = time.Now()
		}
		c.mu.Unlock()
		if !due {
			continue
		}
		if err := c.createJob(ctx, item.Metadata.Namespace, item.Metadata.Name, item.Spec); err != nil {
			c.logger.Error("create canary job", "slo", key, "error", err)
			continue
		}
		_ = c.patchStatus(ctx, item.Metadata.Namespace, item.Metadata.Name, map[string]any{
			"phase": "Scheduled", "lastScheduledAt": time.Now().UTC().Format(time.RFC3339), "observedTarget": item.Spec.Target,
		})
	}
	return nil
}

func (c *Controller) refreshStatus(ctx context.Context, sloNamespace, sloName string) error {
	selector := url.QueryEscape("contextslo.io/slo=" + sloName + ",contextslo.io/slo-namespace=" + sloNamespace)
	var jobs jobList
	path := "/apis/batch/v1/namespaces/" + c.config.CanaryNamespace + "/jobs?labelSelector=" + selector
	if err := c.kube(ctx, http.MethodGet, path, nil, &jobs); err != nil {
		return err
	}
	if len(jobs.Items) == 0 {
		return nil
	}
	latest := jobs.Items[0]
	for _, job := range jobs.Items[1:] {
		if job.Metadata.CreationTimestamp.After(latest.Metadata.CreationTimestamp) {
			latest = job
		}
	}
	phase := "Scheduled"
	switch {
	case latest.Status.Succeeded > 0:
		phase = "Succeeded"
	case latest.Status.Failed > 0:
		phase = "Failed"
	case latest.Status.Active > 0:
		phase = "Running"
	}
	status := map[string]any{"phase": phase, "lastJob": latest.Metadata.Name}
	if latest.Status.CompletionTime != nil {
		status["lastCompletedAt"] = latest.Status.CompletionTime.UTC().Format(time.RFC3339)
	}
	return c.patchStatus(ctx, sloNamespace, sloName, status)
}

func (c *Controller) createJob(ctx context.Context, sloNamespace, sloName string, spec sloSpec) error {
	suffix := strconv.FormatInt(time.Now().Unix(), 36)
	name := truncateName("contextslo-" + sloName + "-" + suffix)
	target := spec.Target
	if target <= 0 || target > 100 {
		target = 99.5
	}
	latency := spec.Latency.P95
	if _, err := time.ParseDuration(latency); err != nil {
		latency = "10s"
	}
	required := strings.Join(spec.RequiredContext, ",")
	if required == "" {
		required = "process,filesystem,network,dns,kubernetes,cloudIdentity,api,mcp"
	}
	args := []string{
		"canary", "--server", c.config.ContextSLOURL, "--cluster", c.config.Cluster,
		"--service", spec.Service, "--target", strconv.FormatFloat(target, 'f', -1, 64),
		"--latency", latency, "--required-context", required,
	}
	env := []any{
		map[string]any{"name": "POD_NAMESPACE", "valueFrom": map[string]any{"fieldRef": map[string]string{"fieldPath": "metadata.namespace"}}},
		map[string]any{"name": "SERVICE_ACCOUNT", "value": "contextslo-canary"},
		map[string]any{"name": "CONTEXTSLO_API_TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": c.config.TokenSecretName, "key": "token", "optional": true}}},
	}
	job := map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{
			"name": name, "namespace": c.config.CanaryNamespace,
			"labels": map[string]string{"app.kubernetes.io/name": "contextslo-canary", "contextslo.io/slo": sloName, "contextslo.io/slo-namespace": sloNamespace},
		},
		"spec": map[string]any{
			"ttlSecondsAfterFinished": 300, "backoffLimit": 0,
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]string{"app.kubernetes.io/name": "contextslo-canary"}},
				"spec": map[string]any{
					"restartPolicy": "Never", "serviceAccountName": "contextslo-canary",
					"securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 65532, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
					"containers": []any{map[string]any{
						"name": "canary", "image": c.config.Image, "args": args, "env": env,
						"securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}},
					}},
				},
			},
		},
	}
	return c.kube(ctx, http.MethodPost, "/apis/batch/v1/namespaces/"+c.config.CanaryNamespace+"/jobs", job, nil)
}

func (c *Controller) patchStatus(ctx context.Context, namespace, name string, status map[string]any) error {
	return c.kubeWithType(ctx, http.MethodPatch, "/apis/contextslo.io/v1alpha1/namespaces/"+namespace+"/securitycontextsloes/"+name+"/status", map[string]any{"status": status}, nil, "application/merge-patch+json")
}

func (c *Controller) kube(ctx context.Context, method, path string, body, destination any) error {
	return c.kubeWithType(ctx, method, path, body, destination, "application/json")
}

func (c *Controller) kubeWithType(ctx context.Context, method, path string, body, destination any, contentType string) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.KubernetesURL, "/")+path, reader)
	if err != nil {
		return err
	}
	token, err := os.ReadFile(c.config.TokenFile)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", contentType)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Kubernetes API %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if destination != nil && len(data) > 0 {
		return json.Unmarshal(data, destination)
	}
	return nil
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Minute {
		return fallback
	}
	return parsed
}

func truncateName(value string) string {
	value = strings.Trim(strings.ToLower(value), "-")
	if len(value) > 63 {
		return strings.TrimRight(value[:63], "-")
	}
	return value
}
