package store

import (
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	slo := domain.SLO{Name: "orders", Target: 99.5}
	s, err := Open(path, slo, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: "run-1", Score: 100, Status: "passing", CompletedAt: time.Now()}
	if err = s.Add(run); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, domain.SLO{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Get("run-1"); !ok || got.Score != 100 {
		t.Fatalf("run was not persisted: %#v %v", got, ok)
	}
}

func TestOverviewUsesRecentWindow(t *testing.T) {
	runs := []domain.Run{{ID: "new", Score: 80, CompletedAt: time.Now()}, {ID: "old", Score: 100, CompletedAt: time.Now().Add(-time.Hour), Status: "passing"}}
	s, _ := Open("", domain.SLO{}, runs)
	overview := s.Overview()
	if overview.Latest.ID != "new" || overview.ContextCoverage != 90 {
		t.Fatalf("unexpected overview: %#v", overview)
	}
}
