package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"control-agents/internal/registry"
	managedsession "control-agents/internal/session"
)

func TestParseOptionsModesAndEnvironmentPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    options
		wantErr string
	}{
		{name: "selector", want: options{attach: true}},
		{name: "direct", args: []string{"main"}, want: options{attach: true, name: "main"}},
		{name: "no attach flag", args: []string{"--no-attach", "main"}, want: options{name: "main"}},
		{name: "no attach environment", args: []string{"main"}, env: map[string]string{"CONTROL_AGENTS_NO_ATTACH": "1"}, want: options{name: "main"}},
		{name: "attach compatibility alias", args: []string{"main"}, env: map[string]string{"CONTROL_AGENTS_ATTACH": "0"}, want: options{name: "main"}},
		{name: "explicit attach wins", args: []string{"--attach", "main"}, env: map[string]string{"CONTROL_AGENTS_NO_ATTACH": "1", "CONTROL_AGENTS_ATTACH": "0"}, want: options{attach: true, name: "main"}},
		{name: "explicit no attach wins", args: []string{"--no-attach", "main"}, env: map[string]string{"CONTROL_AGENTS_ATTACH": "1"}, want: options{name: "main"}},
		{name: "help ignores environment", args: []string{"--help"}, env: map[string]string{"CONTROL_AGENTS_ATTACH": "invalid"}, want: options{help: true}},
		{name: "contradictory flags", args: []string{"--attach", "--no-attach", "main"}, wantErr: "cannot be used together"},
		{name: "noninteractive needs name", args: []string{"--no-attach"}, wantErr: "requires an explicit session name"},
		{name: "environment noninteractive needs name", env: map[string]string{"CONTROL_AGENTS_NO_ATTACH": "1"}, wantErr: "requires an explicit session name"},
		{name: "invalid environment", args: []string{"main"}, env: map[string]string{"CONTROL_AGENTS_ATTACH": "sometimes"}, wantErr: "must be 0 or 1"},
		{name: "too many names", args: []string{"one", "two"}, wantErr: "at most one"},
		{name: "unknown option", args: []string{"--other"}, wantErr: "unknown option"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOptions(test.args, mapLookup(test.env))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("options = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDirectNoAttachCreatesAndPrintsOnlyCanonicalID(t *testing.T) {
	lifecycle := &fakeLifecycle{created: managed("canonical")}
	var output, errorOut bytes.Buffer
	client := app{
		lifecycle: lifecycle,
		input:     strings.NewReader(""),
		output:    &output,
		errorOut:  &errorOut,
		attach: func(context.Context, string) error {
			t.Fatal("attach called in no-attach mode")
			return nil
		},
	}
	if code := client.run(context.Background(), options{name: "requested"}); code != exitSuccess {
		t.Fatalf("exit = %d, want %d; stderr: %s", code, exitSuccess, errorOut.String())
	}
	if got := output.String(); got != "canonical\n" {
		t.Fatalf("stdout = %q, want canonical ID only", got)
	}
	if got := lifecycle.createNames; len(got) != 1 || got[0] != "requested" {
		t.Fatalf("Create calls = %v, want [requested]", got)
	}
}

func TestForwardedAgentRefreshRunsBeforeDirectAndSelectorLifecycleUse(t *testing.T) {
	tests := []struct {
		name      string
		opts      options
		input     string
		configure func(*fakeLifecycle, func() bool)
	}{
		{
			name: "direct",
			opts: options{name: "main"},
			configure: func(lifecycle *fakeLifecycle, refreshed func() bool) {
				lifecycle.beforeCreate = func() {
					if !refreshed() {
						t.Error("Create ran before forwarded agent refresh")
					}
				}
			},
		},
		{
			name:  "selector",
			opts:  options{attach: true},
			input: "q\n",
			configure: func(lifecycle *fakeLifecycle, refreshed func() bool) {
				lifecycle.beforeList = func() {
					if !refreshed() {
						t.Error("List ran before forwarded agent refresh")
					}
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			refreshed := false
			lifecycle := &fakeLifecycle{created: managed("main")}
			test.configure(lifecycle, func() bool { return refreshed })
			var errorOut bytes.Buffer
			client := app{
				lifecycle: lifecycle,
				input:     strings.NewReader(test.input),
				output:    io.Discard,
				errorOut:  &errorOut,
				attach:    func(context.Context, string) error { return nil },
				refreshAgent: func() (managedsession.ForwardedAgentStatus, error) {
					refreshed = true
					return managedsession.ForwardedAgentAvailable, nil
				},
			}
			if code := client.run(context.Background(), test.opts); code != exitSuccess {
				t.Fatalf("exit = %d, stderr = %q", code, errorOut.String())
			}
			if !strings.Contains(errorOut.String(), "forwarded SSH agent: available") {
				t.Fatalf("missing agent status: %q", errorOut.String())
			}
		})
	}
}

func TestForwardedAgentStatusesAreConciseAndNonBlocking(t *testing.T) {
	tests := []struct {
		name   string
		status managedsession.ForwardedAgentStatus
		err    error
		want   string
	}{
		{name: "available", status: managedsession.ForwardedAgentAvailable, want: "available"},
		{name: "unavailable", status: managedsession.ForwardedAgentUnavailable, want: "unavailable"},
		{name: "invalid", status: managedsession.ForwardedAgentInvalid, want: "invalid"},
		{name: "refresh error", err: errors.New("sensitive /tmp/forwarded.sock"), want: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{created: managed("main")}
			var output, errorOut bytes.Buffer
			client := app{
				lifecycle:    lifecycle,
				input:        strings.NewReader(""),
				output:       &output,
				errorOut:     &errorOut,
				attach:       func(context.Context, string) error { return nil },
				refreshAgent: func() (managedsession.ForwardedAgentStatus, error) { return test.status, test.err },
			}
			if code := client.run(context.Background(), options{name: "main"}); code != exitSuccess {
				t.Fatalf("exit = %d", code)
			}
			if output.String() != "main\n" {
				t.Fatalf("stdout = %q, want canonical ID only", output.String())
			}
			if !strings.Contains(errorOut.String(), "forwarded SSH agent: "+test.want) {
				t.Fatalf("stderr = %q, want status %q", errorOut.String(), test.want)
			}
			if strings.Contains(errorOut.String(), "/tmp/forwarded.sock") {
				t.Fatalf("status exposed transient agent path: %q", errorOut.String())
			}
		})
	}
}

func TestDirectNamedAttachExitsAfterDetach(t *testing.T) {
	lifecycle := &fakeLifecycle{created: managed("main")}
	var output bytes.Buffer
	var attached []string
	client := app{
		lifecycle: lifecycle,
		input:     strings.NewReader(""),
		output:    &output,
		errorOut:  io.Discard,
		attach: func(_ context.Context, name string) error {
			attached = append(attached, name)
			return nil
		},
	}
	if code := client.run(context.Background(), options{attach: true, name: "main"}); code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if len(lifecycle.listCalls) != 0 {
		t.Fatalf("direct mode unexpectedly opened selector with %d List calls", len(lifecycle.listCalls))
	}
	if len(attached) != 1 || attached[0] != "tmux-main" {
		t.Fatalf("attached targets = %v, want [tmux-main]", attached)
	}
	if lifecycle.ensureCalls != 1 {
		t.Fatalf("EnsureBridge calls = %d, want 1 after detach", lifecycle.ensureCalls)
	}
}

func TestTmuxAttachPreservesManagedSessionEnvironment(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("PATH", "/operator/bin:/usr/bin:/bin")
	managedTmux := "/managed/bin/tmux"
	command := tmuxAttachCommand(context.Background(), managedTmux, "tmux-main")
	want := []string{managedTmux, "attach-session", "-E", "-t", "tmux-main"}
	if got := command.Args; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("attach command = %v, want %v", got, want)
	}
	if command.Path != managedTmux {
		t.Fatalf("attach executable = %q, want %q", command.Path, managedTmux)
	}
	for _, name := range []string{"LANG", "LC_ALL"} {
		if got := commandEnvironmentValue(command.Env, name); got != "C.UTF-8" {
			t.Fatalf("attach %s = %q, want C.UTF-8", name, got)
		}
	}
}

func commandEnvironmentValue(environment []string, name string) string {
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		if key == name {
			return value
		}
	}
	return ""
}

