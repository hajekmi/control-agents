package tmux

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

func TestCaptureUsesBoundedPaneCapture(t *testing.T) {
	runner := &fakeRunner{
		capture: "one\nselected word\n\n",
	}
	client := NewClientWithRunner(runner)

	capture, err := client.Capture(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}

	if capture.Text != "one\nselected word" {
		t.Fatalf("text = %q", capture.Text)
	}
	wantCalls := [][]string{
		{"output", "tmux", "capture-pane", "-p", "-S", "-2000", "-E", "-1", "-t", "main"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestPasteUsesNamedTmuxBuffer(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	err := client.Paste(context.Background(), "main", PasteRequest{Text: "hello\nworld"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v, want 2 calls", runner.calls)
	}
	setCall := runner.calls[0]
	pasteCall := runner.calls[1]
	if len(setCall) != 7 || !reflect.DeepEqual(setCall[:4], []string{"run", "tmux", "set-buffer", "-b"}) || setCall[5] != "--" || setCall[6] != "hello\nworld" {
		t.Fatalf("set-buffer call = %#v", setCall)
	}
	buffer := setCall[4]
	if !strings.HasPrefix(buffer, "control-agents-paste-") {
		t.Fatalf("buffer = %q", buffer)
	}
	wantPaste := []string{"run", "tmux", "paste-buffer", "-d", "-b", buffer, "-t", "main"}
	if !reflect.DeepEqual(pasteCall, wantPaste) {
		t.Fatalf("paste call = %#v, want %#v", pasteCall, wantPaste)
	}
}

func TestPasteRejectsInvalidText(t *testing.T) {
	for _, text := range []string{"bad\x00text", strings.Repeat("x", MaxPasteBytes+1)} {
		runner := &fakeRunner{}
		client := NewClientWithRunner(runner)

		err := client.Paste(context.Background(), "main", PasteRequest{Text: text})
		if !errors.Is(err, ErrInvalidPaste) {
			t.Fatalf("err = %v, want ErrInvalidPaste", err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("calls = %#v, want none", runner.calls)
		}
	}
}

func TestListResizeClientsClassifiesWebClients(t *testing.T) {
	runner := &fakeRunner{
		resizeClients: "/dev/pts/ios|301|80|24|100|on|80|23\n/dev/pts/chrome|402|140|48|200|on|140|47\n/dev/pts/ssh|501|200|60|300|off|200|60\n",
	}
	client := NewClientWithRunner(runner)
	client.processDescendant = func(pid, ancestor int) bool {
		return (pid == 301 || pid == 402) && ancestor == 300
	}

	clients, err := client.ListResizeClients(context.Background(), "main", 300)
	if err != nil {
		t.Fatal(err)
	}

	want := []ResizeClient{
		{Name: "/dev/pts/ios", PID: 301, Width: 80, Height: 23, Activity: 100, StatusOn: true, Web: true},
		{Name: "/dev/pts/chrome", PID: 402, Width: 140, Height: 47, Activity: 200, StatusOn: true, Web: true},
		{Name: "/dev/pts/ssh", PID: 501, Width: 200, Height: 60, Activity: 300, Web: false},
	}
	if !reflect.DeepEqual(clients, want) {
		t.Fatalf("clients = %#v, want %#v", clients, want)
	}
}

func TestPrimaryResizeClientSelectsLatestNonWebClient(t *testing.T) {
	runner := &fakeRunner{
		resizeClients: "/dev/pts/ios|301|80|24|100|on|80|23\n/dev/pts/chrome|402|140|48|400|on|140|47\n/dev/pts/ssh-old|501|200|60|300|off|200|60\n/dev/pts/ssh-new|502|160|50|500|off|160|50\n",
	}
	client := NewClientWithRunner(runner)
	client.processDescendant = func(pid, ancestor int) bool {
		return (pid == 301 || pid == 402) && ancestor == 300
	}

	primary, err := client.PrimaryResizeClient(context.Background(), "main", 300)
	if err != nil {
		t.Fatal(err)
	}

	want := ResizeClient{Name: "/dev/pts/ssh-new", PID: 502, Width: 160, Height: 50, Activity: 500, Web: false}
	if primary != want {
		t.Fatalf("primary = %#v, want %#v", primary, want)
	}
}

func TestPrimaryResizeClientFallsBackToWindowHeightForControlClient(t *testing.T) {
	runner := &fakeRunner{
		resizeClients: "/dev/console|2|82||500|on|82|21\n",
	}
	client := NewClientWithRunner(runner)

	primary, err := client.PrimaryResizeClient(context.Background(), "main", 300)
	if err != nil {
		t.Fatal(err)
	}

	want := ResizeClient{Name: "/dev/console", PID: 2, Width: 82, Height: 21, Activity: 500, StatusOn: true, Web: false}
	if primary != want {
		t.Fatalf("primary = %#v, want %#v", primary, want)
	}
}

func TestResizeManualSetsManualModeAndDimensions(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	state, err := client.ResizeManual(context.Background(), "main", 140, 47)
	if err != nil {
		t.Fatal(err)
	}

	wantState := ResizeState{Mode: "manual", Width: 140, Height: 47}
	if state != wantState {
		t.Fatalf("state = %#v, want %#v", state, wantState)
	}
	wantCalls := [][]string{
		{"run", "tmux", "set-option", "-w", "-t", "main:", "window-size", "manual"},
		{"run", "tmux", "resize-window", "-t", "main:", "-x", "140", "-y", "47"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestResizeSmallestSetsSmallestMode(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	state, err := client.ResizeSmallest(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}

	wantState := ResizeState{Mode: "smallest"}
	if state != wantState {
		t.Fatalf("state = %#v, want %#v", state, wantState)
	}
	wantCalls := [][]string{
		{"run", "tmux", "set-option", "-w", "-t", "main:", "window-size", "smallest"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

type fakeRunner struct {
	status        string
	statuses      []string
	clients       string
	resizeClients string
	windows       string
	capture       string
	calls         [][]string
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "list-clients" {
		if strings.Contains(args[len(args)-1], "#{client_width}") {
			if f.resizeClients == "" {
				return nil, errors.New("no resize clients")
			}
			f.calls = append(f.calls, append([]string{"output", name}, args...))
			return []byte(f.resizeClients), nil
		}
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
	if len(args) > 0 && args[0] == "capture-pane" {
		f.calls = append(f.calls, append([]string{"output", name}, args...))
		return []byte(f.capture), nil
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
