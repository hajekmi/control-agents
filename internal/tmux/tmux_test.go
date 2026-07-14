package tmux

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSendKeyUsesSupportedTmuxKeyName(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	err := client.SendKey(context.Background(), "main", KeyRequest{Key: "ctrl-c"})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{{"run", "tmux", "send-keys", "-t", "main", "C-c"}}
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

func TestSendTextUsesOneLiteralArgument(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)
	if err := client.SendText(context.Background(), "%42", ";"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"run", "tmux", "send-keys", "-l", "-t", "%42", "--", ";"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	if err := client.SendText(context.Background(), "%42", "ab"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid input error = %v", err)
	}
	if err := client.SendText(context.Background(), "%42", "\u009b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("control input error = %v", err)
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

func TestControlNewWindowUsesExactResolvedWindowIDAndPaneCurrentPath(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	if err := client.Control(context.Background(), "%42", "@7", ControlRequest{Action: "new-window"}); err != nil {
		t.Fatal(err)
	}

	wantCalls := [][]string{
		{"run", "tmux", "new-window", "-a", "-t", "@7", "-c", "#{pane_current_path}"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestControlSelectWindowRequiresRawWindowID(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	err := client.Control(context.Background(), "%42", "1; run-shell touch /tmp/pwned", ControlRequest{Action: "select-window"})
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

	err := client.Control(context.Background(), "%42", "@7", ControlRequest{Action: "attach-session"})
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
		statuses: []string{
			"120\x1f40\x1f100\x1f50000\x1f2048\x1f0\n",
			"120\x1f40\x1f101\x1f50000\x1f2060\x1f0\n",
		},
	}
	client := NewClientWithRunner(runner)
	outputEpochs := []int64{1000, 1001}

	capture, err := client.CaptureHistory(context.Background(), "%42", DefaultSnapshotBytes, func() int64 {
		epoch := outputEpochs[0]
		outputEpochs = outputEpochs[1:]
		return epoch
	})
	if err != nil {
		t.Fatal(err)
	}

	if capture.Text != "one\nselected word\n" {
		t.Fatalf("text = %q", capture.Text)
	}
	if capture.Before.Columns != 120 || capture.After.HistorySize != 101 || capture.Before.OutputEpoch != 1000 || capture.After.OutputEpoch != 1001 {
		t.Fatalf("metadata = %#v/%#v", capture.Before, capture.After)
	}
	metadataFormat := "#{pane_width}\x1f#{pane_height}\x1f#{history_size}\x1f#{history_limit}\x1f#{history_bytes}\x1f#{alternate_on}"
	wantCalls := [][]string{
		{"output", "tmux", "display-message", "-p", "-t", "%42", metadataFormat},
		{"output", "tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", "%42"},
		{"output", "tmux", "display-message", "-p", "-t", "%42", metadataFormat},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestTopologyRejectsMalformedServerIncarnation(t *testing.T) {
	for _, output := range []string{
		"100\n",
		"100\x1f0\n",
		"100\x1fnot-a-pid\n",
		"not-a-time\x1f101\n",
		"100\x1f101\x1fextra\n",
	} {
		t.Run(strconv.Quote(output), func(t *testing.T) {
			runner := &fakeRunner{status: output}
			client := NewClientWithRunner(runner)

			if _, err := client.Topology(context.Background(), "alpha"); err == nil {
				t.Fatal("malformed tmux server incarnation was accepted")
			}
			if len(runner.calls) != 1 || runner.calls[0][len(runner.calls[0])-1] != "#{start_time}\x1f#{pid}" {
				t.Fatalf("calls = %#v, want rejection before topology lookup", runner.calls)
			}
		})
	}
}

func TestVerifyPaneGenerationRejectsChangedServerPIDWithSameStartAndPane(t *testing.T) {
	runner := &fakeRunner{status: "100\x1f202\x1f%42\n"}
	client := NewClientWithRunner(runner)

	err := client.VerifyPaneGeneration(context.Background(), "%42", PaneGeneration{
		ServerStart: "100",
		ServerPID:   101,
		PaneID:      "%42",
	})
	if !errors.Is(err, ErrPaneGenerationChanged) {
		t.Fatalf("error = %v, want changed pane generation", err)
	}
	want := [][]string{{"output", "tmux", "display-message", "-p", "-t", "%42", "#{start_time}\x1f#{pid}\x1f#{pane_id}"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestPasteUsesNamedTmuxBuffer(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	err := client.Paste(context.Background(), "main", PasteRequest{Text: "hello\nworld"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %#v, want 3 calls", runner.calls)
	}
	loadCall := runner.calls[0]
	pasteCall := runner.calls[1]
	if len(loadCall) != 6 || !reflect.DeepEqual(loadCall[:4], []string{"input", "tmux", "load-buffer", "-b"}) || loadCall[5] != "-" {
		t.Fatalf("load-buffer call = %#v", loadCall)
	}
	buffer := loadCall[4]
	if !strings.HasPrefix(buffer, "control-agents-paste-") {
		t.Fatalf("buffer = %q", buffer)
	}
	if runner.input != "hello\nworld" {
		t.Fatalf("stdin = %q", runner.input)
	}
	wantPaste := []string{"run", "tmux", "paste-buffer", "-p", "-r", "-b", buffer, "-t", "main"}
	if !reflect.DeepEqual(pasteCall, wantPaste) {
		t.Fatalf("paste call = %#v, want %#v", pasteCall, wantPaste)
	}
	wantDelete := []string{"run", "tmux", "delete-buffer", "-b", buffer}
	if !reflect.DeepEqual(runner.calls[2], wantDelete) {
		t.Fatalf("delete call = %#v, want %#v", runner.calls[2], wantDelete)
	}
}

func TestPasteRejectsInvalidText(t *testing.T) {
	for _, text := range []string{"bad\x00text", string([]byte{0xff}), strings.Repeat("x", MaxPasteBytes+1)} {
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

func TestPasteDeletesCreatedBufferOnPasteFailure(t *testing.T) {
	want := errors.New("paste failed")
	runner := &fakeRunner{runErrors: map[string]error{"paste-buffer": want}}
	client := NewClientWithRunner(runner)
	err := client.Paste(context.Background(), "%42", PasteRequest{Text: "safe"})
	if !errors.Is(err, want) {
		t.Fatalf("paste error = %v", err)
	}
	if len(runner.calls) != 3 || runner.calls[2][2] != "delete-buffer" || runner.calls[2][4] != runner.calls[0][4] {
		t.Fatalf("failure cleanup calls = %#v", runner.calls)
	}
}

func TestPasteAttemptsCleanupWhenLoadReportsFailure(t *testing.T) {
	want := errors.New("load failed")
	runner := &fakeRunner{inputError: want}
	client := NewClientWithRunner(runner)
	err := client.Paste(context.Background(), "%42", PasteRequest{Text: "safe"})
	if !errors.Is(err, want) {
		t.Fatalf("load error = %v", err)
	}
	if len(runner.calls) != 2 || runner.calls[0][2] != "load-buffer" || runner.calls[1][2] != "delete-buffer" || runner.calls[1][4] != runner.calls[0][4] {
		t.Fatalf("load failure cleanup calls = %#v", runner.calls)
	}
}

func TestPasteUsesUniqueRandomBufferNames(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)
	if err := client.Paste(context.Background(), "%42", PasteRequest{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	first := runner.calls[0][4]
	if err := client.Paste(context.Background(), "%42", PasteRequest{Text: "two"}); err != nil {
		t.Fatal(err)
	}
	second := runner.calls[3][4]
	if first == second {
		t.Fatalf("paste buffer identity reused: %q", first)
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
		{"run", "tmux", "set-option", "-w", "-t", "main", "window-size", "manual"},
		{"run", "tmux", "resize-window", "-t", "main", "-x", "140", "-y", "47"},
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
		{"run", "tmux", "set-option", "-w", "-t", "main", "window-size", "smallest"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestCreateManagedSessionUsesConfiguredHomeAndOptions(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	err := client.CreateManagedSession(context.Background(), "alpha", "/home/service", ManagedSessionOptions{
		WindowSize:  "manual",
		Mouse:       "off",
		StatusLeft:  "[alpha] ",
		SSHAuthSock: "/state/agent/forwarded.sock",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"run", "tmux", "start-server", ";", "new-session", "-d", "-s", "alpha", "-c", "/home/service", "-e", "SSH_AUTH_SOCK=/state/agent/forwarded.sock", "tmux", "wait-for", "control-agents-bootstrap-alpha", ";", "set-option", "-t", "alpha", "history-limit", "50000", ";", "set-hook", "-t", "alpha", "window-linked[900]", "set-option -w window-size manual", ";", "new-window", "-d", "-a", "-t", "alpha:", "-c", "/home/service", ";", "kill-window", "-t", "alpha:"},
		{"run", "tmux", "set-option", "-t", "alpha", "history-limit", "50000"},
		{"run", "tmux", "set-hook", "-t", "alpha", "window-linked[900]", "set-option -w window-size manual"},
		{"output", "tmux", "list-windows", "-t", "alpha", "-F", "#{window_id}"},
		{"run", "tmux", "set-option", "-w", "-t", "@1", "window-size", "manual"},
		{"run", "tmux", "set-option", "-t", "alpha", "destroy-unattached", "off"},
		{"run", "tmux", "set-option", "-t", "alpha", "mouse", "off"},
		{"run", "tmux", "set-option", "-t", "alpha", "status-left-length", "80"},
		{"run", "tmux", "set-option", "-t", "alpha", "status-left", "[alpha] "},
		{"run", "tmux", "set-option", "-t", "alpha", "status-right", "#{pane_current_path}"},
		{"run", "tmux", "set-environment", "-t", "alpha", "SSH_AUTH_SOCK", "/state/agent/forwarded.sock"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestConfigureHistoryScopesDefaultToExactManagedSession(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	if err := client.ConfigureHistory(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"run", "tmux", "set-option", "-t", "alpha", "history-limit", "50000"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want an exact session option with no global mutation: %#v", runner.calls, want)
	}
}

func TestConfigureManualWindowSizeUpdatesEveryExistingWindowWithoutResize(t *testing.T) {
	runner := &fakeRunner{windowIDs: "@4\n@9\n"}
	client := NewClientWithRunner(runner)

	if err := client.ConfigureManualWindowSize(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"run", "tmux", "set-hook", "-t", "alpha", "window-linked[900]", "set-option -w window-size manual"},
		{"output", "tmux", "list-windows", "-t", "alpha", "-F", "#{window_id}"},
		{"run", "tmux", "set-option", "-w", "-t", "@4", "window-size", "manual"},
		{"run", "tmux", "set-option", "-w", "-t", "@9", "window-size", "manual"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want manual option changes without resize-window: %#v", runner.calls, want)
	}
}

func TestPasteLoadsShellMetacharactersOnlyThroughStdin(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)
	text := " space \"quote\";\n$(touch /tmp/control-agents-pwned) `id` & | > < * ? "

	if err := client.Paste(context.Background(), "%42", PasteRequest{Text: text}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 || runner.input != text || runner.calls[1][len(runner.calls[1])-1] != "%42" {
		t.Fatalf("stdin/direct tmux commands = %#v input=%q", runner.calls, runner.input)
	}
	for _, call := range runner.calls {
		for _, argument := range call {
			if argument == text {
				t.Fatalf("paste content entered argv: %#v", runner.calls)
			}
		}
	}
}

func TestSetSessionEnvironmentDoesNotUseGlobalEnvironment(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	if err := client.SetSessionEnvironment(context.Background(), "alpha", "SSH_AUTH_SOCK", "/state/agent/forwarded.sock"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"run", "tmux", "set-environment", "-t", "alpha", "SSH_AUTH_SOCK", "/state/agent/forwarded.sock"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

type fakeRunner struct {
	status        string
	statuses      []string
	clients       string
	resizeClients string
	windows       string
	windowIDs     string
	capture       string
	input         string
	calls         [][]string
	runErrors     map[string]error
	inputError    error
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
		if len(args) > 0 && args[len(args)-1] == "#{window_id}" {
			f.calls = append(f.calls, append([]string{"output", name}, args...))
			if f.windowIDs == "" {
				return []byte("@1\n"), nil
			}
			return []byte(f.windowIDs), nil
		}
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
	if len(args) > 0 && f.runErrors != nil {
		return f.runErrors[args[0]]
	}
	return nil
}

func (f *fakeRunner) RunWithInput(ctx context.Context, input io.Reader, name string, args ...string) error {
	data, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	f.input = string(data)
	f.calls = append(f.calls, append([]string{"input", name}, args...))
	return f.inputError
}
