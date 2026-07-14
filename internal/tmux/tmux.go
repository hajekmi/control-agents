package tmux

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	Run(ctx context.Context, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return commandContext(ctx, name, args...).Output()
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return commandContext(ctx, name, args...).Run()
}

func (ExecRunner) RunWithInput(ctx context.Context, input io.Reader, name string, args ...string) error {
	command := commandContext(ctx, name, args...)
	command.Stdin = input
	return command.Run()
}

func (ExecRunner) OutputLimited(ctx context.Context, limit int64, name string, args ...string) ([]byte, error) {
	if limit <= 0 {
		return nil, ErrSnapshotTooLarge
	}
	command := commandContext(ctx, name, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if int64(len(output)) > limit {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, ErrSnapshotTooLarge
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return output, nil
}

const (
	SupportedVersion = "3.7b"
	UTF8Locale       = "C.UTF-8"
)

// ResolveBinary selects the managed tmux executable independently of the
// caller's PATH. Release-installed server and client binaries prefer the tmux
// executable installed beside them, then the default user-local destination.
// PATH is only a development fallback. Every selected executable must report
// the one tmux version supported by this release.
func ResolveBinary(homeDir string) (string, error) {
	executable, _ := os.Executable()
	return resolveBinary(executable, homeDir, exec.LookPath)
}

func resolveBinary(executable, homeDir string, lookPath func(string) (string, error)) (string, error) {
	candidates := make([]string, 0, 2)
	if executable != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "tmux"))
	}
	if homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".local", "bin", "tmux"))
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return "", fmt.Errorf("resolve tmux executable %q: %w", candidate, err)
		}
		if _, duplicate := seen[absolute]; duplicate {
			continue
		}
		seen[absolute] = struct{}{}
		if _, err := os.Stat(absolute); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("inspect tmux executable %q: %w", absolute, err)
		}
		if err := VerifyBinary(absolute); err != nil {
			return "", err
		}
		return absolute, nil
	}

	selected, err := lookPath("tmux")
	if err != nil {
		return "", fmt.Errorf("tmux %s is required; install it with install-tmux.sh", SupportedVersion)
	}
	absolute, err := filepath.Abs(selected)
	if err != nil {
		return "", fmt.Errorf("resolve tmux executable %q: %w", selected, err)
	}
	if err := VerifyBinary(absolute); err != nil {
		return "", err
	}
	return absolute, nil
}

// VerifyBinary enforces the exact supported tmux release before any managed
// session command is allowed to run.
func VerifyBinary(binary string) error {
	output, err := ConfigureCommand(exec.Command(binary, "-V")).Output()
	if err != nil {
		return fmt.Errorf("verify tmux executable %q: %w", binary, err)
	}
	version := strings.TrimSpace(string(output))
	want := "tmux " + SupportedVersion
	if version != want {
		return fmt.Errorf("tmux %s is required; selected %s reports %s", SupportedVersion, binary, version)
	}
	return nil
}

// UTF8Environment returns a copy of an environment with the deterministic
// UTF-8 locale required by tmux format expansion. Tmux 3.7b rewrites control
// delimiters in the C locale, which would make managed topology unavailable.
func UTF8Environment(environment []string) []string {
	configured := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if name != "LANG" && name != "LC_ALL" {
			configured = append(configured, entry)
		}
	}
	return append(configured, "LANG="+UTF8Locale, "LC_ALL="+UTF8Locale)
}

// ConfigureCommand applies the managed tmux locale to a direct command, such
// as the Go SSH client's interactive attach or the ttyd bridge process.
func ConfigureCommand(command *exec.Cmd) *exec.Cmd {
	environment := command.Env
	if environment == nil {
		environment = os.Environ()
	}
	command.Env = UTF8Environment(environment)
	return command
}

func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return ConfigureCommand(exec.CommandContext(ctx, name, args...))
}

type Client struct {
	runner            CommandRunner
	processDescendant func(pid, ancestor int) bool
	binary            string
}

