package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

const currentVersion = 2

type state struct {
	Version      int                                 `json:"version"`
	SLO          domain.SLO                          `json:"slo"`
	Runs         []domain.Run                        `json:"runs"`
	Sessions     map[string]domain.ValidationSession `json:"sessions,omitempty"`
	Observations []domain.Observation                `json:"observations,omitempty"`
	Clusters     map[string]domain.Cluster           `json:"clusters,omitempty"`
}
type Store struct {
	mu    sync.RWMutex
	path  string
	state state
}

func Open(path string, slo domain.SLO, seed []domain.Run) (*Store, error) {
	s := &Store{path: path, state: state{Version: currentVersion, SLO: slo, Runs: seed, Sessions: map[string]domain.ValidationSession{}, Clusters: map[string]domain.Cluster{}}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("decode state: %w", err)
		}
		s.initialize()
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
func (s *Store) initialize() {
	s.state.Version = currentVersion
	if s.state.Sessions == nil {
		s.state.Sessions = map[string]domain.ValidationSession{}
	}
	if s.state.Clusters == nil {
		s.state.Clusters = map[string]domain.Cluster{}
	}
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
	s.addRunLocked(run)
	return s.saveLocked()
}
func (s *Store) addRunLocked(run domain.Run) {
	for i, existing := range s.state.Runs {
		if existing.ID == run.ID {
			s.state.Runs[i] = run
			return
		}
	}
	s.state.Runs = append([]domain.Run{run}, s.state.Runs...)
	if len(s.state.Runs) > 500 {
		s.state.Runs = s.state.Runs[:500]
	}
}
func (s *Store) Runs() []domain.Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := append([]domain.Run(nil), s.state.Runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].CompletedAt.After(runs[j].CompletedAt) })
	return runs
}
func (s *Store) Get(id string) (domain.Run, bool) {
	for _, run := range s.Runs() {
		if run.ID == id {
			return run, true
		}
	}
	return domain.Run{}, false
}
func (s *Store) Baseline() float64 {
	for _, run := range s.Runs() {
		if run.Status == "passing" {
			return run.Score
		}
	}
	return 0
}

func (s *Store) CreateSession(session domain.ValidationSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.Marker == "" {
		return fmt.Errorf("session marker is required")
	}
	if _, exists := s.state.Sessions[session.Marker]; exists {
		return fmt.Errorf("session %s already exists", session.Marker)
	}
	if session.Status == "" {
		session.Status = "collecting"
	}
	s.state.Sessions[session.Marker] = session
	s.touchClusterLocked(session.Cluster, func(cluster *domain.Cluster) { cluster.ActiveSession++ })
	return s.saveLocked()
}
func (s *Store) Session(marker string) (domain.ValidationSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.state.Sessions[marker]
	if !ok {
		return domain.ValidationSession{}, false
	}
	session.Truth = append([]domain.TruthEvent(nil), session.Truth...)
	session.Observations = append([]domain.Observation(nil), session.Observations...)
	return session, true
}
func (s *Store) Sessions() []domain.ValidationSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessions := make([]domain.ValidationSession, 0, len(s.state.Sessions))
	for _, session := range s.state.Sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.After(sessions[j].CreatedAt) })
	return sessions
}
func (s *Store) AddTruth(event domain.TruthEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.state.Sessions[event.Marker]
	if !ok {
		return fmt.Errorf("session %s not found", event.Marker)
	}
	for _, existing := range session.Truth {
		if existing.ID == event.ID {
			return nil
		}
	}
	session.Truth = append(session.Truth, event)
	s.state.Sessions[event.Marker] = session
	s.touchClusterLocked(event.Cluster, func(cluster *domain.Cluster) { cluster.TruthEvents++ })
	return s.saveLocked()
}
func (s *Store) AddObservation(event domain.Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.state.Observations {
		if existing.ID == event.ID {
			return nil
		}
	}
	s.state.Observations = append(s.state.Observations, event)
	if len(s.state.Observations) > 10000 {
		s.state.Observations = s.state.Observations[len(s.state.Observations)-10000:]
	}
	if session, ok := s.state.Sessions[event.Marker]; ok {
		session.Observations = append(session.Observations, event)
		s.state.Sessions[event.Marker] = session
	}
	s.touchClusterLocked(event.Cluster, func(cluster *domain.Cluster) { cluster.Observations++ })
	return s.saveLocked()
}
func (s *Store) CompleteSession(marker string, run domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.state.Sessions[marker]
	if !ok {
		return fmt.Errorf("session %s not found", marker)
	}
	if session.Status != "completed" {
		s.touchClusterLocked(session.Cluster, func(cluster *domain.Cluster) {
			if cluster.ActiveSession > 0 {
				cluster.ActiveSession--
			}
		})
	}
	session.Status = "completed"
	session.CompletedAt = run.CompletedAt
	session.RunID = run.ID
	s.state.Sessions[marker] = session
	s.addRunLocked(run)
	return s.saveLocked()
}
func (s *Store) Clusters() []domain.Cluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clusters := make([]domain.Cluster, 0, len(s.state.Clusters))
	for _, cluster := range s.state.Clusters {
		clusters = append(clusters, cluster)
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Name < clusters[j].Name })
	return clusters
}
func (s *Store) touchClusterLocked(name string, update func(*domain.Cluster)) {
	if name == "" {
		name = "unknown"
	}
	cluster := s.state.Clusters[name]
	cluster.Name = name
	cluster.LastSeenAt = time.Now().UTC()
	update(&cluster)
	s.state.Clusters[name] = cluster
}

func (s *Store) Overview() domain.Overview {
	runs := s.Runs()
	overview := domain.Overview{SLO: s.SLO(), History: runs, TotalRuns: len(runs)}
	if len(runs) > 0 {
		overview.Latest = runs[0]
		overview.LastValidatedAt = runs[0].CompletedAt
	}
	limit := len(runs)
	if limit > 30 {
		limit = 30
	}
	sum := 0.0
	for i, run := range runs {
		if run.Status == "passing" {
			overview.PassingRuns++
		}
		if i < limit {
			sum += run.Score
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
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
func mathRound(value float64) float64 { return float64(int(value*10+0.5)) / 10 }
