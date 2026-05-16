package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	Run(ctx context.Context, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

type Client struct {
	runner            CommandRunner
	processDescendant func(pid, ancestor int) bool
}

type ScrollState struct {
	Position       int  `json:"position"`
	HistorySize    int  `json:"historySize"`
	PaneHeight     int  `json:"paneHeight"`
	ScrollTop      int  `json:"scrollTop"`
	ScrollMax      int  `json:"scrollMax"`
	InCopyMode     bool `json:"inCopyMode"`
	WindowHeight   int  `json:"windowHeight,omitempty"`
	WindowOffsetY  int  `json:"windowOffsetY,omitempty"`
	WindowOverflow int  `json:"windowOverflow,omitempty"`
	clientName     string
	clientHeight   int
}

type ScrollRequest struct {
	Action string `json:"action"`
	Amount int    `json:"amount,omitempty"`
	Value  int    `json:"value,omitempty"`
}

type KeyRequest struct {
	Key string `json:"key"`
}

type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Panes  int    `json:"panes"`
}

type ControlRequest struct {
	Action      string `json:"action"`
	WindowIndex *int   `json:"windowIndex,omitempty"`
	Name        string `json:"name,omitempty"`
}

var ErrUnsupportedKey = errors.New("unsupported key")
var ErrUnsupportedControlAction = errors.New("unsupported tmux control action")
var ErrInvalidControlRequest = errors.New("invalid tmux control request")

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

func NewClientWithRunner(runner CommandRunner) *Client {
	return &Client{
		runner:            runner,
		processDescendant: processDescendsFrom,
	}
}

func (c *Client) Status(ctx context.Context, target string) (ScrollState, error) {
	return c.status(ctx, target, 0)
}

func (c *Client) StatusForProcess(ctx context.Context, target string, processPID int) (ScrollState, error) {
	return c.status(ctx, target, processPID)
}

func (c *Client) status(ctx context.Context, target string, processPID int) (ScrollState, error) {
	output, err := c.runner.Output(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}")
	if err != nil {
		return ScrollState{}, err
	}
	state, err := parseScrollState(string(output))
	if err != nil {
		return ScrollState{}, err
	}
	if view, err := c.scrollClient(ctx, target, processPID); err == nil {
		state.applyClientView(view)
	}
	return state, nil
}

func (c *Client) Scroll(ctx context.Context, target string, request ScrollRequest) (ScrollState, error) {
	return c.scroll(ctx, target, request, 0)
}

func (c *Client) ScrollForProcess(ctx context.Context, target string, processPID int, request ScrollRequest) (ScrollState, error) {
	return c.scroll(ctx, target, request, processPID)
}

func (c *Client) scroll(ctx context.Context, target string, request ScrollRequest, processPID int) (ScrollState, error) {
	state, err := c.status(ctx, target, processPID)
	if err != nil {
		return ScrollState{}, err
	}
	amount := normalizedAmount(request.Amount)

	switch request.Action {
	case "line-up":
		if err := c.lineUp(ctx, target, state, amount); err != nil {
			return ScrollState{}, err
		}
	case "line-down":
		if err := c.lineDown(ctx, target, state, amount, processPID); err != nil {
			return ScrollState{}, err
		}
	case "page-up":
		if err := c.pageUp(ctx, target, state, amount); err != nil {
			return ScrollState{}, err
		}
	case "page-down":
		if err := c.pageDown(ctx, target, state, amount, processPID); err != nil {
			return ScrollState{}, err
		}
	case "top":
		if err := c.top(ctx, target, state); err != nil {
			return ScrollState{}, err
		}
	case "bottom":
		if err := c.bottom(ctx, target, state, processPID); err != nil {
			return ScrollState{}, err
		}
	case "set":
		if err := c.setScrollTop(ctx, target, state, request.Value, processPID); err != nil {
			return ScrollState{}, err
		}
	default:
		return ScrollState{}, fmt.Errorf("unsupported scroll action %q", request.Action)
	}

	return c.status(ctx, target, processPID)
}

func (c *Client) SendKey(ctx context.Context, target string, request KeyRequest) error {
	key, ok := supportedKeys[strings.ToLower(strings.TrimSpace(request.Key))]
	if !ok {
		return fmt.Errorf("%w %q", ErrUnsupportedKey, request.Key)
	}

	state, err := c.Status(ctx, target)
	if err != nil {
		return err
	}
	if state.InCopyMode {
		if err := c.sendCopyCommand(ctx, target, 1, "cancel"); err != nil {
			return err
		}
	}
	return c.runner.Run(ctx, "tmux", "send-keys", "-t", target, key)
}