type KeyRequest struct {
	Key string `json:"key"`
}

type HistoryMetadata struct {
	Columns         int
	Rows            int
	HistorySize     int
	HistoryLimit    int
	HistoryBytes    int64
	AlternateScreen bool
	OutputEpoch     int64
}

type HistoryCapture struct {
	Text   string
	Before HistoryMetadata
	After  HistoryMetadata
}

type PasteRequest struct {
	Text string `json:"text"`
}

type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Panes  int    `json:"panes"`
}

type PaneGeneration struct {
	ServerStart string
	ServerPID   int
	PaneID      string
}

type TopologyPane struct {
	ID         string
	WindowID   string
	Name       string
	Active     bool
	WindowName string
	WindowOpen bool
	Width      int
	Height     int
	Generation PaneGeneration
}

type Topology struct {
	ServerStart string
	ServerPID   int
	Panes       []TopologyPane
}

type ControlRequest struct {
	Action string
	Name   string
}

type ResizeState struct {
	Mode       string `json:"mode,omitempty"`
	ClientName string `json:"clientName,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

type ResizeClient struct {
	Name     string `json:"name"`
	PID      int    `json:"pid,omitempty"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Activity int64  `json:"activity,omitempty"`
	StatusOn bool   `json:"statusOn,omitempty"`
	Web      bool   `json:"web"`
}

type ManagedSessionOptions struct {
	WindowSize  string
	Mouse       string
	StatusLeft  string
	SSHAuthSock string
}

var ErrUnsupportedKey = errors.New("unsupported key")
var ErrUnsupportedControlAction = errors.New("unsupported tmux control action")
var ErrInvalidControlRequest = errors.New("invalid tmux control request")
var ErrInvalidPaste = errors.New("invalid paste")
var ErrInvalidInput = errors.New("invalid terminal input")
var ErrSnapshotTooLarge = errors.New("terminal snapshot exceeds byte limit")
var ErrPaneGenerationChanged = errors.New("tmux pane generation changed")

const (
	MaxPasteBytes        = 64 * 1024
	DefaultSnapshotBytes = 32 * 1024 * 1024
	ManagedHistoryLimit  = 50000
	managedWindowHook    = "window-linked[900]"
	managedWindowCommand = "set-option -w window-size manual"
)

var rawWindowIDPattern = regexp.MustCompile(`^@[0-9]+$`)
var rawPaneIDPattern = regexp.MustCompile(`^%[0-9]+$`)
var positiveDecimalPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

var supportedKeys = map[string]string{
	"ctrl-a":    "C-a",
	"ctrl-c":    "C-c",
	"ctrl-d":    "C-d",
	"ctrl-e":    "C-e",
	"ctrl-k":    "C-k",
	"ctrl-l":    "C-l",
	"ctrl-r":    "C-r",
	"ctrl-u":    "C-u",
	"ctrl-w":    "C-w",
	"ctrl-z":    "C-z",
	"escape":    "Escape",
	"tab":       "Tab",
	"enter":     "Enter",
	"backspace": "BSpace",
	"delete":    "DC",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"page-up":   "PPage",
	"page-down": "NPage",
}

func NewClient() *Client {
	return NewClientWithRunner(ExecRunner{})
}

func NewClientWithBinary(binary string) *Client {
	client := NewClientWithRunner(ExecRunner{})
	if strings.TrimSpace(binary) != "" {
		client.binary = binary
	}
	return client
}

func NewClientWithRunner(runner CommandRunner) *Client {
	return &Client{
		runner:            runner,
		processDescendant: processDescendsFrom,
		binary:            "tmux",
	}
}

func (c *Client) HasSession(ctx context.Context, target string) (bool, error) {
	err := c.runner.Run(ctx, c.binary, "has-session", "-t", target)
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return false, nil
	}
	return false, err
}

