package server

import (
	"errors"
	"testing"
	"time"
)

func TestPasteTokenIsSingleUseBoundAndExpires(t *testing.T) {
	manager := newPasteTokenManager(time.Minute, 4)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	binding := testPasteTokenBinding("safe\n")
	token, expires, err := manager.Create(binding)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(now.Add(time.Minute)) || !pasteTokenIDPattern.MatchString(token) {
		t.Fatalf("token/expiry = %q/%v", token, expires)
	}
	if err := manager.Consume(token, binding); err != nil {
		t.Fatal(err)
	}
	if err := manager.Consume(token, binding); !errors.Is(err, errPasteTokenInvalid) {
		t.Fatalf("replayed token error = %v", err)
	}

	token, _, err = manager.Create(binding)
	if err != nil {
		t.Fatal(err)
	}
	wrong := binding
	wrong.User = "other-login"
	if err := manager.Consume(token, wrong); !errors.Is(err, errPasteTokenInvalid) {
		t.Fatalf("cross-login token error = %v", err)
	}
	if err := manager.Consume(token, binding); !errors.Is(err, errPasteTokenInvalid) {
		t.Fatal("binding mismatch did not consume token")
	}

	token, _, err = manager.Create(binding)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := manager.Consume(token, binding); !errors.Is(err, errPasteTokenInvalid) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestPasteActionCountsUTF8LinesControlsAndTrailingNewline(t *testing.T) {
	action := pasteTextAction("é\r\nsecond\tline\r")
	if action.Bytes != 16 || action.Lines != 3 || !action.ControlCharacters || !action.TrailingNewline || !pasteDigestPattern.MatchString(action.Digest) {
		t.Fatalf("paste action = %#v", action)
	}
}

func TestPasteTokenRejectsImpossibleActionMetadata(t *testing.T) {
	manager := newPasteTokenManager(time.Minute, 4)
	for _, change := range []func(*pasteTokenBinding){
		func(binding *pasteTokenBinding) { binding.Action.Digest = "invalid" },
		func(binding *pasteTokenBinding) { binding.Action.Lines = binding.Action.Bytes + 2 },
		func(binding *pasteTokenBinding) { binding.Action.ControlCharacters = false },
		func(binding *pasteTokenBinding) { binding.Action.Lines = 1 },
	} {
		binding := testPasteTokenBinding("safe\n")
		change(&binding)
		if _, _, err := manager.Create(binding); !errors.Is(err, errPasteTokenInvalid) {
			t.Fatalf("impossible action was accepted: %#v", binding.Action)
		}
	}
}

func testPasteTokenBinding(text string) pasteTokenBinding {
	return pasteTokenBinding{
		User: "login", SessionRef: testSessionRef, PaneRef: PaneRef("p_abcdefghijklmnopqrstuvwx"),
		Generation: PaneGeneration{TmuxServerStart: "100", TmuxServerPID: 101, PaneID: "%42"},
		Action:     pasteTextAction(text),
	}
}