func (c *Client) Windows(ctx context.Context, target string) ([]Window, error) {
	output, err := c.runner.Output(ctx, "tmux", "list-windows", "-t", target, "-F", windowListFormat())
	if err != nil {
		return nil, err
	}
	return parseWindows(string(output))
}

func (c *Client) Control(ctx context.Context, target string, request ControlRequest) ([]Window, error) {
	if err := c.control(ctx, target, request); err != nil {
		return nil, err
	}
	return c.Windows(ctx, target)
}

func (c *Client) control(ctx context.Context, target string, request ControlRequest) error {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "new-window":
		return c.runner.Run(ctx, "tmux", "new-window", "-t", target+":", "-c", "#{pane_current_path}")
	case "select-window":
		if request.WindowIndex == nil || *request.WindowIndex < 0 {
			return fmt.Errorf("%w: missing window index", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, "tmux", "select-window", "-t", fmt.Sprintf("%s:%d", target, *request.WindowIndex))
	case "next-window":
		return c.runner.Run(ctx, "tmux", "next-window", "-t", target)
	case "previous-window":
		return c.runner.Run(ctx, "tmux", "previous-window", "-t", target)
	case "rename-window":
		name := strings.TrimSpace(request.Name)
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return fmt.Errorf("%w: invalid window name", ErrInvalidControlRequest)
		}
		return c.runner.Run(ctx, "tmux", "rename-window", "-t", target, name)
	case "split-horizontal":
		return c.runner.Run(ctx, "tmux", "split-window", "-h", "-t", target, "-c", "#{pane_current_path}")
	case "split-vertical":
		return c.runner.Run(ctx, "tmux", "split-window", "-v", "-t", target, "-c", "#{pane_current_path}")
	case "select-pane-left":
		return c.runner.Run(ctx, "tmux", "select-pane", "-t", target, "-L")
	case "select-pane-right":
		return c.runner.Run(ctx, "tmux", "select-pane", "-t", target, "-R")
	case "select-pane-up":
		return c.runner.Run(ctx, "tmux", "select-pane", "-t", target, "-U")
	case "select-pane-down":
		return c.runner.Run(ctx, "tmux", "select-pane", "-t", target, "-D")
	case "resize-pane-left":
		return c.runner.Run(ctx, "tmux", "resize-pane", "-t", target, "-L", "5")
	case "resize-pane-right":
		return c.runner.Run(ctx, "tmux", "resize-pane", "-t", target, "-R", "5")
	case "resize-pane-up":
		return c.runner.Run(ctx, "tmux", "resize-pane", "-t", target, "-U", "5")
	case "resize-pane-down":
		return c.runner.Run(ctx, "tmux", "resize-pane", "-t", target, "-D", "5")
	case "toggle-zoom":
		return c.runner.Run(ctx, "tmux", "resize-pane", "-Z", "-t", target)
	case "close-pane":
		return c.runner.Run(ctx, "tmux", "kill-pane", "-t", target)
	case "close-window":
		return c.runner.Run(ctx, "tmux", "kill-window", "-t", target)
	case "choose-window":
		return c.runner.Run(ctx, "tmux", "choose-tree", "-w", "-t", target)
	case "command-prompt":
		return c.runner.Run(ctx, "tmux", "command-prompt", "-t", target)
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedControlAction, request.Action)
	}
}

func (c *Client) copyMode(ctx context.Context, target string) error {
	return c.runner.Run(ctx, "tmux", "copy-mode", "-t", target)
}

func (c *Client) bottom(ctx context.Context, target string, state ScrollState, processPID int) error {
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	if err := c.sendCopyCommand(ctx, target, 1, "history-bottom"); err != nil {
		return err
	}
	if err := c.sendCopyCommand(ctx, target, 1, "cancel"); err != nil {
		return err
	}
	if state.hasClientOverflow() {
		next, err := c.status(ctx, target, processPID)
		if err != nil {
			return err
		}
		return c.setClientOffset(ctx, next, next.WindowOverflow)
	}
	return nil
}

func (c *Client) setScrollTop(ctx context.Context, target string, state ScrollState, value int, processPID int) error {
	if value < 0 {
		value = 0
	}
	if value > state.ScrollMax {
		value = state.ScrollMax
	}
	if value >= state.HistorySize {
		if state.InCopyMode || state.Position > 0 {
			if err := c.bottom(ctx, target, state, processPID); err != nil {
				return err
			}
			next, err := c.status(ctx, target, processPID)
			if err != nil {
				return err
			}
			state = next
		}
		return c.setClientOffset(ctx, state, value-state.HistorySize)
	}

	desiredPosition := state.HistorySize - value
	if err := c.setClientOffset(ctx, state, 0); err != nil {
		return err
	}
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	if err := c.sendCopyCommand(ctx, target, 1, "history-top"); err != nil {
		return err
	}
	delta := state.HistorySize - desiredPosition
	if delta > 0 {
		return c.sendCopyCommand(ctx, target, delta, "scroll-down")
	}
	return nil
}