func (c *Client) CreateManagedSession(ctx context.Context, name, home string, options ManagedSessionOptions) error {
	if strings.TrimSpace(home) == "" {
		return errors.New("managed session home directory cannot be empty")
	}
	// The bootstrap pane directly executes tmux wait-for and is removed before
	// this function returns. This gives the new session an exact, private scope
	// where its history default and window hook can be installed before the
	// durable user shell is created, without changing global tmux options.
	arguments := []string{
		"start-server", ";",
		"new-session", "-d", "-s", name, "-c", home,
	}
	if options.SSHAuthSock != "" {
		arguments = append(arguments, "-e", "SSH_AUTH_SOCK="+options.SSHAuthSock)
	}
	arguments = append(arguments,
		c.binary, "wait-for", "control-agents-bootstrap-"+name, ";",
		"set-option", "-t", name, "history-limit", strconv.Itoa(ManagedHistoryLimit), ";",
		"set-hook", "-t", name, managedWindowHook, managedWindowCommand, ";",
		"new-window", "-d", "-a", "-t", name+":", "-c", home, ";",
		"kill-window", "-t", name+":",
	)
	if err := c.runner.Run(ctx, c.binary, arguments...); err != nil {
		return err
	}
	return c.ConfigureManagedSession(ctx, name, options)
}

func (c *Client) ConfigureManagedSession(ctx context.Context, name string, options ManagedSessionOptions) error {
	if err := c.ConfigureHistory(ctx, name); err != nil {
		return err
	}
	if err := c.ConfigureManualWindowSize(ctx, name); err != nil {
		return err
	}
	commands := [][]string{
		{"set-option", "-t", name, "destroy-unattached", "off"},
		{"set-option", "-t", name, "mouse", options.Mouse},
		{"set-option", "-t", name, "status-left-length", "80"},
		{"set-option", "-t", name, "status-left", escapeFormatLiteral(options.StatusLeft)},
		{"set-option", "-t", name, "status-right", "#{pane_current_path}"},
	}
	if options.SSHAuthSock != "" {
		commands = append(commands, []string{"set-environment", "-t", name, "SSH_AUTH_SOCK", options.SSHAuthSock})
	}
	for _, args := range commands {
		if err := c.runner.Run(ctx, c.binary, args...); err != nil {
			return err
		}
	}
	return nil
}

// ConfigureHistory updates only the exact managed session. Tmux does not
// reconstruct history already discarded by existing panes, so creation calls
// install this session default before creating the durable user pane.
func (c *Client) ConfigureHistory(ctx context.Context, target string) error {
	return c.runner.Run(ctx, c.binary, "set-option", "-t", target, "history-limit", strconv.Itoa(ManagedHistoryLimit))
}

