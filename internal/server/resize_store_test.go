package server

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateResizeMode(t *testing.T) {
	for _, mode := range []string{"off", "smallest", "web", "primary", " WEB "} {
		if err := validateResizeMode(normalizeResizeMode(mode)); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	for _, mode := range []string{"", "latest", "manual", "browser"} {
		if err := validateResizeMode(normalizeResizeMode(mode)); err == nil {
			t.Fatalf("mode %q accepted", mode)
		}
	}
}

func TestResizeStorePersistsSettings(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)

	err := store.Save("alpha", resizeSettings{Mode: " WEB ", SelectedViewerID: "viewer-1"})
	if err != nil {
		t.Fatal(err)
	}

	next := newResizeStore(store.dir, time.Minute)
	settings, err := next.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeWeb || settings.SelectedViewerID != "viewer-1" {
		t.Fatalf("settings = %#v, want web viewer-1", settings)
	}

	err = store.Save("alpha", resizeSettings{Mode: resizeModePrimary, SelectedViewerID: "viewer-1"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err = next.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModePrimary || settings.SelectedViewerID != "" {
		t.Fatalf("settings = %#v, want primary without selected viewer", settings)
	}
}

func TestResizeStoreExpiresViewers(t *testing.T) {
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 30*time.Second)
	store.now = func() time.Time { return now }

	_, err := store.RecordViewer("alpha", resizeViewer{ID: "old", Width: 80, Height: 24})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	_, err = store.RecordViewer("alpha", resizeViewer{ID: "new", Width: 100, Height: 30})
	if err != nil {
		t.Fatal(err)
	}

	viewers := store.Viewers("alpha")
	if len(viewers) != 1 || viewers[0].ID != "new" {
		t.Fatalf("viewers = %#v, want only new", viewers)
	}
	if _, ok := store.Viewer("alpha", "old"); ok {
		t.Fatal("expired viewer is still selectable")
	}
}