func (c *Client) lineUp(ctx context.Context, target string, state ScrollState, amount int) error {
	if !state.InCopyMode && state.hasClientOverflow() && state.WindowOffsetY > 0 {
		step := min(amount, state.WindowOffsetY)
		if err := c.adjustClientOffset(ctx, state, "up", step); err != nil {
			return err
		}
		amount -= step
	}
	if amount <= 0 {
		return nil
	}
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	return c.sendCopyCommand(ctx, target, amount, "scroll-up")
}

func (c *Client) lineDown(ctx context.Context, target string, state ScrollState, amount int, processPID int) error {
	if state.InCopyMode || state.Position > 0 {
		if err := c.sendCopyCommand(ctx, target, amount, "scroll-down"); err != nil {
			return err
		}
		return c.cancelCopyModeAtLiveBottom(ctx, target, processPID)
	}
	return c.adjustClientOffset(ctx, state, "down", min(amount, state.WindowOverflow-state.WindowOffsetY))
}

func (c *Client) pageUp(ctx context.Context, target string, state ScrollState, amount int) error {
	if !state.InCopyMode && state.hasClientOverflow() && state.WindowOffsetY > 0 {
		step := min(amount*state.pageRows(), state.WindowOffsetY)
		if err := c.adjustClientOffset(ctx, state, "up", step); err != nil {
			return err
		}
		if step == amount*state.pageRows() {
			return nil
		}
	}
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	return c.sendCopyCommand(ctx, target, amount, "page-up")
}

func (c *Client) pageDown(ctx context.Context, target string, state ScrollState, amount int, processPID int) error {
	if state.InCopyMode || state.Position > 0 {
		if err := c.sendCopyCommand(ctx, target, amount, "page-down"); err != nil {
			return err
		}
		return c.cancelCopyModeAtLiveBottom(ctx, target, processPID)
	}
	return c.adjustClientOffset(ctx, state, "down", min(amount*state.pageRows(), state.WindowOverflow-state.WindowOffsetY))
}

func (c *Client) top(ctx context.Context, target string, state ScrollState) error {
	if err := c.setClientOffset(ctx, state, 0); err != nil {
		return err
	}
	if state.HistorySize <= 0 {
		return nil
	}
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	return c.sendCopyCommand(ctx, target, 1, "history-top")
}

func (c *Client) sendCopyCommand(ctx context.Context, target string, amount int, command string) error {
	if amount <= 1 {
		return c.runner.Run(ctx, "tmux", "send-keys", "-t", target, "-X", command)
	}
	return c.runner.Run(ctx, "tmux", "send-keys", "-t", target, "-X", "-N", strconv.Itoa(amount), command)
}

func (c *Client) setClientOffset(ctx context.Context, state ScrollState, value int) error {
	if !state.hasClientOverflow() {
		return nil
	}
	if value < 0 {
		value = 0
	}
	if value > state.WindowOverflow {
		value = state.WindowOverflow
	}
	delta := value - state.WindowOffsetY
	if delta > 0 {
		return c.adjustClientOffset(ctx, state, "down", delta)
	}
	if delta < 0 {
		return c.adjustClientOffset(ctx, state, "up", -delta)
	}
	return nil
}

func (c *Client) adjustClientOffset(ctx context.Context, state ScrollState, direction string, amount int) error {
	if amount <= 0 || state.clientName == "" {
		return nil
	}
	flag := "-D"
	if direction == "up" {
		flag = "-U"
	}
	return c.runner.Run(ctx, "tmux", "refresh-client", "-t", state.clientName, flag, strconv.Itoa(amount))
}

func (c *Client) cancelCopyModeAtLiveBottom(ctx context.Context, target string, processPID int) error {
	state, err := c.status(ctx, target, processPID)
	if err != nil {
		return err
	}
	if !state.InCopyMode || state.Position > 0 {
		return nil
	}
	if err := c.sendCopyCommand(ctx, target, 1, "cancel"); err != nil {
		return err
	}
	if state.hasClientOverflow() {
		next, err := c.status(ctx, target, processPID)
		if err != nil {
			return err
		}
		return c.setClientOffset(ctx, next, next.WindowOverflow)
	}
	return nil
}