// ConfigureManualWindowSize installs the session-local synchronous hook before
// migrating existing windows. A concurrently created or linked window is
// therefore covered either by the hook or by the following enumeration, with
// no periodic-reconciliation gap. Setting the option does not call
// resize-window, so existing windows keep their current dimensions.
func (c *Client) ConfigureManualWindowSize(ctx context.Context, target string) error {
	if err := c.runner.Run(ctx, c.binary, "set-hook", "-t", target, managedWindowHook, managedWindowCommand); err != nil {
		return err
	}
	windowIDs, err := c.managedWindowIDs(ctx, target)
	if err != nil {
		return err
	}
	for _, windowID := range windowIDs {
		if err := c.runner.Run(ctx, c.binary, "set-option", "-w", "-t", windowID, "window-size", "manual"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) managedWindowIDs(ctx context.Context, target string) ([]string, error) {
	output, err := c.runner.Output(ctx, c.binary, "list-windows", "-t", target, "-F", "#{window_id}")
	if err != nil {
		return nil, err
	}
	windowIDs := strings.Fields(string(output))
	if len(windowIDs) == 0 {
		return nil, errors.New("managed tmux session contains no windows")
	}
	for _, windowID := range windowIDs {
		if !rawWindowIDPattern.MatchString(windowID) {
			return nil, errors.New("unexpected tmux window id")
		}
	}
	return windowIDs, nil
}

// SetSessionEnvironment updates one variable in a tmux session environment so
// future windows and panes inherit it without mutating tmux's global state.
func (c *Client) SetSessionEnvironment(ctx context.Context, target, name, value string) error {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "=") {
		return errors.New("invalid tmux environment variable name")
	}
	return c.runner.Run(ctx, c.binary, "set-environment", "-t", target, name, value)
}

func (c *Client) KillSession(ctx context.Context, target string) error {
	return c.runner.Run(ctx, c.binary, "kill-session", "-t", target)
}

func (c *Client) CaptureHistory(ctx context.Context, target string, maxBytes int64, outputEpoch func() int64) (HistoryCapture, error) {
	before, err := c.historyMetadata(ctx, target)
	if err != nil {
		return HistoryCapture{}, err
	}
	if outputEpoch != nil {
		before.OutputEpoch = outputEpoch()
	}
	var output []byte
	if runner, ok := c.runner.(interface {
		OutputLimited(context.Context, int64, string, ...string) ([]byte, error)
	}); ok {
		output, err = runner.OutputLimited(ctx, maxBytes, c.binary, "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", target)
	} else {
		output, err = c.runner.Output(ctx, c.binary, "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", target)
		if err == nil && int64(len(output)) > maxBytes {
			err = ErrSnapshotTooLarge
		}
	}
	if err != nil {
		return HistoryCapture{}, err
	}
	afterOutputEpoch := int64(0)
	if outputEpoch != nil {
		afterOutputEpoch = outputEpoch()
	}
	after, err := c.historyMetadata(ctx, target)
	if err != nil {
		return HistoryCapture{}, err
	}
	after.OutputEpoch = afterOutputEpoch
	return HistoryCapture{
		Text:   strings.TrimSuffix(string(output), "\n"),
		Before: before,
		After:  after,
	}, nil
}

func (c *Client) historyMetadata(ctx context.Context, target string) (HistoryMetadata, error) {
	format := strings.Join([]string{
		"#{pane_width}", "#{pane_height}", "#{history_size}",
		"#{history_limit}", "#{history_bytes}", "#{alternate_on}",
	}, "\x1f")
	output, err := c.runner.Output(ctx, c.binary, "display-message", "-p", "-t", target, format)
	if err != nil {
		return HistoryMetadata{}, err
	}
	return parseHistoryMetadata(string(output))
}

func (c *Client) HistoryActivity(ctx context.Context, target string) (HistoryMetadata, error) {
	return c.historyMetadata(ctx, target)
}

func (c *Client) Topology(ctx context.Context, target string) (Topology, error) {
	incarnationOutput, err := c.runner.Output(ctx, c.binary, "display-message", "-p", "-t", target, "#{start_time}\x1f#{pid}")
	if err != nil {
		return Topology{}, err
	}
	serverStart, serverPID, err := parseServerIncarnation(string(incarnationOutput))
	if err != nil {
		return Topology{}, err
	}
	format := strings.Join([]string{
		"#{window_id}", "#{pane_id}", "#{window_active}", "#{pane_active}",
		"#{window_width}", "#{window_height}", "#{window_name}",
	}, "\x1f")
	output, err := c.runner.Output(ctx, c.binary, "list-panes", "-s", "-t", target, "-F", format)
	if err != nil {
		return Topology{}, err
	}
	topology := Topology{ServerStart: serverStart, ServerPID: serverPID}
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 7)
		if len(parts) != 7 || !rawWindowIDPattern.MatchString(parts[0]) || !rawPaneIDPattern.MatchString(parts[1]) {
			continue
		}
		width, widthErr := strconv.Atoi(parts[4])
		height, heightErr := strconv.Atoi(parts[5])
		if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
			continue
		}
		topology.Panes = append(topology.Panes, TopologyPane{
			ID:         parts[1],
			WindowID:   parts[0],
			Active:     parts[3] == "1",
			WindowName: parts[6],
			WindowOpen: parts[2] == "1",
			Width:      width,
			Height:     height,
			Generation: PaneGeneration{ServerStart: serverStart, ServerPID: serverPID, PaneID: parts[1]},
		})
	}
	if len(topology.Panes) == 0 {
		return Topology{}, errors.New("tmux topology contains no panes")
	}
	return topology, nil
}