func TestSelectorRefreshesAfterAttachAndQuitsWithoutTermination(t *testing.T) {
	lifecycle := &fakeLifecycle{lists: [][]registry.Session{{managed("backend"), managed("main")}, {managed("backend"), managed("main")}}}
	var output, errorOut bytes.Buffer
	var attached []string
	client := app{
		lifecycle: lifecycle,
		input:     strings.NewReader("2\nq\n"),
		output:    &output,
		errorOut:  &errorOut,
		attach: func(_ context.Context, name string) error {
			attached = append(attached, name)
			return nil
		},
	}
	if code := client.run(context.Background(), options{attach: true}); code != exitSuccess {
		t.Fatalf("exit = %d, want %d; stderr: %s", code, exitSuccess, errorOut.String())
	}
	if lifecycle.listCount != 2 {
		t.Fatalf("List calls = %d, want refresh before both prompts", lifecycle.listCount)
	}
	if len(attached) != 1 || attached[0] != "tmux-main" {
		t.Fatalf("attached targets = %v, want [tmux-main]", attached)
	}
	if lifecycle.createNames != nil {
		t.Fatalf("selecting existing session called Create: %v", lifecycle.createNames)
	}
	if lifecycle.terminateCalls != 0 {
		t.Fatalf("Terminate calls = %d, want 0", lifecycle.terminateCalls)
	}
	if count := strings.Count(output.String(), "Control Agents sessions"); count != 2 {
		t.Fatalf("selector render count = %d, want 2; output: %q", count, output.String())
	}
	if !strings.Contains(output.String(), "Detach with Ctrl-b d") {
		t.Fatalf("missing detach hint: %q", output.String())
	}
}

