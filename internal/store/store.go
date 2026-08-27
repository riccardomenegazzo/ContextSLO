package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

type state struct {
	SLO  domain.SLO   `json:"slo"`
	Runs []domain.Run `json:"runs"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	state state
}

func Open(path string, slo domain.SLO, seed []domain.Run) (*Store, error) {
	s := &Store{path: path, state: state{SLO: slo, Runs: seed}}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, err
		}
		return s, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err = s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) SLO() domain.SLO { s.mu.RLock(); defer s.mu.RUnlock(); return s.state.SLO }
func (s *Store) UpdateSLO(slo domain.SLO) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.SLO = slo
	return s.saveLocked()
}
func (s *Store) Add(run domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Runs = append([]domain.Run{run}, s.state.Runs...)
	if len(s.state.Runs) > 50 {
		s.state.Runs = s.state.Runs[:50]
	}
	return s.saveLocked()
}
func (s *Store) Runs() []domain.Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := append([]domain.Run(nil), s.state.Runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].CompletedAt.After(runs[j].CompletedAt) })
	return runs
}
func (s *Store) Get(id string) (domain.Run, bool) {
	for _, r := range s.Runs() {
		if r.ID == id {
			return r, true
		}
	}
	return domain.Run{}, false
}
func (s *Store) Baseline() float64 {
	runs := s.Runs()
	for _, r := range runs {
		if r.Status == "passing" {
			return r.Score
		}
	}
	if len(runs) > 0 {
		return runs[0].Score
	}
	return 0
}

func (s *Store) Overview() domain.Overview {
	runs := s.Runs()
	overview := domain.Overview{SLO: s.SLO(), History: runs, TotalRuns: len(runs)}
	if len(runs) > 0 {
		overview.Latest = runs[0]
		overview.LastValidatedAt = runs[0].CompletedAt
	}
	limit := len(runs)
	if limit > 12 {
		limit = 12
	}
	sum := 0.0
	for i, r := range runs {
		if r.Status == "passing" {
			overview.PassingRuns++
		}
		if i < limit {
			sum += r.Score
		}
	}
	if limit > 0 {
		overview.ContextCoverage = mathRound(sum / float64(limit))
	}
	return overview
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
func mathRound(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