func (c *Client) VerifyPaneGeneration(ctx context.Context, paneID string, expected PaneGeneration) error {
	if !rawPaneIDPattern.MatchString(paneID) || paneID != expected.PaneID || !validServerIncarnation(expected.ServerStart, expected.ServerPID) {
		return ErrPaneGenerationChanged
	}
	output, err := c.runner.Output(ctx, c.binary, "display-message", "-p", "-t", paneID, "#{start_time}\x1f#{pid}\x1f#{pane_id}")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPaneGenerationChanged, err)
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "\x1f")
	if len(parts) != 3 {
		return ErrPaneGenerationChanged
	}
	serverStart, serverPID, err := parseServerIncarnation(parts[0] + "\x1f" + parts[1])
	if err != nil || serverStart != expected.ServerStart || serverPID != expected.ServerPID || parts[2] != expected.PaneID {
		return ErrPaneGenerationChanged
	}
	return nil
}

func parseServerIncarnation(output string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(output), "\x1f")
	if len(parts) != 2 || !positiveDecimalPattern.MatchString(parts[0]) || !positiveDecimalPattern.MatchString(parts[1]) {
		return "", 0, errors.New("unexpected tmux server incarnation format")
	}
	serverPID, err := strconv.Atoi(parts[1])
	if err != nil || serverPID <= 0 {
		return "", 0, errors.New("unexpected tmux server incarnation format")
	}
	return parts[0], serverPID, nil
}

func validServerIncarnation(serverStart string, serverPID int) bool {
	return positiveDecimalPattern.MatchString(serverStart) && serverPID > 0
}

func (c *Client) Paste(ctx context.Context, target string, request PasteRequest) (result error) {
	text := request.Text
	if !utf8.ValidString(text) || len(text) > MaxPasteBytes || strings.ContainsRune(text, '\x00') {
		return ErrInvalidPaste
	}
	if text == "" {
		return nil
	}
	inputRunner, ok := c.runner.(interface {
		RunWithInput(context.Context, io.Reader, string, ...string) error
	})
	if !ok {
		return errors.New("tmux command runner does not support stdin")
	}
	random := make([]byte, 18)
	if _, err := rand.Read(random); err != nil {
		return errors.New("generate tmux paste buffer identity")
	}
	buffer := "control-agents-paste-" + base64.RawURLEncoding.EncodeToString(random)
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result = errors.Join(result, c.runner.Run(cleanupContext, c.binary, "delete-buffer", "-b", buffer))
	}()
	if err := inputRunner.RunWithInput(ctx, strings.NewReader(text), c.binary, "load-buffer", "-b", buffer, "-"); err != nil {
		return err
	}
	return c.runner.Run(ctx, c.binary, "paste-buffer", "-p", "-r", "-b", buffer, "-t", target)
}

func (c *Client) SendKey(ctx context.Context, target string, request KeyRequest) error {
	key, ok := supportedKeys[strings.ToLower(strings.TrimSpace(request.Key))]
	if !ok {
		return fmt.Errorf("%w %q", ErrUnsupportedKey, request.Key)
	}
	return c.runner.Run(ctx, c.binary, "send-keys", "-t", target, key)
}

// SendText forwards one printable input rune after the browser has returned
// from local History to Live. The literal flag and direct argument vector keep
// the text out of tmux command parsing and shell interpolation.
func (c *Client) SendText(ctx context.Context, target, text string) error {
	if len([]rune(text)) != 1 || strings.IndexFunc(text, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return ErrInvalidInput
	}
	return c.runner.Run(ctx, c.binary, "send-keys", "-l", "-t", target, "--", text)
}

func (c *Client) Windows(ctx context.Context, target string) ([]Window, error) {
	output, err := c.runner.Output(ctx, c.binary, "list-windows", "-t", target, "-F", windowListFormat())
	if err != nil {
		return nil, err
	}
	return parseWindows(string(output))
}

