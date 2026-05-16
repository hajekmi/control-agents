package tmux

import (
	"context"
	"errors"
	"fmt"
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
	runner CommandRunner
}

type ScrollState struct {
	Position    int  `json:"position"`
	HistorySize int  `json:"historySize"`
	PaneHeight  int  `json:"paneHeight"`
	ScrollTop   int  `json:"scrollTop"`
	ScrollMax   int  `json:"scrollMax"`
	InCopyMode  bool `json:"inCopyMode"`
}

type ScrollRequest struct {
	Action string `json:"action"`
	Amount int    `json:"amount,omitempty"`
	Value  int    `json:"value,omitempty"`
}

func NewClient() *Client {
	return NewClientWithRunner(ExecRunner{})
}

func NewClientWithRunner(runner CommandRunner) *Client {
	return &Client{runner: runner}
}

func (c *Client) Status(ctx context.Context, target string) (ScrollState, error) {
	output, err := c.runner.Output(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_in_mode}|#{scroll_position}|#{history_size}|#{pane_height}")
	if err != nil {
		return ScrollState{}, err
	}
	state, err := parseScrollState(string(output))
	if err != nil {
		return ScrollState{}, err
	}
	return state, nil
}

func (c *Client) Scroll(ctx context.Context, target string, request ScrollRequest) (ScrollState, error) {
	amount := request.Amount
	if amount <= 0 {
		amount = 1
	}

	switch request.Action {
	case "line-up":
		if err := c.copyMode(ctx, target); err != nil {
			return ScrollState{}, err
		}
		if err := c.sendCopyCommand(ctx, target, amount, "scroll-up"); err != nil {
			return ScrollState{}, err
		}
	case "line-down":
		if err := c.copyMode(ctx, target); err != nil {
			return ScrollState{}, err
		}
		if err := c.sendCopyCommand(ctx, target, amount, "scroll-down"); err != nil {
			return ScrollState{}, err
		}
	case "page-up":
		if err := c.copyMode(ctx, target); err != nil {
			return ScrollState{}, err
		}
		if err := c.sendCopyCommand(ctx, target, amount, "page-up"); err != nil {
			return ScrollState{}, err
		}
	case "page-down":
		if err := c.copyMode(ctx, target); err != nil {
			return ScrollState{}, err
		}
		if err := c.sendCopyCommand(ctx, target, amount, "page-down"); err != nil {
			return ScrollState{}, err
		}
	case "top":
		if err := c.copyMode(ctx, target); err != nil {
			return ScrollState{}, err
		}
		if err := c.sendCopyCommand(ctx, target, 1, "history-top"); err != nil {
			return ScrollState{}, err
		}
	case "bottom":
		if err := c.bottom(ctx, target); err != nil {
			return ScrollState{}, err
		}
	case "set":
		if err := c.setScrollTop(ctx, target, request.Value); err != nil {
			return ScrollState{}, err
		}
	default:
		return ScrollState{}, fmt.Errorf("unsupported scroll action %q", request.Action)
	}

	return c.Status(ctx, target)
}

func (c *Client) copyMode(ctx context.Context, target string) error {
	return c.runner.Run(ctx, "tmux", "copy-mode", "-t", target)
}

func (c *Client) bottom(ctx context.Context, target string) error {
	if err := c.copyMode(ctx, target); err != nil {
		return err
	}
	if err := c.sendCopyCommand(ctx, target, 1, "history-bottom"); err != nil {
		return err
	}
	return c.sendCopyCommand(ctx, target, 1, "cancel")
}

func (c *Client) setScrollTop(ctx context.Context, target string, value int) error {
	state, err := c.Status(ctx, target)
	if err != nil {
		return err
	}
	if value < 0 {
		value = 0
	}
	if value >= state.HistorySize {
		return c.bottom(ctx, target)
	}

	desiredPosition := state.HistorySize - value
	if desiredPosition <= 0 {
		return c.bottom(ctx, target)
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

func (c *Client) sendCopyCommand(ctx context.Context, target string, amount int, command string) error {
	if amount <= 1 {
		return c.runner.Run(ctx, "tmux", "send-keys", "-t", target, "-X", command)
	}
	return c.runner.Run(ctx, "tmux", "send-keys", "-t", target, "-X", "-N", strconv.Itoa(amount), command)
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

	return ScrollState{
		Position:    position,
		HistorySize: historySize,
		PaneHeight:  paneHeight,
		ScrollTop:   scrollTop,
		ScrollMax:   historySize,
		InCopyMode:  inCopyMode,
	}, nil
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}
