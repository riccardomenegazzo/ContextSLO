package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/canary"
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/operator"
	"github.com/riccardomenegazzo/ContextSLO/internal/probe"
	"github.com/riccardomenegazzo/ContextSLO/internal/server"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
)

const version = "1.0.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "contextslo:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	command := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "serve":
		return serve(args)
	case "validate":
		return validate(args)
	case "canary":
		return runCanary(args)
	case "operator":
		return runOperator(args)
	case "probe-child":
		if len(args) != 1 {
			return fmt.Errorf("probe-child requires a marker")
		}
		probe.Child(args[0])
		return nil
	case "version":
		fmt.Println("contextslo " + version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", env("CONTEXTSLO_LISTEN", ":8080"), "HTTP listen address")
	data := flags.String("data", env("CONTEXTSLO_DATA", "./data/state.json"), "state file path")
	seed := flags.Bool("seed-demo", envBool("CONTEXTSLO_SEED_DEMO", true), "seed demonstration history for an empty store")
	wait := flags.Duration("collection-wait", envDuration("CONTEXTSLO_COLLECTION_WAIT", 2*time.Second), "live telemetry collection window")
	if err := flags.Parse(args); err != nil {
		return err
	}
	logger := newLogger()
	slo := defaultSLO()
	eng := engine.New()
	var seeds []domain.Run
	if *seed {
		seeds = seedRuns(eng, slo)
	}
	st, err := store.Open(*data, slo, seeds)
	if err != nil {
		return err
	}
	config := server.Config{APIToken: os.Getenv("CONTEXTSLO_API_TOKEN"), CloudProbeURL: os.Getenv("CONTEXTSLO_CLOUD_PROBE_URL"), MCPProbeURL: os.Getenv("CONTEXTSLO_MCP_PROBE_URL"), IntegrationToken: os.Getenv("CONTEXTSLO_INTEGRATION_TOKEN"), DNSName: env("CONTEXTSLO_DNS_NAME", "localhost"), CollectionWait: *wait, MaxCollectionWait: envDuration("CONTEXTSLO_MAX_COLLECTION_WAIT", 30*time.Second)}
	httpServer := &http.Server{Addr: *listen, Handler: server.NewWithConfig(st, eng, probe.New(), logger, config).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("ContextSLO listening", "address", *listen, "data", *data, "auth", config.APIToken != "")
		errCh <- httpServer.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err = <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	scenario := flags.String("scenario", "baseline", "demo scenario")
	jsonOutput := flags.Bool("json", false, "print complete JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	run := engine.New().Run(defaultSLO(), *scenario, 100)
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(run)
	}
	printRun(run)
	if run.Status != "passing" {
		return fmt.Errorf("context SLO failed")
	}
	return nil
}
func runCanary(args []string) error {
	flags := flag.NewFlagSet("canary", flag.ContinueOnError)
	config := canary.Config{}
	var target float64
	var latency time.Duration
	var requiredContext string
	flags.StringVar(&config.Server, "server", env("CONTEXTSLO_SERVER", "http://contextslo.contextslo-system.svc:8080"), "ContextSLO API URL")
	flags.StringVar(&config.Token, "token", os.Getenv("CONTEXTSLO_API_TOKEN"), "API bearer token")
	flags.StringVar(&config.Marker, "marker", os.Getenv("CONTEXTSLO_MARKER"), "existing session marker")
	flags.StringVar(&config.Cluster, "cluster", env("CONTEXTSLO_CLUSTER", "default"), "cluster name")
	flags.StringVar(&config.Service, "service", env("CONTEXTSLO_SERVICE", "canary"), "service name")
	flags.StringVar(&config.DNSName, "dns-name", env("CONTEXTSLO_DNS_NAME", "kubernetes.default.svc"), "DNS probe target")
	flags.StringVar(&config.CloudURL, "cloud-url", os.Getenv("CONTEXTSLO_CLOUD_PROBE_URL"), "cloud identity probe URL")
	flags.StringVar(&config.MCPURL, "mcp-url", os.Getenv("CONTEXTSLO_MCP_PROBE_URL"), "MCP JSON-RPC URL")
	flags.StringVar(&config.IntegrationToken, "integration-token", os.Getenv("CONTEXTSLO_INTEGRATION_TOKEN"), "integration bearer token")
	flags.DurationVar(&config.Wait, "wait", envDuration("CONTEXTSLO_COLLECTION_WAIT", 5*time.Second), "telemetry collection window")
	flags.Float64Var(&target, "target", 0, "session context integrity target")
	flags.DurationVar(&latency, "latency", 10*time.Second, "session p95 latency objective")
	flags.StringVar(&requiredContext, "required-context", "", "comma-separated required context dimensions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if target > 0 {
		dimensions, err := parseDimensions(requiredContext)
		if err != nil {
			return err
		}
		config.SLO = &domain.SLO{Name: config.Service + "-context", Service: config.Service, Cluster: config.Cluster, Target: target, LatencyP95MS: int(latency.Milliseconds()), RequiredContext: dimensions, Window: "30d", Tier: "Tier 1"}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return canary.Run(ctx, config)
}
func runOperator(args []string) error {
	flags := flag.NewFlagSet("operator", flag.ContinueOnError)
	config := operator.Config{}
	flags.StringVar(&config.KubernetesURL, "kubernetes-url", os.Getenv("KUBERNETES_API_URL"), "Kubernetes API URL")
	flags.StringVar(&config.Image, "image", env("CONTEXTSLO_IMAGE", "ghcr.io/riccardomenegazzo/contextslo:latest"), "canary image")
	flags.StringVar(&config.ContextSLOURL, "server", env("CONTEXTSLO_SERVER", "http://contextslo.contextslo-system.svc:8080"), "ContextSLO API URL")
	flags.StringVar(&config.TokenSecretName, "token-secret", env("CONTEXTSLO_TOKEN_SECRET", "contextslo-api"), "Secret containing the optional API token")
	flags.StringVar(&config.Cluster, "cluster", env("CONTEXTSLO_CLUSTER", "default"), "cluster name")
	flags.StringVar(&config.CanaryNamespace, "canary-namespace", env("CONTEXTSLO_CANARY_NAMESPACE", "contextslo-system"), "namespace for canary Jobs")
	flags.DurationVar(&config.PollInterval, "poll", envDuration("CONTEXTSLO_OPERATOR_POLL", 30*time.Second), "reconciliation interval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	controller, err := operator.New(config, newLogger())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return controller.Run(ctx)
}

func printRun(run domain.Run) {
	fmt.Printf("%s  %s\n", run.Marker, run.Status)
	fmt.Printf("Context integrity: %.1f%% (target %.1f%%)\n", run.Score, run.Target)
	fmt.Printf("Telemetry p95: %dms\n", run.LatencyP95MS)
	for _, dimension := range run.Dimensions {
		fmt.Printf("  %-16s %5.1f%%\n", dimension.Dimension, dimension.Score)
	}
	if len(run.Breaks) > 0 {
		fmt.Println("Context breaks:")
		for _, item := range run.Breaks {
			fmt.Printf("  %s → %s: %s\n", item.From, item.To, item.Explanation)
		}
	}
}
func defaultSLO() domain.SLO {
	return domain.SLO{Name: "orders-api-context", Service: "orders-api", Cluster: env("CONTEXTSLO_CLUSTER", "production-eu"), Target: 99.5, LatencyP95MS: 10000, RequiredContext: domain.DimensionOrder, Window: "30d", Tier: "Tier 1"}
}
func seedRuns(eng *engine.Engine, slo domain.SLO) []domain.Run {
	scenarios := []string{"identity-gap", "mcp-gap", "baseline", "baseline", "mcp-gap", "baseline"}
	runs := make([]domain.Run, 0, len(scenarios))
	for i, scenario := range scenarios {
		run := eng.Run(slo, scenario, 100)
		run.ID = fmt.Sprintf("demo-%02d", i+1)
		run.CompletedAt = run.CompletedAt.Add(-time.Duration(i) * 24 * time.Hour)
		run.StartedAt = run.StartedAt.Add(-time.Duration(i) * 24 * time.Hour)
		runs = append(runs, run)
	}
	return runs
}
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if env("CONTEXTSLO_LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}
func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func parseDimensions(value string) ([]domain.Dimension, error) {
	if strings.TrimSpace(value) == "" {
		return append([]domain.Dimension(nil), domain.DimensionOrder...), nil
	}
	known := map[domain.Dimension]bool{}
	for _, dimension := range domain.DimensionOrder {
		known[dimension] = true
	}
	seen := map[domain.Dimension]bool{}
	dimensions := []domain.Dimension{}
	for _, item := range strings.Split(value, ",") {
		dimension := domain.Dimension(strings.TrimSpace(item))
		if !known[dimension] {
			return nil, fmt.Errorf("unknown context dimension %q", item)
		}
		if !seen[dimension] {
			seen[dimension] = true
			dimensions = append(dimensions, dimension)
		}
	}
	return dimensions, nil
}
func usage() {
	fmt.Printf("ContextSLO %s — reliability engineering for security context\n\nUsage:\n  contextslo serve [options]\n  contextslo validate [--scenario baseline] [--json]\n  contextslo canary [--server URL] [--cluster NAME] [--service NAME]\n  contextslo operator [--image IMAGE] [--server URL]\n  contextslo version\n", version)
}