func (c *Client) Control(ctx context.Context, paneTarget, windowTarget string, request ControlRequest) error {
	return c.control(ctx, paneTarget, windowTarget, request)
}

func (c *Client) ListResizeClients(ctx context.Context, target string, processPID int) ([]ResizeClient, error) {
	views, err := c.resizeClients(ctx, target)
	if err != nil {
		return nil, err
	}
	clients := make([]ResizeClient, 0, len(views))
	for _, view := range views {
		clients = append(clients, c.resizeClientView(view, processPID))
	}
	return clients, nil
}

func (c *Client) PrimaryResizeClient(ctx context.Context, target string, processPID int) (ResizeClient, error) {
	views, err := c.resizeClients(ctx, target)
	if err != nil {
		return ResizeClient{}, err
	}
	var best resizeClientView
	for _, view := range views {
		if processPID > 0 && c.belongsToProcess(view.pid, processPID) {
			continue
		}
		if betterResizeClient(view, best) {
			best = view
		}
	}
	if best.name == "" {
		return ResizeClient{}, errors.New("no primary tmux clients")
	}
	return c.resizeClientView(best, processPID), nil
}

func (c *Client) ResizeManual(ctx context.Context, target string, width, height int) (ResizeState, error) {
	if width <= 0 || height <= 0 {
		return ResizeState{}, errors.New("invalid tmux resize dimensions")
	}
	if err := c.runner.Run(ctx, c.binary, "set-option", "-w", "-t", target, "window-size", "manual"); err != nil {
		return ResizeState{}, err
	}
	if err := c.runner.Run(ctx, c.binary, "resize-window", "-t", target, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height)); err != nil {
		return ResizeState{}, err
	}
	return ResizeState{
		Mode:   "manual",
		Width:  width,
		Height: height,
	}, nil
}

func (c *Client) ResizeFixed(ctx context.Context, target string) (ResizeState, error) {
	if err := c.runner.Run(ctx, c.binary, "set-option", "-w", "-t", target, "window-size", "manual"); err != nil {
		return ResizeState{}, err
	}
	return ResizeState{Mode: "fixed"}, nil
}

func (c *Client) ResizeSmallest(ctx context.Context, target string) (ResizeState, error) {
	if err := c.runner.Run(ctx, c.binary, "set-option", "-w", "-t", target, "window-size", "smallest"); err != nil {
		return ResizeState{}, err
	}
	return ResizeState{Mode: "smallest"}, nil
}

