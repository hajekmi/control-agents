package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"control-agents/internal/registry"
)

const (
	resizeModeFixed                = "fixed"
	resizeModeFitOnce              = "fit-once"
	resizeModeFollowDevice         = "follow-device"
	defaultResizeViewersPerSession = 32
	defaultResizeViewersPerProcess = 256
	maxResizeViewerDimension       = 1000
)

var viewerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{15,127}$`)
var errResizeViewerCapacity = errors.New("resize viewer capacity reached")

type resizeSettings struct {
	Mode             string   `json:"mode"`
	SelectedViewerID ViewerID `json:"selectedViewerId,omitempty"`
}

type resizeViewer struct {
	ID        ViewerID
	SessionID string
	IP        string
	UserAgent string
	Width     int
	Height    int
	LastSeen  time.Time
}

type resizeStore struct {
	dir                  string
	viewerTTL            time.Duration
	now                  func() time.Time
	maxViewersPerSession int
	maxViewersPerProcess int

	mu      sync.Mutex
	viewers map[string]map[string]resizeViewer
}

func newResizeStore(dir string, viewerTTL time.Duration) *resizeStore {
	return &resizeStore{
		dir:                  dir,
		viewerTTL:            viewerTTL,
		now:                  time.Now,
		maxViewersPerSession: defaultResizeViewersPerSession,
		maxViewersPerProcess: defaultResizeViewersPerProcess,
		viewers:              make(map[string]map[string]resizeViewer),
	}
}

func validateResizeMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case resizeModeFixed, resizeModeFitOnce, resizeModeFollowDevice:
		return nil
	default:
		return fmt.Errorf("unsupported resize mode %q", mode)
	}
}

func normalizeResizeMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func (s *resizeStore) Load(sessionID string) (resizeSettings, error) {
	if !registry.ValidPublicRef(sessionID) {
		return resizeSettings{}, fmt.Errorf("invalid session id %q", sessionID)
	}
	data, err := os.ReadFile(s.path(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return resizeSettings{Mode: resizeModeFixed}, nil
	}
	if err != nil {
		return resizeSettings{}, fmt.Errorf("read resize settings: %w", err)
	}
	var settings resizeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return resizeSettings{}, fmt.Errorf("decode resize settings: %w", err)
	}
	settings.Mode = normalizeResizeMode(settings.Mode)
	if err := validateResizeMode(settings.Mode); err != nil {
		// Resize settings from the pre-foundation API never remain authoritative.
		return resizeSettings{Mode: resizeModeFixed}, nil
	}
	if settings.Mode == resizeModeFollowDevice {
		settings.SelectedViewerID = ""
	}
	return settings, nil
}

func (s *resizeStore) Save(sessionID string, settings resizeSettings) error {
	if !registry.ValidPublicRef(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	settings.Mode = normalizeResizeMode(settings.Mode)
	if err := validateResizeMode(settings.Mode); err != nil {
		return err
	}
	if settings.Mode == resizeModeFollowDevice {
		settings.SelectedViewerID = ""
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create resize dir: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("set resize dir permissions: %w", err)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode resize settings: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, sessionID+".*.tmp")
	if err != nil {
		return fmt.Errorf("create resize temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write resize temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close resize temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path(sessionID)); err != nil {
		return fmt.Errorf("save resize settings: %w", err)
	}
	return nil
}

func (s *resizeStore) RecordViewer(sessionID string, viewer resizeViewer) (resizeViewer, error) {
	if !registry.ValidPublicRef(sessionID) {
		return resizeViewer{}, fmt.Errorf("invalid session id %q", sessionID)
	}
	viewer.ID = ViewerID(strings.TrimSpace(string(viewer.ID)))
	if !viewerIDPattern.MatchString(string(viewer.ID)) {
		return resizeViewer{}, errors.New("invalid viewer id")
	}
	if viewer.Width <= 0 || viewer.Height <= 0 || viewer.Width > maxResizeViewerDimension || viewer.Height > maxResizeViewerDimension {
		return resizeViewer{}, errors.New("invalid viewer dimensions")
	}
	now := s.now()
	viewer.SessionID = sessionID
	viewer.LastSeen = now

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneAllLocked(now)
	sessionViewers := s.viewers[sessionID]
	if _, exists := sessionViewers[string(viewer.ID)]; !exists {
		if len(sessionViewers) >= s.maxViewersPerSession || s.viewerCountLocked() >= s.maxViewersPerProcess {
			return resizeViewer{}, errResizeViewerCapacity
		}
	}
	if s.viewers[sessionID] == nil {
		s.viewers[sessionID] = make(map[string]resizeViewer)
	}
	s.viewers[sessionID][string(viewer.ID)] = viewer
	return viewer, nil
}

func (s *resizeStore) Viewers(sessionID string) []resizeViewer {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(sessionID, now)
	sessionViewers := s.viewers[sessionID]
	viewers := make([]resizeViewer, 0, len(sessionViewers))
	for _, viewer := range sessionViewers {
		viewers = append(viewers, viewer)
	}
	return viewers
}

func (s *resizeStore) Viewer(sessionID string, viewerID ViewerID) (resizeViewer, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(sessionID, now)
	viewer, ok := s.viewers[sessionID][string(viewerID)]
	return viewer, ok
}

func (s *resizeStore) RemoveSession(sessionID string) error {
	if !registry.ValidPublicRef(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	s.ForgetSession(sessionID)
	if err := os.Remove(s.path(sessionID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove resize settings: %w", err)
	}
	return nil
}

func (s *resizeStore) ForgetSession(sessionID string) {
	s.mu.Lock()
	delete(s.viewers, sessionID)
	s.mu.Unlock()
}

func (s *resizeStore) pruneLocked(sessionID string, now time.Time) {
	if s.viewerTTL <= 0 {
		return
	}
	expiresBefore := now.Add(-s.viewerTTL)
	for id, viewer := range s.viewers[sessionID] {
		if viewer.LastSeen.Before(expiresBefore) {
			delete(s.viewers[sessionID], id)
		}
	}
	if len(s.viewers[sessionID]) == 0 {
		delete(s.viewers, sessionID)
	}
}

func (s *resizeStore) pruneAllLocked(now time.Time) {
	for sessionID := range s.viewers {
		s.pruneLocked(sessionID, now)
	}
}

func (s *resizeStore) viewerCountLocked() int {
	total := 0
	for _, viewers := range s.viewers {
		total += len(viewers)
	}
	return total
}

func (s *resizeStore) path(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".json")
}