func TestSelectorNewSessionValidationAndImmediateAttach(t *testing.T) {
	lifecycle := &fakeLifecycle{
		lists:   [][]registry.Session{nil, nil, {managed("new-session")}},
		created: managed("new-session"),
		createErrors: map[string]error{
			"bad name": managedsession.ErrInvalidName,
		},
	}
	var output, errorOut bytes.Buffer
	var attached []string
	client := app{
		lifecycle: lifecycle,
		input:     strings.NewReader("n\nbad name\nn\nnew-session\nq\n"),
		output:    &output,
		errorOut:  &errorOut,
		attach: func(_ context.Context, name string) error {
			attached = append(attached, name)
			return nil
		},
	}
	if code := client.run(context.Background(), options{attach: true}); code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if got := lifecycle.createNames; len(got) != 2 || got[0] != "bad name" || got[1] != "new-session" {
		t.Fatalf("Create calls = %v", got)
	}
	if len(attached) != 1 || attached[0] != "tmux-new-session" {
		t.Fatalf("attached targets = %v", attached)
	}
	if !strings.Contains(errorOut.String(), "invalid_name") {
		t.Fatalf("missing validation error: %q", errorOut.String())
	}
}

func TestSelectorReportsSessionTerminationAndRemainsUsable(t *testing.T) {
	lifecycle := &fakeLifecycle{
		lists:        [][]registry.Session{{managed("main")}, nil},
		ensureErrors: map[string]error{"main": managedsession.ErrNotFound},
	}
	var errorOut bytes.Buffer
	client := app{
		lifecycle: lifecycle,
		input:     strings.NewReader("1\nq\n"),
		output:    io.Discard,
		errorOut:  &errorOut,
		attach:    func(context.Context, string) error { return nil },
	}
	if code := client.run(context.Background(), options{attach: true}); code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if !strings.Contains(errorOut.String(), "ended or is unavailable") {
		t.Fatalf("missing termination message: %q", errorOut.String())
	}
	if lifecycle.listCount != 2 {
		t.Fatalf("List calls = %d, want selector to remain usable", lifecycle.listCount)
	}
}

