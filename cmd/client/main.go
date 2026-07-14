package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"control-agents/internal/registry"
	managedsession "control-agents/internal/session"
	"control-agents/internal/tmux"
	"control-agents/internal/version"
)

const (
	exitSuccess  = 0
	exitFailure  = 1
	exitUsage    = 2
	exitConflict = 3
)

var errInterrupted = errors.New("interrupted")

type options struct {
	attach  bool
	name    string
	help    bool
	version bool
}

type app struct {
	lifecycle    managedsession.Lifecycle
	input        io.Reader
	output       io.Writer
	errorOut     io.Writer
	interrupt    <-chan os.Signal
	attach       func(context.Context, string) error
	refreshAgent func() (managedsession.ForwardedAgentStatus, error)
}

type lineResult struct {
	line string
	err  error
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.LookupEnv, isTerminal))
}

func runMain(args []string, stdin *os.File, stdout, stderr io.Writer, lookupEnv func(string) (string, bool), terminal func(*os.File) bool) int {
	opts, err := parseOptions(args, lookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "control-agents: %v\n\n%s", err, usageText)
		return exitUsage
	}
	if opts.help {
		fmt.Fprint(stdout, usageText)
		return exitSuccess
	}
	if opts.version {
		fmt.Fprintf(stdout, "control-agents %s\n", version.String())
		return exitSuccess
	}
	if opts.name == "" && (!terminal(stdin) || !writerIsTerminal(stdout, terminal)) {
		fmt.Fprintln(stderr, "control-agents: the session selector requires an interactive terminal; provide NAME with --no-attach for scripts")
		return exitUsage
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "control-agents: determine user home directory: %v\n", err)
		return exitFailure
	}
	stateDir := filepath.Join(homeDir, ".local", "state", "control-agents")
	if value, ok := lookupEnv("CONTROL_AGENTS_STATE_DIR"); ok && value != "" {
		stateDir = value
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	lifecycleConfig, err := managedsession.ConfigFromEnvironment(stateDir, homeDir, logger)
	if err != nil {
		fmt.Fprintf(stderr, "control-agents: configuration error: %v\n", err)
		return exitUsage
	}
	lifecycle, err := managedsession.New(lifecycleConfig)
	if err != nil {
		fmt.Fprintf(stderr, "control-agents: initialize session lifecycle: %v\n", err)
		return exitFailure
	}
	forwardedAgent, forwardedAgentErr := managedsession.NewForwardedAgent(stateDir)

	interrupts := make(chan os.Signal, 8)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	client := app{
		lifecycle: lifecycle,
		input:     stdin,
		output:    stdout,
		errorOut:  stderr,
		interrupt: interrupts,
		attach: func(ctx context.Context, tmuxName string) error {
			command := tmuxAttachCommand(ctx, lifecycleConfig.TmuxBinary, tmuxName)
			command.Stdin = stdin
			command.Stdout = stdout
			command.Stderr = stderr
			return command.Run()
		},
		refreshAgent: func() (managedsession.ForwardedAgentStatus, error) {
			if forwardedAgentErr != nil {
				return managedsession.ForwardedAgentInvalid, forwardedAgentErr
			}
			return forwardedAgent.RefreshFromEnvironment(lookupEnv)
		},
	}
	return client.run(context.Background(), opts)
}

func tmuxAttachCommand(ctx context.Context, tmuxBinary, tmuxName string) *exec.Cmd {
	return tmux.ConfigureCommand(exec.CommandContext(ctx, tmuxBinary, "attach-session", "-E", "-t", tmuxName))
}

func writerIsTerminal(writer io.Writer, terminal func(*os.File) bool) bool {
	file, ok := writer.(*os.File)
	return ok && terminal(file)
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var state syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&state)), 0, 0, 0)
	return errno == 0
}

