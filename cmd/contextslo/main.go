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
	"syscall"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/server"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "contextslo:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	command := "serve"
	if len(args) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "serve":
		return serve(args)
	case "validate":
		return validate(args)
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slo := defaultSLO()
	eng := engine.New()
	st, err := store.Open(*data, slo, seedRuns(eng, slo))
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: *listen, Handler: server.New(st, eng, logger).Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("ContextSLO listening", "address", *listen, "data", *data)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", "error", err)
			errCh <- err
			return
		}
		errCh <- nil
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	scenario := flags.String("scenario", "baseline", "baseline, identity-gap, mcp-gap, or latency-regression")
	jsonOutput := flags.Bool("json", false, "print complete JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	run := engine.New().Run(defaultSLO(), *scenario, 100)
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(run)
	}
	fmt.Printf("%s  %s\n", run.Marker, run.Status)
	fmt.Printf("Context integrity: %.1f%% (target %.1f%%)\n", run.Score, run.Target)
	fmt.Printf("Telemetry p95: %dms\n", run.LatencyP95MS)
	for _, d := range run.Dimensions {
		fmt.Printf("  %-16s %5.1f%%\n", d.Dimension, d.Score)
	}
	if len(run.Breaks) > 0 {
		fmt.Println("Context breaks:")
		for _, b := range run.Breaks {
			fmt.Printf("  %s → %s: %s\n", b.From, b.To, b.Explanation)
		}
	}
	if run.Status != "passing" {
		return fmt.Errorf("context SLO failed")
	}
	return nil
}

func defaultSLO() domain.SLO {
	return domain.SLO{Name: "orders-api-context", Service: "orders-api", Cluster: "production-eu", Target: 99.5, LatencyP95MS: 10000, RequiredContext: domain.DimensionOrder, Window: "30d", Tier: "Tier 1"}
}
func seedRuns(eng *engine.Engine, slo domain.SLO) []domain.Run {
	scenarios := []string{"identity-gap", "mcp-gap", "baseline", "baseline", "mcp-gap", "baseline"}
	runs := make([]domain.Run, 0, len(scenarios))
	for i, scenario := range scenarios {
		r := eng.Run(slo, scenario, 100)
		r.ID = fmt.Sprintf("demo-%02d", i+1)
		r.CompletedAt = r.CompletedAt.Add(-time.Duration(i) * 24 * time.Hour)
		r.StartedAt = r.StartedAt.Add(-time.Duration(i) * 24 * time.Hour)
		runs = append(runs, r)
	}
	return runs
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func usage() {
	fmt.Printf("ContextSLO %s — reliability engineering for security context\n\nUsage:\n  contextslo serve [--listen :8080] [--data ./data/state.json]\n  contextslo validate [--scenario baseline] [--json]\n  contextslo version\n", version)
}
