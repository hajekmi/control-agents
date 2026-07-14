package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	forwardedAgentDirectory = "agent"
	forwardedAgentSocket    = "forwarded.sock"
)

// ForwardedAgentStatus describes whether an invocation refreshed the stable
// managed-session SSH agent socket.
type ForwardedAgentStatus int

const (
	ForwardedAgentUnavailable ForwardedAgentStatus = iota
	ForwardedAgentInvalid
	ForwardedAgentAvailable
)

// ForwardedAgent maintains the private stable socket path shared by managed
// sessions. It stores only a symlink to the currently forwarded socket.
type ForwardedAgent struct {
	stateDir   string
	agentDir   string
	socketPath string
}

// NewForwardedAgent prepares a forwarded-agent helper for stateDir.
func NewForwardedAgent(stateDir string) (*ForwardedAgent, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("forwarded agent state directory cannot be empty")
	}
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve forwarded agent state directory: %w", err)
	}
	agentDir := filepath.Join(absolute, forwardedAgentDirectory)
	return &ForwardedAgent{
		stateDir:   absolute,
		agentDir:   agentDir,
		socketPath: filepath.Join(agentDir, forwardedAgentSocket),
	}, nil
}

// ForwardedAgentSocketPath returns the stable SSH_AUTH_SOCK used by managed
// tmux sessions. The path may not exist while no SSH connection is forwarding
// an agent.
func ForwardedAgentSocketPath(stateDir string) (string, error) {
	forwarded, err := NewForwardedAgent(stateDir)
	if err != nil {
		return "", err
	}
	return forwarded.socketPath, nil
}

// RefreshFromEnvironment atomically retargets the stable link when this is an
// SSH invocation and SSH_AUTH_SOCK names an existing Unix socket. Invalid or
// unavailable input leaves the previous link untouched.
func (f *ForwardedAgent) RefreshFromEnvironment(lookupEnv func(string) (string, bool)) (ForwardedAgentStatus, error) {
	if !sshInvocation(lookupEnv) {
		return ForwardedAgentUnavailable, nil
	}
	target, ok := lookupEnv("SSH_AUTH_SOCK")
	if !ok || strings.TrimSpace(target) == "" {
		return ForwardedAgentUnavailable, nil
	}
	targetPath, err := filepath.Abs(target)
	if err != nil || targetPath == f.socketPath {
		return ForwardedAgentInvalid, nil
	}
	referencesStableLink, err := symlinkChainReferences(targetPath, f.socketPath)
	if err != nil || referencesStableLink {
		return ForwardedAgentInvalid, nil
	}
	info, err := os.Stat(targetPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ForwardedAgentInvalid, nil
	}
	if err := f.ensurePrivateDirectories(); err != nil {
		return ForwardedAgentInvalid, err
	}
	if err := replaceSymlink(targetPath, f.socketPath); err != nil {
		return ForwardedAgentInvalid, err
	}
	return ForwardedAgentAvailable, nil
}

func (f *ForwardedAgent) ensurePrivateDirectories() error {
	for _, directory := range []string{f.stateDir, f.agentDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create private forwarded agent directory: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("set private forwarded agent directory permissions: %w", err)
		}
	}
	return nil
}

func sshInvocation(lookupEnv func(string) (string, bool)) bool {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if value, ok := lookupEnv(name); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

// symlinkChainReferences reports whether resolving path would pass through
// link. Parent directories are resolved at every step so aliases of the
// stable link's directory cannot hide an indirect self-reference.
func symlinkChainReferences(path, link string) (bool, error) {
	current := filepath.Clean(path)
	link = filepath.Clean(link)
	if resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(link)); err == nil {
		link = filepath.Join(resolvedDirectory, filepath.Base(link))
	}
	visited := make(map[string]struct{})

	for {
		resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(current))
		if err != nil {
			return false, err
		}
		current = filepath.Join(resolvedDirectory, filepath.Base(current))
		if current == link {
			return true, nil
		}
		if _, ok := visited[current]; ok {
			return false, errors.New("symlink cycle")
		}
		visited[current] = struct{}{}

		info, err := os.Lstat(current)
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		target, err := os.Readlink(current)
		if err != nil {
			return false, err
		}
		if filepath.IsAbs(target) {
			current = filepath.Clean(target)
		} else {
			current = filepath.Clean(filepath.Join(filepath.Dir(current), target))
		}
	}
}

func replaceSymlink(target, link string) error {
	temporary, err := os.CreateTemp(filepath.Dir(link), ".forwarded.sock-*")
	if err != nil {
		return fmt.Errorf("reserve temporary forwarded agent link: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary forwarded agent link: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("prepare temporary forwarded agent link: %w", err)
	}
	defer os.Remove(temporaryPath)
	if err := os.Symlink(target, temporaryPath); err != nil {
		return fmt.Errorf("create temporary forwarded agent link: %w", err)
	}
	if err := os.Rename(temporaryPath, link); err != nil {
		return fmt.Errorf("replace forwarded agent link: %w", err)
	}
	return nil
}