func (c *Client) control(ctx context.Context, paneTarget, windowTarget string, request ControlRequest) error {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "new-window":
		if !rawWindowIDPattern.MatchString(windowTarget) {
			return fmt.Errorf("%w: missing window reference", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, c.binary, "new-window", "-a", "-t", windowTarget, "-c", "#{pane_current_path}")
	case "select-window":
		if !rawWindowIDPattern.MatchString(windowTarget) {
			return fmt.Errorf("%w: missing window reference", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, c.binary, "select-window", "-t", windowTarget)
	case "next-window":
		return c.runner.Run(ctx, c.binary, "next-window", "-t", paneTarget)
	case "previous-window":
		return c.runner.Run(ctx, c.binary, "previous-window", "-t", paneTarget)
	case "rename-window":
		name := strings.TrimSpace(request.Name)
		if name == "" || strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return fmt.Errorf("%w: invalid window name", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, c.binary, "rename-window", "-t", paneTarget, name)
	case "split-horizontal":
		return c.runner.Run(ctx, c.binary, "split-window", "-h", "-t", paneTarget, "-c", "#{pane_current_path}")
	case "split-vertical":
		return c.runner.Run(ctx, c.binary, "split-window", "-v", "-t", paneTarget, "-c", "#{pane_current_path}")
	case "select-pane-left":
		return c.runner.Run(ctx, c.binary, "select-pane", "-t", paneTarget, "-L")
	case "select-pane-right":
		return c.runner.Run(ctx, c.binary, "select-pane", "-t", paneTarget, "-R")
	case "select-pane-up":
		return c.runner.Run(ctx, c.binary, "select-pane", "-t", paneTarget, "-U")
	case "select-pane-down":
		return c.runner.Run(ctx, c.binary, "select-pane", "-t", paneTarget, "-D")
	case "resize-pane-left":
		return c.runner.Run(ctx, c.binary, "resize-pane", "-t", paneTarget, "-L", "5")
	case "resize-pane-right":
		return c.runner.Run(ctx, c.binary, "resize-pane", "-t", paneTarget, "-R", "5")
	case "resize-pane-up":
		return c.runner.Run(ctx, c.binary, "resize-pane", "-t", paneTarget, "-U", "5")
	case "resize-pane-down":
		return c.runner.Run(ctx, c.binary, "resize-pane", "-t", paneTarget, "-D", "5")
	case "toggle-zoom":
		return c.runner.Run(ctx, c.binary, "resize-pane", "-Z", "-t", paneTarget)
	case "close-pane":
		return c.runner.Run(ctx, c.binary, "kill-pane", "-t", paneTarget)
	case "close-window":
		if !rawWindowIDPattern.MatchString(windowTarget) {
			return fmt.Errorf("%w: missing window reference", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, c.binary, "kill-window", "-t", windowTarget)
	case "choose-window":
		return c.runner.Run(ctx, c.binary, "choose-tree", "-w", "-t", paneTarget)
	case "command-prompt":
		return c.runner.Run(ctx, c.binary, "command-prompt", "-t", paneTarget)
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedControlAction, request.Action)
	}
}

func escapeFormatLiteral(value string) string {
	return strings.ReplaceAll(value, "#", "##")
}

type resizeClientView struct {
	name           string
	pid            int
	width          int
	height         int
	heightIsWindow bool
	activity       int64
	statusOn       bool
}

func (c *Client) resizeClients(ctx context.Context, target string) ([]resizeClientView, error) {
	output, err := c.runner.Output(ctx, c.binary, "list-clients", "-t", target, "-F", "#{client_name}|#{client_pid}|#{client_width}|#{client_height}|#{client_activity}|#{status}|#{window_width}|#{window_height}")
	if err != nil {
		return nil, err
	}
	views := make([]resizeClientView, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		view, err := parseResizeClientView(line)
		if err != nil {
			continue
		}
		if view.name == "" || view.width <= 0 || view.height <= 0 {
			continue
		}
		views = append(views, view)
	}
	if len(views) == 0 {
		return nil, errors.New("no tmux clients")
	}
	return views, nil
}

func (c *Client) resizeClientView(view resizeClientView, processPID int) ResizeClient {
	return ResizeClient{
		Name:     view.name,
		PID:      view.pid,
		Width:    view.width,
		Height:   view.visiblePaneHeight(),
		Activity: view.activity,
		StatusOn: view.statusOn,
		Web:      processPID > 0 && c.belongsToProcess(view.pid, processPID),
	}
}

func betterResizeClient(candidate, current resizeClientView) bool {
	if current.name == "" {
		return true
	}
	if candidate.activity != current.activity {
		return candidate.activity > current.activity
	}
	candidateArea := candidate.width * candidate.visiblePaneHeight()
	currentArea := current.width * current.visiblePaneHeight()
	if candidateArea != currentArea {
		return candidateArea > currentArea
	}
	return candidate.height > current.height
}

func (c *Client) belongsToProcess(pid, ancestor int) bool {
	if pid <= 0 || ancestor <= 0 {
		return false
	}
	if pid == ancestor {
		return true
	}
	if c.processDescendant == nil {
		return false
	}
	return c.processDescendant(pid, ancestor)
}

func parseResizeClientView(line string) (resizeClientView, error) {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 8 {
		return resizeClientView{}, errors.New("unexpected tmux resize client format")
	}
	pid, err := parseOptionalInt(parts[1])
	if err != nil {
		return resizeClientView{}, fmt.Errorf("parse client pid: %w", err)
	}
	width, err := parseOptionalInt(parts[2])
	if err != nil {
		return resizeClientView{}, fmt.Errorf("parse client width: %w", err)
	}
	height, err := parseOptionalInt(parts[3])
	if err != nil {
		return resizeClientView{}, fmt.Errorf("parse client height: %w", err)
	}
	activity, err := parseOptionalInt64(parts[4])
	if err != nil {
		return resizeClientView{}, fmt.Errorf("parse client activity: %w", err)
	}
	if pid < 0 {
		pid = 0
	}
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	heightIsWindow := false
	if height == 0 {
		windowHeight, err := parseOptionalInt(parts[7])
		if err != nil {
			return resizeClientView{}, fmt.Errorf("parse window height: %w", err)
		}
		if windowHeight < 0 {
			windowHeight = 0
		}
		if windowHeight > 0 {
			height = windowHeight
			heightIsWindow = true
		}
	}
	return resizeClientView{
		name:           parts[0],
		pid:            pid,
		width:          width,
		height:         height,
		heightIsWindow: heightIsWindow,
		activity:       activity,
		statusOn:       strings.EqualFold(parts[5], "on") || parts[5] == "1",
	}, nil
}

func (view resizeClientView) visiblePaneHeight() int {
	height := view.height
	if view.statusOn && !view.heightIsWindow {
		height--
	}
	if height < 1 {
		return 1
	}
	return height
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseHistoryMetadata(output string) (HistoryMetadata, error) {
	parts := strings.Split(strings.TrimSpace(output), "\x1f")
	if len(parts) != 6 {
		return HistoryMetadata{}, errors.New("unexpected tmux history metadata format")
	}
	values := make([]int64, 6)
	for index, value := range parts {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return HistoryMetadata{}, errors.New("unexpected tmux history metadata format")
		}
		values[index] = parsed
	}
	if values[0] <= 0 || values[1] <= 0 || values[3] <= 0 || values[5] > 1 {
		return HistoryMetadata{}, errors.New("unexpected tmux history metadata format")
	}
	return HistoryMetadata{
		Columns:         int(values[0]),
		Rows:            int(values[1]),
		HistorySize:     int(values[2]),
		HistoryLimit:    int(values[3]),
		HistoryBytes:    values[4],
		AlternateScreen: values[5] == 1,
	}, nil
}

func windowListFormat() string {
	return strings.Join([]string{"#{window_index}", "#{window_name}", "#{window_active}", "#{window_panes}"}, "\x1f")
}

func parseWindows(output string) ([]Window, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	windows := make([]Window, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		window, err := parseWindow(line)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func parseWindow(line string) (Window, error) {
	parts := strings.Split(strings.TrimSpace(line), "\x1f")
	if len(parts) != 4 {
		return Window{}, errors.New("unexpected tmux window format")
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil {
		return Window{}, fmt.Errorf("parse window index: %w", err)
	}
	panes, err := parseOptionalInt(parts[3])
	if err != nil {
		return Window{}, fmt.Errorf("parse window panes: %w", err)
	}
	if index < 0 {
		index = 0
	}
	if panes < 0 {
		panes = 0
	}
	return Window{
		Index:  index,
		Name:   parts[1],
		Active: parts[2] == "1",
		Panes:  panes,
	}, nil
}

func processDescendsFrom(pid, ancestor int) bool {
	seen := make(map[int]bool)
	for pid > 1 && !seen[pid] {
		if pid == ancestor {
			return true
		}
		seen[pid] = true
		parent, err := parentPID(pid)
		if err != nil || parent <= 0 || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}

func parentPID(pid int) (int, error) {
	if parent, err := parentPIDFromProc(pid); err == nil {
		return parent, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(output)))
}

func parentPIDFromProc(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return 0, errors.New("unexpected proc stat format")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 2 {
		return 0, errors.New("unexpected proc stat fields")
	}
	return strconv.Atoi(fields[1])
}