func parseOptions(args []string, lookupEnv func(string) (string, bool)) (options, error) {
	var parsed options
	var attachFlag, noAttachFlag bool
	positional := make([]string, 0, 1)
	parseFlags := true
	for _, argument := range args {
		if parseFlags && argument == "--" {
			parseFlags = false
			continue
		}
		if parseFlags {
			switch argument {
			case "-h", "--help":
				parsed.help = true
				continue
			case "--version":
				parsed.version = true
				continue
			case "--attach":
				attachFlag = true
				continue
			case "--no-attach":
				noAttachFlag = true
				continue
			}
			if strings.HasPrefix(argument, "-") {
				return options{}, fmt.Errorf("unknown option %q", argument)
			}
		}
		positional = append(positional, argument)
	}

	if attachFlag && noAttachFlag {
		return options{}, errors.New("--attach and --no-attach cannot be used together")
	}
	if parsed.help && parsed.version {
		return options{}, errors.New("--help and --version cannot be used together")
	}
	if (parsed.help || parsed.version) && (attachFlag || noAttachFlag || len(positional) != 0) {
		return options{}, errors.New("--help and --version cannot be combined with a session invocation")
	}
	if len(positional) > 1 {
		return options{}, errors.New("expected at most one session name")
	}
	if len(positional) == 1 {
		parsed.name = positional[0]
	}
	if parsed.help || parsed.version {
		return parsed, nil
	}

	parsed.attach = true
	if attachFlag {
		parsed.attach = true
	} else if noAttachFlag {
		parsed.attach = false
	} else {
		attach, err := attachFromEnvironment(lookupEnv)
		if err != nil {
			return options{}, err
		}
		parsed.attach = attach
	}
	if parsed.name == "" && !parsed.attach && !parsed.help && !parsed.version {
		return options{}, errors.New("non-interactive mode requires an explicit session name")
	}
	return parsed, nil
}

func attachFromEnvironment(lookupEnv func(string) (string, bool)) (bool, error) {
	if value, ok := lookupEnv("CONTROL_AGENTS_NO_ATTACH"); ok && value != "" {
		switch value {
		case "0":
		case "1":
			return false, nil
		default:
			return false, errors.New("CONTROL_AGENTS_NO_ATTACH must be 0 or 1")
		}
	}
	if value, ok := lookupEnv("CONTROL_AGENTS_ATTACH"); ok && value != "" {
		switch value {
		case "0":
			return false, nil
		case "1":
			return true, nil
		default:
			return false, errors.New("CONTROL_AGENTS_ATTACH must be 0 or 1")
		}
	}
	return true, nil
}

func (a *app) run(ctx context.Context, opts options) int {
	a.reportForwardedAgent()
	if opts.name != "" {
		managed, err := a.lifecycle.Create(ctx, opts.name)
		if err != nil {
			fmt.Fprintf(a.errorOut, "control-agents: %v\n", err)
			return lifecycleExitCode(err)
		}
		if !opts.attach {
			fmt.Fprintln(a.output, managed.ID)
			return exitSuccess
		}
		if err := a.attachManaged(ctx, managed); err != nil {
			fmt.Fprintf(a.errorOut, "control-agents: attach session %q: %v\n", managed.ID, err)
			return exitFailure
		}
		return exitSuccess
	}
	return a.runSelector(ctx)
}

func (a *app) reportForwardedAgent() {
	if a.refreshAgent == nil {
		return
	}
	status, err := a.refreshAgent()
	if err != nil {
		fmt.Fprintln(a.errorOut, "control-agents: forwarded SSH agent: invalid")
		return
	}
	switch status {
	case managedsession.ForwardedAgentAvailable:
		fmt.Fprintln(a.errorOut, "control-agents: forwarded SSH agent: available")
	case managedsession.ForwardedAgentInvalid:
		fmt.Fprintln(a.errorOut, "control-agents: forwarded SSH agent: invalid")
	default:
		fmt.Fprintln(a.errorOut, "control-agents: forwarded SSH agent: unavailable")
	}
}

