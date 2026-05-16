package tmux

import (
	"context"
	"errors"
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
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "copy-mode", "-t", "main"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "history-bottom"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "cancel"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestStatusIncludesClientWindowOverflow(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/ssh|101|90|95|5|off\n/dev/pts/mobile|102|24|95|71|off\n",
	}
	client := NewClientWithRunner(runner)

	state, err := client.Status(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}

	if state.PaneHeight != 24 {
		t.Fatalf("pane height = %d, want visible client height 24", state.PaneHeight)
	}
	if state.WindowOverflow != 71 || state.WindowOffsetY != 71 {
		t.Fatalf("window overflow/offset = %d/%d, want 71/71", state.WindowOverflow, state.WindowOffsetY)
	}
	if state.ScrollTop != 91 || state.ScrollMax != 91 {
		t.Fatalf("scroll top/max = %d/%d, want 91/91", state.ScrollTop, state.ScrollMax)
	}
}

func TestStatusForProcessPrefersMatchingClientOverSmallestClient(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/ssh|101|24|95|71|off\n/dev/pts/web|202|90|95|5|off\n",
	}
	client := NewClientWithRunner(runner)
	client.processDescendant = func(pid, ancestor int) bool {
		return pid == 202 && ancestor == 200
	}

	state, err := client.StatusForProcess(context.Background(), "main", 200)
	if err != nil {
		t.Fatal(err)
	}

	if state.PaneHeight != 90 {
		t.Fatalf("pane height = %d, want matching web client height 90", state.PaneHeight)
	}
	if state.WindowOverflow != 5 || state.WindowOffsetY != 5 {
		t.Fatalf("window overflow/offset = %d/%d, want matching web client 5/5", state.WindowOverflow, state.WindowOffsetY)
	}
	if state.ScrollTop != 25 || state.ScrollMax != 25 {
		t.Fatalf("scroll top/max = %d/%d, want 25/25", state.ScrollTop, state.ScrollMax)
	}
}

