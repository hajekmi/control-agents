package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

type SessionRef string
type WindowRef string
type PaneRef string
type ViewerID string

type PaneGeneration struct {
	TmuxServerStart string
	TmuxServerPID   int
	PaneID          string
}

type publicWindow struct {
	Ref           WindowRef `json:"ref"`
	Name          string    `json:"name"`
	Active        bool      `json:"active"`
	Panes         int       `json:"panes"`
	ActivePaneRef PaneRef   `json:"activePaneRef,omitempty"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
}

type publicTopology struct {
	Windows         []publicWindow
	ActiveWindowRef WindowRef
	ActivePaneRef   PaneRef
	WindowWidth     int
	WindowHeight    int
}

type paneBinding struct {
	ref        PaneRef
	rawID      string
	windowID   string
	active     bool
	generation PaneGeneration
}

type windowBinding struct {
	ref   WindowRef
	rawID string
}

type sessionIdentity struct {
	canonical  string
	panes      map[PaneRef]paneBinding
	paneRefs   map[string]PaneRef
	windows    map[WindowRef]windowBinding
	windowRefs map[string]WindowRef
}

type identityStore struct {
	mu       sync.Mutex
	sessions map[SessionRef]*sessionIdentity
}

var errStaleTerminalIdentity = errors.New("stale terminal identity")

func newIdentityStore() *identityStore {
	return &identityStore{sessions: make(map[SessionRef]*sessionIdentity)}
}

func (s *identityStore) refresh(ctx context.Context, client *tmux.Client, managed registry.Session) (publicTopology, error) {
	topology, err := client.Topology(ctx, managed.TmuxName)
	if err != nil {
		return publicTopology{}, err
	}
	ref := SessionRef(managed.PublicRef)
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := s.sessions[ref]
	if identity == nil || identity.canonical != managed.ID {
		identity = &sessionIdentity{
			canonical:  managed.ID,
			panes:      make(map[PaneRef]paneBinding),
			paneRefs:   make(map[string]PaneRef),
			windows:    make(map[WindowRef]windowBinding),
			windowRefs: make(map[string]WindowRef),
		}
		s.sessions[ref] = identity
	}
	return identity.update(topology)
}

func (s *identityStore) resolvePane(ctx context.Context, client *tmux.Client, managed registry.Session, ref PaneRef, active bool) (paneBinding, error) {
	if ref == "" {
		return paneBinding{}, errStaleTerminalIdentity
	}
	if _, err := s.refresh(ctx, client, managed); err != nil {
		return paneBinding{}, err
	}
	sessionRef := SessionRef(managed.PublicRef)
	s.mu.Lock()
	identity := s.sessions[sessionRef]
	binding, ok := identity.panes[ref]
	s.mu.Unlock()
	if !ok || (active && !binding.active) {
		return paneBinding{}, errStaleTerminalIdentity
	}
	expected := tmux.PaneGeneration{
		ServerStart: binding.generation.TmuxServerStart,
		ServerPID:   binding.generation.TmuxServerPID,
		PaneID:      binding.generation.PaneID,
	}
	if err := client.VerifyPaneGeneration(ctx, binding.rawID, expected); err != nil {
		return paneBinding{}, errStaleTerminalIdentity
	}
	return binding, nil
}

func (s *identityStore) resolveWindow(managed registry.Session, ref WindowRef) (windowBinding, error) {
	if ref == "" {
		return windowBinding{}, errStaleTerminalIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := s.sessions[SessionRef(managed.PublicRef)]
	if identity == nil {
		return windowBinding{}, errStaleTerminalIdentity
	}
	binding, ok := identity.windows[ref]
	if !ok {
		return windowBinding{}, errStaleTerminalIdentity
	}
	return binding, nil
}

func (s *identityStore) forget(ref SessionRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, ref)
}

func (s *sessionIdentity) update(topology tmux.Topology) (publicTopology, error) {
	livePanes := make(map[string]bool)
	liveWindows := make(map[string]bool)
	windowOrder := make([]string, 0)
	windowMeta := make(map[string]tmux.TopologyPane)
	windowPanes := make(map[string][]paneBinding)

	for _, pane := range topology.Panes {
		livePanes[pane.ID] = true
		liveWindows[pane.WindowID] = true
		if _, ok := windowMeta[pane.WindowID]; !ok {
			windowOrder = append(windowOrder, pane.WindowID)
			windowMeta[pane.WindowID] = pane
		}
		windowRef, ok := s.windowRefs[pane.WindowID]
		if !ok {
			windowRef = WindowRef(newOpaqueRuntimeRef("w"))
			s.windowRefs[pane.WindowID] = windowRef
			s.windows[windowRef] = windowBinding{ref: windowRef, rawID: pane.WindowID}
		}
		generation := PaneGeneration{TmuxServerStart: topology.ServerStart, TmuxServerPID: topology.ServerPID, PaneID: pane.ID}
		paneRef, ok := s.paneRefs[pane.ID]
		if ok {
			previous := s.panes[paneRef]
			if previous.generation != generation {
				delete(s.panes, paneRef)
				delete(s.paneRefs, pane.ID)
				ok = false
			}
		}
		if !ok {
			paneRef = PaneRef(newOpaqueRuntimeRef("p"))
			s.paneRefs[pane.ID] = paneRef
		}
		binding := paneBinding{
			ref:        paneRef,
			rawID:      pane.ID,
			windowID:   pane.WindowID,
			active:     pane.Active && pane.WindowOpen,
			generation: generation,
		}
		s.panes[paneRef] = binding
		windowPanes[pane.WindowID] = append(windowPanes[pane.WindowID], binding)
	}

	for raw, ref := range s.paneRefs {
		if !livePanes[raw] {
			delete(s.paneRefs, raw)
			delete(s.panes, ref)
		}
	}
	for raw, ref := range s.windowRefs {
		if !liveWindows[raw] {
			delete(s.windowRefs, raw)
			delete(s.windows, ref)
		}
	}

	result := publicTopology{Windows: make([]publicWindow, 0, len(windowOrder))}
	for _, rawWindowID := range windowOrder {
		meta := windowMeta[rawWindowID]
		window := publicWindow{
			Ref:    s.windowRefs[rawWindowID],
			Name:   meta.WindowName,
			Active: meta.WindowOpen,
			Panes:  len(windowPanes[rawWindowID]),
			Width:  meta.Width,
			Height: meta.Height,
		}
		for _, pane := range windowPanes[rawWindowID] {
			if pane.active {
				window.ActivePaneRef = pane.ref
			}
		}
		if window.Active {
			result.ActiveWindowRef = window.Ref
			result.ActivePaneRef = window.ActivePaneRef
			result.WindowWidth = window.Width
			result.WindowHeight = window.Height
		}
		result.Windows = append(result.Windows, window)
	}
	if result.ActivePaneRef == "" || result.ActiveWindowRef == "" {
		return publicTopology{}, errStaleTerminalIdentity
	}
	return result, nil
}

func newOpaqueRuntimeRef(prefix string) string {
	random := make([]byte, 18)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(random)
}
