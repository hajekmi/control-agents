package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

func TestPublicTopologyUsesOpaqueReferencesAndDisplayMetadataOnly(t *testing.T) {
	identity := &sessionIdentity{
		canonical:  "alpha",
		panes:      make(map[PaneRef]paneBinding),
		paneRefs:   make(map[string]PaneRef),
		windows:    make(map[WindowRef]windowBinding),
		windowRefs: make(map[string]WindowRef),
	}
	topology, err := identity.update(tmux.Topology{
		ServerStart: "100",
		ServerPID:   101,
		Panes: []tmux.TopologyPane{{
			ID: "%42", WindowID: "@7", Active: true, WindowOpen: true,
			WindowName: "shell; $(id)", Width: 120, Height: 40,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if topology.ActivePaneRef == "" || topology.ActiveWindowRef == "" || strings.Contains(string(topology.ActivePaneRef), "%42") || strings.Contains(string(topology.ActiveWindowRef), "@7") {
		t.Fatalf("public references expose tmux targets: %#v", topology)
	}
	encoded, err := json.Marshal(topology.Windows)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "%42") || strings.Contains(text, "@7") || strings.Contains(text, "pane_id") || strings.Contains(text, "target") {
		t.Fatalf("public topology exposes internal target: %s", text)
	}
	if !strings.Contains(text, `"name":"shell; $(id)"`) {
		t.Fatalf("display-only window name missing: %s", text)
	}
}

func TestPaneGenerationServerPIDChangeInvalidatesOldOpaqueReference(t *testing.T) {
	runner := &identityRunner{serverStart: "100", serverPID: 101}
	client := tmux.NewClientWithRunner(runner)
	store := newIdentityStore()
	managed := registry.Session{ID: "alpha", PublicRef: testSessionRef, TmuxName: "alpha"}
	first, err := store.refresh(context.Background(), client, managed)
	if err != nil {
		t.Fatal(err)
	}
	oldRef := first.ActivePaneRef
	if _, err := store.resolvePane(context.Background(), client, managed, oldRef, true); err != nil {
		t.Fatalf("live generation rejected: %v", err)
	}

	// Simulate a tmux server restarting within the same start_time second and
	// reusing the same pane ID. The server PID still separates the incarnation.
	runner.serverPID = 202
	if _, err := store.resolvePane(context.Background(), client, managed, oldRef, true); !errors.Is(err, errStaleTerminalIdentity) {
		t.Fatalf("old generation error = %v, want stale identity", err)
	}
	second, err := store.refresh(context.Background(), client, managed)
	if err != nil {
		t.Fatal(err)
	}
	if second.ActivePaneRef == oldRef {
		t.Fatalf("pane reference survived generation change: %q", oldRef)
	}
}

func TestPaneReferenceCannotCrossManagedSessionBoundary(t *testing.T) {
	runner := &identityRunner{serverStart: "100", serverPID: 101}
	client := tmux.NewClientWithRunner(runner)
	store := newIdentityStore()
	alpha := registry.Session{ID: "alpha", PublicRef: testSessionRef, TmuxName: "alpha"}
	beta := registry.Session{ID: "beta", PublicRef: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TmuxName: "beta"}
	alphaTopology, err := store.refresh(context.Background(), client, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.refresh(context.Background(), client, beta); err != nil {
		t.Fatal(err)
	}
	if _, err := store.resolvePane(context.Background(), client, beta, alphaTopology.ActivePaneRef, true); !errors.Is(err, errStaleTerminalIdentity) {
		t.Fatalf("cross-session reference error = %v, want stale identity", err)
	}
}

type identityRunner struct {
	serverStart string
	serverPID   int
}

func (r *identityRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}
	switch {
	case args[0] == "list-panes":
		paneID := "%42"
		windowID := "@7"
		if targetArgument(args) == "beta" {
			paneID = "%84"
			windowID = "@14"
		}
		return []byte(windowID + "\x1f" + paneID + "\x1f1\x1f1\x1f120\x1f40\x1fshell\n"), nil
	case args[0] == "display-message" && strings.Contains(args[len(args)-1], "#{pane_id}"):
		return []byte(r.serverStart + "\x1f" + strconv.Itoa(r.serverPID) + "\x1f" + targetArgument(args) + "\n"), nil
	case args[0] == "display-message":
		return []byte(r.serverStart + "\x1f" + strconv.Itoa(r.serverPID) + "\n"), nil
	default:
		return nil, errors.New("unsupported command")
	}
}

func (*identityRunner) Run(context.Context, string, ...string) error {
	return nil
}

func targetArgument(args []string) string {
	for index := range args {
		if args[index] == "-t" && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}