func TestScrollForProcessBottomPansMatchingClientAfterCopyMode(t *testing.T) {
	runner := &fakeRunner{
		statuses: []string{
			"1|10|20|95\n",
			"0||20|95\n",
			"0||20|95\n",
		},
		clients: "/dev/pts/ssh|101|24|95|71|off\n/dev/pts/web|202|90|95|0|off\n",
	}
	client := NewClientWithRunner(runner)
	client.processDescendant = func(pid, ancestor int) bool {
		return pid == 202 && ancestor == 200
	}

	_, err := client.ScrollForProcess(context.Background(), "main", 200, ScrollRequest{Action: "bottom"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
		{"run", "tmux", "copy-mode", "-t", "main"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "history-bottom"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "cancel"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
		{"run", "tmux", "refresh-client", "-t", "/dev/pts/web", "-D", "5"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestStatusLineReducesVisibleClientHeight(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/mobile|102|40|95|56|on\n",
	}
	client := NewClientWithRunner(runner)

	state, err := client.Status(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}

	if state.PaneHeight != 39 {
		t.Fatalf("pane height = %d, want visible client height 39", state.PaneHeight)
	}
	if state.WindowOverflow != 56 || state.WindowOffsetY != 56 {
		t.Fatalf("window overflow/offset = %d/%d, want 56/56", state.WindowOverflow, state.WindowOffsetY)
	}
	if state.ScrollTop != 76 || state.ScrollMax != 76 {
		t.Fatalf("scroll top/max = %d/%d, want 76/76", state.ScrollTop, state.ScrollMax)
	}
}

func TestLineUpPansClientWindowBeforeHistory(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/mobile|102|24|95|10|off\n",
	}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "line-up", Amount: 3})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
		{"run", "tmux", "refresh-client", "-t", "/dev/pts/mobile", "-U", "3"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestScrollSetCanPanWithinLiveWindow(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/mobile|102|24|95|71|off\n",
	}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "set", Value: 30})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
		{"run", "tmux", "refresh-client", "-t", "/dev/pts/mobile", "-U", "61"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestScrollSetBottomAccountsForStatusLine(t *testing.T) {
	runner := &fakeRunner{
		status:  "0||20|95\n",
		clients: "/dev/pts/mobile|102|40|95|55|on\n",
	}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "set", Value: 76})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
		{"run", "tmux", "refresh-client", "-t", "/dev/pts/mobile", "-D", "1"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestLineDownCancelsCopyModeAtHistoryBottom(t *testing.T) {
	runner := &fakeRunner{
		statuses: []string{
			"1|1|20|24\n",
			"1|0|20|24\n",
			"0||20|24\n",
		},
	}
	client := NewClientWithRunner(runner)

	_, err := client.Scroll(context.Background(), "main", ScrollRequest{Action: "line-down"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "scroll-down"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "cancel"},
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestSendKeyUsesSupportedTmuxKeyName(t *testing.T) {
	runner := &fakeRunner{status: "0||100|24\n"}
	client := NewClientWithRunner(runner)

	err := client.SendKey(context.Background(), "main", KeyRequest{Key: "ctrl-c"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "send-keys", "-t", "main", "C-c"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestSendKeyCancelsCopyModeFirst(t *testing.T) {
	runner := &fakeRunner{status: "1|20|100|24\n"}
	client := NewClientWithRunner(runner)

	err := client.SendKey(context.Background(), "main", KeyRequest{Key: "escape"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "main", "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}"},
		{"run", "tmux", "send-keys", "-t", "main", "-X", "cancel"},
		{"run", "tmux", "send-keys", "-t", "main", "Escape"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestSendKeyRejectsUnsupportedKey(t *testing.T) {
	runner := &fakeRunner{status: "0||100|24\n"}
	client := NewClientWithRunner(runner)

	err := client.SendKey(context.Background(), "main", KeyRequest{Key: "ctrl-alt-delete"})
	if err == nil {
		t.Fatal("expected unsupported key error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want none", runner.calls)
	}
}

func TestWindowsParsesListWindows(t *testing.T) {
	runner := &fakeRunner{
		windows: "0\x1fshell\x1f1\x1f2\n1\x1feditor\x1f0\x1f1\n",
	}
	client := NewClientWithRunner(runner)

	windows, err := client.Windows(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}

	want := []Window{
		{Index: 0, Name: "shell", Active: true, Panes: 2},
		{Index: 1, Name: "editor", Active: false, Panes: 1},
	}
	if !reflect.DeepEqual(windows, want) {
		t.Fatalf("windows = %#v, want %#v", windows, want)
	}
}

func TestControlNewWindowUsesPaneCurrentPathAndReturnsWindows(t *testing.T) {
	runner := &fakeRunner{
		windows: "0\x1fshell\x1f1\x1f1\n1\x1fnew\x1f0\x1f1\n",
	}
	client := NewClientWithRunner(runner)

	windows, err := client.Control(context.Background(), "main", ControlRequest{Action: "new-window"})
	if err != nil {
		t.Fatal(err)
	}

	wantCalls := [][]string{
		{"run", "tmux", "new-window", "-t", "main:", "-c", "#{pane_current_path}"},
		{"output", "tmux", "list-windows", "-t", "main", "-F", windowListFormat()},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
	if len(windows) != 2 {
		t.Fatalf("len(windows) = %d, want 2", len(windows))
	}
}

func TestControlSelectWindowRequiresIndex(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	_, err := client.Control(context.Background(), "main", ControlRequest{Action: "select-window"})
	if !errors.Is(err, ErrInvalidControlRequest) {
		t.Fatalf("err = %v, want ErrInvalidControlRequest", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want none", runner.calls)
	}
}

func TestControlRejectsUnsupportedAction(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	_, err := client.Control(context.Background(), "main", ControlRequest{Action: "attach-session"})
	if !errors.Is(err, ErrUnsupportedControlAction) {
		t.Fatalf("err = %v, want ErrUnsupportedControlAction", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want none", runner.calls)
	}
}

type fakeRunner struct {
	status   string
	statuses []string
	clients  string
	windows  string
	calls    [][]string
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "list-clients" {
		if f.clients == "" {
			return nil, errors.New("no clients")
		}
		f.calls = append(f.calls, append([]string{"output", name}, args...))
		return []byte(f.clients), nil
	}
	if len(args) > 0 && args[0] == "list-windows" {
		if f.windows == "" {
			return nil, errors.New("no windows")
		}
		f.calls = append(f.calls, append([]string{"output", name}, args...))
		return []byte(f.windows), nil
	}
	f.calls = append(f.calls, append([]string{"output", name}, args...))
	if len(f.statuses) > 0 {
		status := f.statuses[0]
		f.statuses = f.statuses[1:]
		return []byte(status), nil
	}
	return []byte(f.status), nil
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{"run", name}, args...))
	return nil
}