func (a *app) runSelector(ctx context.Context) int {
	reader := bufio.NewReader(a.input)
	for {
		sessions, err := a.lifecycle.List(ctx)
		if err != nil {
			fmt.Fprintf(a.errorOut, "control-agents: refresh sessions: %v\n", err)
			sessions = nil
		}
		a.renderSelector(sessions)
		selection, err := a.readLine(ctx, reader)
		if errors.Is(err, io.EOF) || errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
			fmt.Fprintln(a.output)
			return exitSuccess
		}
		if err != nil {
			fmt.Fprintf(a.errorOut, "control-agents: read selection: %v\n", err)
			return exitFailure
		}

		switch strings.ToLower(strings.TrimSpace(selection)) {
		case "q", "quit":
			return exitSuccess
		case "n", "new":
			if exit, code := a.createFromPrompt(ctx, reader); exit {
				return code
			}
			continue
		}

		index, err := strconv.Atoi(strings.TrimSpace(selection))
		if err != nil || index < 1 || index > len(sessions) {
			fmt.Fprintln(a.errorOut, "Invalid selection. Choose a session number, n, or q.")
			continue
		}
		managed := sessions[index-1]
		if err := a.attachManaged(ctx, managed); err != nil {
			fmt.Fprintf(a.errorOut, "Session %q ended or is unavailable; refreshing the list.\n", managed.ID)
		}
	}
}

func (a *app) renderSelector(sessions []registry.Session) {
	fmt.Fprintln(a.output, "Control Agents sessions")
	fmt.Fprintln(a.output)
	for index, managed := range sessions {
		fmt.Fprintf(a.output, "%d) %s\n", index+1, managed.Name)
	}
	fmt.Fprintln(a.output, "n) New session")
	fmt.Fprintln(a.output, "q) Quit")
	fmt.Fprintln(a.output)
	fmt.Fprint(a.output, "Select: ")
}

func (a *app) createFromPrompt(ctx context.Context, reader *bufio.Reader) (bool, int) {
	fmt.Fprint(a.output, "Session name (empty to cancel): ")
	name, err := a.readLine(ctx, reader)
	if errors.Is(err, io.EOF) || errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
		fmt.Fprintln(a.output)
		return true, exitSuccess
	}
	if err != nil {
		fmt.Fprintf(a.errorOut, "control-agents: read session name: %v\n", err)
		return true, exitFailure
	}
	if name == "" {
		return false, exitSuccess
	}
	managed, err := a.lifecycle.Create(ctx, name)
	if err != nil {
		fmt.Fprintf(a.errorOut, "Could not create session: %v\n", err)
		return false, exitSuccess
	}
	if err := a.attachManaged(ctx, managed); err != nil {
		fmt.Fprintf(a.errorOut, "Session %q ended or is unavailable; refreshing the list.\n", managed.ID)
	}
	return false, exitSuccess
}

func (a *app) attachManaged(ctx context.Context, managed registry.Session) error {
	fmt.Fprintf(a.output, "Attaching to %s. Detach with Ctrl-b d.\n", managed.Name)
	err := a.attach(ctx, managed.TmuxName)
	a.drainInterrupts()
	if err != nil {
		return err
	}
	_, err = a.lifecycle.EnsureBridge(ctx, managed.ID)
	return err
}

func (a *app) drainInterrupts() {
	for a.interrupt != nil {
		select {
		case <-a.interrupt:
		default:
			return
		}
	}
}

func (a *app) readLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	result := make(chan lineResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if len(line) > 0 && errors.Is(err, io.EOF) {
			err = nil
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		result <- lineResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-a.interrupt:
		return "", errInterrupted
	case read := <-result:
		return read.line, read.err
	}
}

func lifecycleExitCode(err error) int {
	switch {
	case errors.Is(err, managedsession.ErrInvalidName):
		return exitUsage
	case errors.Is(err, managedsession.ErrConflict):
		return exitConflict
	default:
		return exitFailure
	}
}

const usageText = `Usage:
  control-agents
  control-agents [--attach] NAME
  control-agents --no-attach NAME

With no NAME, open the interactive managed-session selector. A named session
is created or reused and attached directly. Use --no-attach for scripts.

Options:
  --attach       Attach a named session, overriding environment settings.
  --no-attach    Create or reuse a named session, print its ID, and exit.
  --version      Print version information.
  -h, --help     Show this help.
`