func TestSelectorEOFAndInterruptNeverTerminateSessions(t *testing.T) {
	tests := []struct {
		name      string
		input     io.Reader
		interrupt <-chan os.Signal
	}{
		{name: "EOF", input: strings.NewReader("")},
		{name: "interrupt", input: blockingReader{}, interrupt: signalChannel(os.Interrupt)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{lists: [][]registry.Session{{managed("main")}}}
			client := app{
				lifecycle: lifecycle,
				input:     test.input,
				output:    io.Discard,
				errorOut:  io.Discard,
				interrupt: test.interrupt,
				attach:    func(context.Context, string) error { return nil },
			}
			if code := client.run(context.Background(), options{attach: true}); code != exitSuccess {
				t.Fatalf("exit = %d, want %d", code, exitSuccess)
			}
			if lifecycle.terminateCalls != 0 {
				t.Fatalf("Terminate calls = %d, want 0", lifecycle.terminateCalls)
			}
		})
	}
}

func TestLifecycleExitCodes(t *testing.T) {
	if got := lifecycleExitCode(managedsession.ErrInvalidName); got != exitUsage {
		t.Fatalf("invalid name exit = %d, want %d", got, exitUsage)
	}
	if got := lifecycleExitCode(managedsession.ErrConflict); got != exitConflict {
		t.Fatalf("conflict exit = %d, want %d", got, exitConflict)
	}
	if got := lifecycleExitCode(errors.New("dependency")); got != exitFailure {
		t.Fatalf("dependency exit = %d, want %d", got, exitFailure)
	}
}

type fakeLifecycle struct {
	mu             sync.Mutex
	lists          [][]registry.Session
	created        registry.Session
	createErrors   map[string]error
	ensureErrors   map[string]error
	createNames    []string
	listCalls      []struct{}
	listCount      int
	ensureCalls    int
	terminateCalls int
	beforeCreate   func()
	beforeList     func()
}

func (f *fakeLifecycle) List(context.Context) ([]registry.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeList != nil {
		f.beforeList()
	}
	f.listCount++
	f.listCalls = append(f.listCalls, struct{}{})
	if len(f.lists) == 0 {
		return nil, nil
	}
	index := f.listCount - 1
	if index >= len(f.lists) {
		index = len(f.lists) - 1
	}
	return f.lists[index], nil
}

func (f *fakeLifecycle) Create(_ context.Context, name string) (registry.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeCreate != nil {
		f.beforeCreate()
	}
	f.createNames = append(f.createNames, name)
	if err := f.createErrors[name]; err != nil {
		return registry.Session{}, err
	}
	return f.created, nil
}

func (f *fakeLifecycle) EnsureBridge(_ context.Context, name string) (registry.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCalls++
	if err := f.ensureErrors[name]; err != nil {
		return registry.Session{}, err
	}
	return managed(name), nil
}

func (f *fakeLifecycle) Reconcile(ctx context.Context) ([]registry.Session, error) {
	return f.List(ctx)
}

func (f *fakeLifecycle) Terminate(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminateCalls++
	return nil
}

func managed(name string) registry.Session {
	return registry.Session{ID: name, Name: name, TmuxName: "tmux-" + name}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func signalChannel(signal os.Signal) <-chan os.Signal {
	interrupts := make(chan os.Signal, 1)
	interrupts <- signal
	return interrupts
}

type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	select {}
}
