package tmux

import (
	"context"
	"reflect"
	"testing"
)

func TestParseScrollStateAtBottom(t *testing.T) {
	state, err := parseScrollState("0||57|24\n")
	if err != nil {
		t.Fatal(err)
	}
	if state.Position != 0 || state.ScrollTop != 57 || state.ScrollMax != 57 || state.InCopyMode {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestParseScrollStateInCopyMode(t *testing.T) {
	state, err := parseScrollState("1|22|57|24\n")
	if err != nil {
		t.Fatal(err)
	}
	if state.Position != 22 || state.ScrollTop != 35 || state.ScrollMax != 57 || !state.InCopyMode {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestScrollSetUsesHistoryTopThenScrollDown(t *testing.T) {
	runner := &fakeRunner{status: "0||100|24\n"}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "set", Value: 40})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "copy-mode", "-t", "main"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "history-top"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "-N", "40", "scroll-down"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestScrollBottomCancelsCopyMode(t *testing.T) {
	runner := &fakeRunner{status: "1|20|100|24\n"}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "bottom"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"run", "tmux", "copy-mode", "-t", "main"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "history-bottom"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "cancel"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

type fakeRunner struct {
	status string
	calls  [][]string
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{"output", name}, args...))
	return []byte(f.status), nil
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{"run", name}, args...))
	return nil
}
