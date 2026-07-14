package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testSessionRef = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testViewerID   = "viewer_1234567890"
)

func TestValidateResizeMode(t *testing.T) {
	for _, mode := range []string{"fixed", "fit-once", "follow-device", " FIXED "} {
		if err := validateResizeMode(normalizeResizeMode(mode)); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	for _, mode := range []string{"", "off", "smallest", "web", "primary", "latest", "manual"} {
		if err := validateResizeMode(normalizeResizeMode(mode)); err == nil {
			t.Fatalf("mode %q accepted", mode)
		}
	}
}

func TestResizeStorePersistsFixedStateAndLastFitViewer(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	if err := store.Save(testSessionRef, resizeSettings{Mode: " FIXED ", SelectedViewerID: testViewerID}); err != nil {
		t.Fatal(err)
	}

	next := newResizeStore(store.dir, time.Minute)
	settings, err := next.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFixed || settings.SelectedViewerID != testViewerID {
		t.Fatalf("settings = %#v, want fixed with selected viewer", settings)
	}

	if err := store.Save(testSessionRef, resizeSettings{Mode: resizeModeFollowDevice, SelectedViewerID: testViewerID}); err != nil {
		t.Fatal(err)
	}
	settings, err = next.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFollowDevice || settings.SelectedViewerID != "" {
		t.Fatalf("settings = %#v, want follow-device without selected viewer", settings)
	}
}

func TestResizeStoreMigratesPreFoundationModeToFixed(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(testSessionRef), []byte(`{"mode":"smallest"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFixed {
		t.Fatalf("settings = %#v, want fixed migration", settings)
	}
}

func TestResizeStoreExpiresViewers(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 30*time.Second)
	store.now = func() time.Time { return now }

	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	newViewerID := ViewerID("viewer_0987654321")
	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: newViewerID, Width: 100, Height: 30}); err != nil {
		t.Fatal(err)
	}

	viewers := store.Viewers(testSessionRef)
	if len(viewers) != 1 || viewers[0].ID != newViewerID {
		t.Fatalf("viewers = %#v, want only new viewer", viewers)
	}
	if _, ok := store.Viewer(testSessionRef, testViewerID); ok {
		t.Fatal("expired viewer is still selectable")
	}
}

func TestResizeStoreRemoveSessionClearsSettingsAndViewers(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	if err := store.Save(testSessionRef, resizeSettings{Mode: resizeModeFixed}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSession(testSessionRef); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFixed || len(store.Viewers(testSessionRef)) != 0 {
		t.Fatalf("settings/viewers = %#v/%#v", settings, store.Viewers(testSessionRef))
	}
}

func TestResizeStoreBoundsViewerStateWithoutEvictingActiveEntries(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	store.maxViewersPerSession = 1
	store.maxViewersPerProcess = 2
	secondSession := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	thirdSession := "cccccccccccccccccccccccccccccccc"
	secondViewer := ViewerID("viewer_0987654321")
	thirdViewer := ViewerID("viewer_1122334455")

	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: secondViewer, Width: 90, Height: 25}); !errors.Is(err, errResizeViewerCapacity) {
		t.Fatalf("per-session capacity error = %v", err)
	}
	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 81, Height: 26}); err != nil {
		t.Fatalf("existing viewer refresh was rejected: %v", err)
	}
	if _, err := store.RecordViewer(secondSession, resizeViewer{ID: secondViewer, Width: 100, Height: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordViewer(thirdSession, resizeViewer{ID: thirdViewer, Width: 110, Height: 31}); !errors.Is(err, errResizeViewerCapacity) {
		t.Fatalf("process capacity error = %v", err)
	}

	firstViewers := store.Viewers(testSessionRef)
	secondViewers := store.Viewers(secondSession)
	if len(firstViewers) != 1 || firstViewers[0].ID != testViewerID || firstViewers[0].Width != 81 ||
		len(secondViewers) != 1 || secondViewers[0].ID != secondViewer || len(store.Viewers(thirdSession)) != 0 {
		t.Fatalf("capacity rejection evicted or replaced active viewers: %#v %#v", firstViewers, secondViewers)
	}
}

func TestResizeStoreRejectsUnreasonableViewerDimensions(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	for _, viewer := range []resizeViewer{
		{ID: testViewerID, Width: maxResizeViewerDimension + 1, Height: 24},
		{ID: testViewerID, Width: 80, Height: maxResizeViewerDimension + 1},
	} {
		if _, err := store.RecordViewer(testSessionRef, viewer); err == nil {
			t.Fatalf("unreasonable viewer dimensions were accepted: %#v", viewer)
		}
	}
}