func parseScrollState(output string) (ScrollState, error) {
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 4 {
		return ScrollState{}, errors.New("unexpected tmux scroll status format")
	}

	inCopyMode := parts[0] == "1"
	position, err := parseOptionalInt(parts[1])
	if err != nil {
		return ScrollState{}, fmt.Errorf("parse scroll position: %w", err)
	}
	historySize, err := parseOptionalInt(parts[2])
	if err != nil {
		return ScrollState{}, fmt.Errorf("parse history size: %w", err)
	}
	paneHeight, err := parseOptionalInt(parts[3])
	if err != nil {
		return ScrollState{}, fmt.Errorf("parse pane height: %w", err)
	}

	if position < 0 {
		position = 0
	}
	if historySize < 0 {
		historySize = 0
	}
	if position > historySize {
		position = historySize
	}
	scrollTop := historySize - position
	if scrollTop < 0 {
		scrollTop = 0
	}

	state := ScrollState{
		Position:    position,
		HistorySize: historySize,
		PaneHeight:  paneHeight,
		ScrollTop:   scrollTop,
		ScrollMax:   historySize,
		InCopyMode:  inCopyMode,
	}
	return state, nil
}

type clientView struct {
	name         string
	pid          int
	height       int
	windowHeight int
	offsetY      int
	statusOn     bool
}

func (c *Client) scrollClient(ctx context.Context, target string, processPID int) (clientView, error) {
	output, err := c.runner.Output(ctx, "tmux", "list-clients", "-t", target, "-F", "#{client_name}|#{client_pid}|#{client_height}|#{window_height}|#{window_offset_y}|#{status}")
	if err != nil {
		return clientView{}, err
	}
	var best clientView
	var preferred clientView
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		view, err := parseClientView(line)
		if err != nil {
			continue
		}
		if view.name == "" || view.height <= 0 || view.windowHeight <= 0 {
			continue
		}
		if processPID > 0 && c.belongsToProcess(view.pid, processPID) {
			if preferred.name == "" || view.height < preferred.height {
				preferred = view
			}
			continue
		}
		if best.name == "" || view.height < best.height {
			best = view
		}
	}
	if preferred.name != "" {
		return preferred, nil
	}
	if best.name == "" {
		return clientView{}, errors.New("no tmux clients")
	}
	return best, nil
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

func parseClientView(line string) (clientView, error) {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 6 {
		return clientView{}, errors.New("unexpected tmux client format")
	}
	pid, err := parseOptionalInt(parts[1])
	if err != nil {
		return clientView{}, fmt.Errorf("parse client pid: %w", err)
	}
	height, err := parseOptionalInt(parts[2])
	if err != nil {
		return clientView{}, fmt.Errorf("parse client height: %w", err)
	}
	windowHeight, err := parseOptionalInt(parts[3])
	if err != nil {
		return clientView{}, fmt.Errorf("parse window height: %w", err)
	}
	offsetY, err := parseOptionalInt(parts[4])
	if err != nil {
		return clientView{}, fmt.Errorf("parse window offset: %w", err)
	}
	if pid < 0 {
		pid = 0
	}
	if height < 0 {
		height = 0
	}
	if windowHeight < 0 {
		windowHeight = 0
	}
	if offsetY < 0 {
		offsetY = 0
	}
	return clientView{
		name:         parts[0],
		pid:          pid,
		height:       height,
		windowHeight: windowHeight,
		offsetY:      offsetY,
		statusOn:     strings.EqualFold(parts[5], "on") || parts[5] == "1",
	}, nil
}

func (state *ScrollState) applyClientView(view clientView) {
	if view.name == "" || view.height <= 0 || view.windowHeight <= 0 {
		return
	}
	visibleHeight := view.visiblePaneHeight()
	overflow := view.windowHeight - visibleHeight
	if overflow < 0 {
		overflow = 0
	}
	if view.offsetY > overflow {
		view.offsetY = overflow
	}
	state.clientName = view.name
	state.clientHeight = visibleHeight
	state.WindowHeight = view.windowHeight
	state.WindowOffsetY = view.offsetY
	state.WindowOverflow = overflow
	state.ScrollMax = state.HistorySize + overflow
	state.ScrollTop = state.HistorySize - state.Position + view.offsetY
	if state.ScrollTop < 0 {
		state.ScrollTop = 0
	}
	if state.ScrollTop > state.ScrollMax {
		state.ScrollTop = state.ScrollMax
	}
	if overflow > 0 && visibleHeight < state.PaneHeight {
		state.PaneHeight = visibleHeight
	}
}

func (view clientView) visiblePaneHeight() int {
	height := view.height
	if view.statusOn {
		height--
	}
	if height < 1 {
		return 1
	}
	return height
}

func (state ScrollState) hasClientOverflow() bool {
	return state.clientName != "" && state.WindowOverflow > 0
}

func (state ScrollState) pageRows() int {
	rows := state.clientHeight
	if rows <= 0 {
		rows = state.PaneHeight
	}
	if rows <= 1 {
		return 1
	}
	return rows - 1
}

func normalizedAmount(amount int) int {
	if amount <= 0 {
		return 1
	}
	return amount
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
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
	output, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
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
