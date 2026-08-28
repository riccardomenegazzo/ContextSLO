package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

type Request struct{ Marker, Cluster, Service, DNSName, CloudURL, MCPURL, Token string }
type Runner struct {
	Client       *http.Client
	Now          func() time.Time
	ChildCommand []string
}

func New() *Runner { return &Runner{Client: &http.Client{Timeout: 5 * time.Second}, Now: time.Now} }

func (r *Runner) Run(ctx context.Context, req Request) ([]domain.TruthEvent, error) {
	if req.Marker == "" {
		return nil, fmt.Errorf("marker is required")
	}
	if req.Cluster == "" {
		req.Cluster = "local"
	}
	events := make([]domain.TruthEvent, 0, 8)
	add := func(d domain.Dimension, kind, evidence string, success bool, err error, attrs map[string]string) {
		event := domain.TruthEvent{ID: req.Marker + "-" + string(d), Marker: req.Marker, Cluster: req.Cluster, Dimension: d, Kind: kind, OccurredAt: r.Now().UTC(), PID: os.Getpid(), ParentPID: os.Getppid(), Workload: env("HOSTNAME", "local-process"), Namespace: env("POD_NAMESPACE", "local"), ServiceAccount: env("SERVICE_ACCOUNT", "local"), Evidence: evidence, Success: success, Attributes: attrs}
		if err != nil {
			event.Error = err.Error()
		}
		events = append(events, event)
	}

	command := append([]string(nil), r.ChildCommand...)
	if len(command) == 0 {
		executable, err := os.Executable()
		if err != nil {
			add(domain.Process, "execve", "resolve executable", false, err, nil)
		} else {
			command = []string{executable, "probe-child"}
		}
	}
	if len(command) > 0 {
		cmd := exec.CommandContext(ctx, command[0], append(command[1:], req.Marker)...)
		output, cmdErr := cmd.CombinedOutput()
		add(domain.Process, "execve", fmt.Sprintf("pid=%d child=%s", os.Getpid(), bytes.TrimSpace(output)), cmdErr == nil, cmdErr, map[string]string{"executable": command[0]})
	}

	path := filepath.Join(os.TempDir(), "contextslo-"+req.Marker)
	payload := []byte(req.Marker + "\n")
	writeErr := os.WriteFile(path, payload, 0600)
	read, readErr := os.ReadFile(path)
	_ = os.Remove(path)
	fileErr := writeErr
	if fileErr == nil {
		fileErr = readErr
	}
	add(domain.Filesystem, "file-read", path+" bytes="+strconv.Itoa(len(read)), fileErr == nil, fileErr, map[string]string{"path": path})

	dnsName := req.DNSName
	if dnsName == "" {
		dnsName = "localhost"
	}
	addresses, dnsErr := net.DefaultResolver.LookupHost(ctx, dnsName)
	add(domain.DNS, "dns-query", dnsName+" → "+fmt.Sprint(addresses), dnsErr == nil, dnsErr, map[string]string{"dns.question.name": dnsName})

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr == nil {
		accepted := make(chan error, 1)
		go func() {
			conn, e := listener.Accept()
			if e == nil {
				_, e = io.Copy(io.Discard, io.LimitReader(conn, 64))
				_ = conn.Close()
			}
			accepted <- e
		}()
		conn, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 2*time.Second)
		if dialErr == nil {
			_, dialErr = conn.Write([]byte(req.Marker))
			_ = conn.Close()
		}
		acceptErr := <-accepted
		_ = listener.Close()
		if dialErr == nil {
			dialErr = acceptErr
		}
		add(domain.Network, "tcp-connect", listener.Addr().String(), dialErr == nil, dialErr, map[string]string{"network.peer.address": listener.Addr().String()})
	} else {
		add(domain.Network, "tcp-connect", "listener", false, listenErr, nil)
	}

	apiListener, apiErr := net.Listen("tcp", "127.0.0.1:0")
	if apiErr == nil {
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			w.Header().Set("X-ContextSLO-Marker", q.Header.Get("X-ContextSLO-Marker"))
			w.WriteHeader(http.StatusNoContent)
		})}
		go server.Serve(apiListener)
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+apiListener.Addr().String()+"/v1/context-canary", bytes.NewBufferString(req.Marker))
		request.Header.Set("X-ContextSLO-Marker", req.Marker)
		response, callErr := r.Client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		_ = server.Shutdown(context.Background())
		add(domain.API, "http-request", "POST /v1/context-canary", callErr == nil, callErr, map[string]string{"http.route": "/v1/context-canary"})
	} else {
		add(domain.API, "http-request", "local API", false, apiErr, nil)
	}

	kubeAttrs := map[string]string{"k8s.pod.name": env("HOSTNAME", "local-process"), "k8s.namespace.name": env("POD_NAMESPACE", "local"), "k8s.serviceaccount.name": env("SERVICE_ACCOUNT", "local")}
	add(domain.Kubernetes, "workload-identity", kubeAttrs["k8s.namespace.name"]+"/"+kubeAttrs["k8s.pod.name"], true, nil, kubeAttrs)

	cloudSuccess, cloudEvidence, cloudErr := r.call(ctx, req.CloudURL, req.Token, req.Marker, nil)
	add(domain.CloudIAM, "cloud-identity", cloudEvidence, cloudSuccess, cloudErr, map[string]string{"cloud.provider": "configured-http"})
	mcpBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.Marker, "method": "tools/call", "params": map[string]any{"name": "contextslo_canary", "arguments": map[string]string{"marker": req.Marker}}})
	mcpSuccess, mcpEvidence, mcpErr := r.call(ctx, req.MCPURL, req.Token, req.Marker, mcpBody)
	add(domain.MCP, "mcp-tool-call", mcpEvidence, mcpSuccess, mcpErr, map[string]string{"mcp.tool.name": "contextslo_canary"})
	return events, nil
}

func (r *Runner) call(ctx context.Context, url, token, marker string, body []byte) (bool, string, error) {
	if url == "" {
		return false, "integration endpoint is not configured", fmt.Errorf("endpoint not configured")
	}
	method := http.MethodGet
	var reader io.Reader
	if body != nil {
		method = http.MethodPost
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return false, url, err
	}
	request.Header.Set("X-ContextSLO-Marker", marker)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := r.Client.Do(request)
	if err != nil {
		return false, url, err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, string(data), fmt.Errorf("endpoint returned %s", response.Status)
	}
	return true, string(data), nil
}
func Child(marker string) {
	fmt.Printf("%s pid=%d ppid=%d os=%s\n", marker, os.Getpid(), os.Getppid(), runtime.GOOS)
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
