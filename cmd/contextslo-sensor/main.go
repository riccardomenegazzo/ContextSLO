package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/truthsensor"
)

func main() {
	server := flag.String("server", env("CONTEXTSLO_SERVER", "http://contextslo.contextslo-system.svc:8080"), "ContextSLO server")
	token := flag.String("token", os.Getenv("CONTEXTSLO_API_TOKEN"), "API bearer token")
	cluster := flag.String("cluster", env("CONTEXTSLO_CLUSTER", "default"), "cluster name")
	flag.Parse()
	sensor, err := truthsensor.New()
	if err != nil {
		fatal(err)
	}
	defer sensor.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 10 * time.Second}
	resolver := newResolver(client, *server, *token)
	err = sensor.Run(ctx, func(event truthsensor.Event) error {
		marker, ok := resolver.marker(ctx, int(event.PID))
		if !ok {
			return nil
		}
		dimension := map[uint32]domain.Dimension{1: domain.Process, 2: domain.Filesystem, 3: domain.Network, 4: domain.DNS}[event.Type]
		truth := domain.TruthEvent{ID: marker + "-kernel-" + strconv.FormatUint(event.TimestampNS, 10), Marker: marker, Cluster: *cluster, Dimension: dimension, Kind: "ebpf-" + event.Command, OccurredAt: time.Now().UTC(), PID: int(event.PID), ParentPID: int(event.ParentPID), Evidence: event.Detail, Success: true, Attributes: map[string]string{"truth.provider": "ebpf", "process.command": event.Command}}
		return post(ctx, client, *server, *token, map[string]any{"events": []domain.TruthEvent{truth}})
	})
	if err != nil && ctx.Err() == nil {
		fatal(err)
	}
}

type resolver struct {
	client        *http.Client
	server, token string
	updated       time.Time
	pids          map[int]string
}

func newResolver(client *http.Client, server, token string) *resolver {
	return &resolver{client: client, server: server, token: token, pids: map[int]string{}}
}
func (r *resolver) marker(ctx context.Context, pid int) (string, bool) {
	if time.Since(r.updated) > 3*time.Second {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.server, "/")+"/api/v1/sessions", nil)
		if r.token != "" {
			request.Header.Set("Authorization", "Bearer "+r.token)
		}
		if response, err := r.client.Do(request); err == nil {
			var envelope struct {
				Sessions []domain.ValidationSession `json:"sessions"`
			}
			_ = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope)
			_ = response.Body.Close()
			next := map[int]string{}
			for _, session := range envelope.Sessions {
				if session.Status != "collecting" {
					continue
				}
				for _, event := range session.Truth {
					if event.PID > 0 {
						next[event.PID] = session.Marker
					}
				}
			}
			r.pids = next
			r.updated = time.Now()
		}
	}
	marker, ok := r.pids[pid]
	return marker, ok
}
func post(ctx context.Context, client *http.Client, server, token string, body any) error {
	data, _ := json.Marshal(body)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server, "/")+"/api/v1/truth", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("server returned %s", response.Status)
	}
	return nil
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "contextslo-sensor:", err); os.Exit(1) }
