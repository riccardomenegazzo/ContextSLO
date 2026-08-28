package canary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/probe"
)

type Config struct {
	Server, Token, Marker, Cluster, Service, DNSName, CloudURL, MCPURL, IntegrationToken string
	Wait                                                                                 time.Duration
	SLO                                                                                  *domain.SLO
}

func Run(ctx context.Context, config Config) error {
	if config.Server == "" {
		return fmt.Errorf("server URL is required")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	marker := strings.ToUpper(config.Marker)
	if marker == "" {
		var session domain.ValidationSession
		input := map[string]any{"mode": "live", "cluster": config.Cluster, "service": config.Service}
		if config.SLO != nil {
			input["slo"] = config.SLO
		}
		if err := request(ctx, client, config, http.MethodPost, "/api/v1/sessions", input, &session); err != nil {
			return err
		}
		marker = session.Marker
	}
	events, err := probe.New().Run(ctx, probe.Request{Marker: marker, Cluster: config.Cluster, Service: config.Service, DNSName: config.DNSName, CloudURL: config.CloudURL, MCPURL: config.MCPURL, Token: config.IntegrationToken})
	if err != nil {
		return err
	}
	if err = request(ctx, client, config, http.MethodPost, "/api/v1/truth", map[string]any{"events": events}, nil); err != nil {
		return err
	}
	if config.Wait <= 0 {
		config.Wait = 5 * time.Second
	}
	timer := time.NewTimer(config.Wait)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
	}
	var run domain.Run
	if err = request(ctx, client, config, http.MethodPost, "/api/v1/sessions/"+marker+"/correlate", map[string]any{}, &run); err != nil {
		return err
	}
	fmt.Printf("%s score=%.1f status=%s breaks=%d\n", marker, run.Score, run.Status, len(run.Breaks))
	if run.Status != "passing" {
		return fmt.Errorf("context SLO failed at %.1f%%", run.Score)
	}
	return nil
}
func request(ctx context.Context, client *http.Client, config Config, method, path string, body any, destination any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(config.Server, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+config.Token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ContextSLO returned %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}
	if destination != nil {
		return json.Unmarshal(responseBody, destination)
	}
	return nil
}
