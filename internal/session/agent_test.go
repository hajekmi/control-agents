package session

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestForwardedAgentRefreshesStablePrivateLink(t *testing.T) {
	stateDir := filepath.Join(shortTempDir(t), "state")
	forwarded := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "forwarded.sock"))
	agent, err := NewForwardedAgent(stateDir)
	if err != nil {
		t.Fatal(err)
	}

	status, err := agent.RefreshFromEnvironment(environmentLookup(map[string]string{
		"SSH_CONNECTION": "192.0.2.10 54321 192.0.2.20 22",
		"SSH_AUTH_SOCK":  forwarded.Addr().String(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if status != ForwardedAgentAvailable {
		t.Fatalf("status = %v, want available", status)
	}
	target, err := os.Readlink(agent.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if target != forwarded.Addr().String() {
		t.Fatalf("stable link target = %q, want forwarded socket", target)
	}
	assertMode(t, agent.stateDir, 0o700)
	assertMode(t, agent.agentDir, 0o700)
}

func TestForwardedAgentAcceptsStandardSSHContextMetadata(t *testing.T) {
	for _, metadata := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Run(metadata, func(t *testing.T) {
			forwarded := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "forwarded.sock"))
			agent, err := NewForwardedAgent(filepath.Join(shortTempDir(t), "state"))
			if err != nil {
				t.Fatal(err)
			}
			status, err := agent.RefreshFromEnvironment(environmentLookup(map[string]string{
				metadata:        "present",
				"SSH_AUTH_SOCK": forwarded.Addr().String(),
			}))
			if err != nil || status != ForwardedAgentAvailable {
				t.Fatalf("status/error = %v/%v, want available", status, err)
			}
		})
	}
}

func TestForwardedAgentUnavailableOrInvalidInputKeepsPreviousLink(t *testing.T) {
	stateDir := filepath.Join(shortTempDir(t), "state")
	valid := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "valid.sock"))
	agent, err := NewForwardedAgent(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := agent.RefreshFromEnvironment(environmentLookup(map[string]string{
		"SSH_CONNECTION": "present",
		"SSH_AUTH_SOCK":  valid.Addr().String(),
	})); err != nil || status != ForwardedAgentAvailable {
		t.Fatalf("initial status/error = %v/%v", status, err)
	}
	regularFile := filepath.Join(shortTempDir(t), "regular")
	if err := os.WriteFile(regularFile, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	stableAlias := filepath.Join(shortTempDir(t), "stable-alias.sock")
	stableAliasIntermediate := filepath.Join(shortTempDir(t), "stable-alias-intermediate.sock")
	if err := os.Symlink(agent.socketPath, stableAliasIntermediate); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stableAliasIntermediate, stableAlias); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		env    map[string]string
		status ForwardedAgentStatus
	}{
		{name: "not SSH", env: map[string]string{"SSH_AUTH_SOCK": valid.Addr().String()}, status: ForwardedAgentUnavailable},
		{name: "missing socket variable", env: map[string]string{"SSH_CONNECTION": "present"}, status: ForwardedAgentUnavailable},
		{name: "missing path", env: map[string]string{"SSH_CONNECTION": "present", "SSH_AUTH_SOCK": filepath.Join(shortTempDir(t), "missing")}, status: ForwardedAgentInvalid},
		{name: "regular file", env: map[string]string{"SSH_CONNECTION": "present", "SSH_AUTH_SOCK": regularFile}, status: ForwardedAgentInvalid},
		{name: "directory", env: map[string]string{"SSH_CONNECTION": "present", "SSH_AUTH_SOCK": shortTempDir(t)}, status: ForwardedAgentInvalid},
		{name: "stable link itself", env: map[string]string{"SSH_CONNECTION": "present", "SSH_AUTH_SOCK": agent.socketPath}, status: ForwardedAgentInvalid},
		{name: "alias chain through stable link", env: map[string]string{"SSH_CONNECTION": "present", "SSH_AUTH_SOCK": stableAlias}, status: ForwardedAgentInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, err := agent.RefreshFromEnvironment(environmentLookup(test.env))
			if err != nil {
				t.Fatal(err)
			}
			if status != test.status {
				t.Fatalf("status = %v, want %v", status, test.status)
			}
			target, err := os.Readlink(agent.socketPath)
			if err != nil {
				t.Fatal(err)
			}
			if target != valid.Addr().String() {
				t.Fatalf("invalid refresh replaced link with %q", target)
			}
		})
	}
}

func TestForwardedAgentRetargetRestoresStableSocketReachability(t *testing.T) {
	agent, err := NewForwardedAgent(filepath.Join(shortTempDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	first := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "first.sock"))
	refreshTestAgent(t, agent, first.Addr().String())
	assertSocketAcceptsThrough(t, first, agent.socketPath)

	second := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "second.sock"))
	refreshTestAgent(t, agent, second.Addr().String())
	assertSocketAcceptsThrough(t, second, agent.socketPath)
}

func TestConcurrentForwardedAgentRefreshAlwaysLeavesValidLink(t *testing.T) {
	agent, err := NewForwardedAgent(filepath.Join(shortTempDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	const refreshes = 24
	listeners := make([]net.Listener, 0, refreshes)
	validTargets := make(map[string]bool, refreshes)
	for index := 0; index < refreshes; index++ {
		listener := newTestUnixSocket(t, filepath.Join(shortTempDir(t), "forwarded.sock"))
		listeners = append(listeners, listener)
		validTargets[listener.Addr().String()] = true
	}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, refreshes)
	for _, listener := range listeners {
		wait.Add(1)
		go func(target string) {
			defer wait.Done()
			status, err := agent.RefreshFromEnvironment(environmentLookup(map[string]string{
				"SSH_CONNECTION": "present",
				"SSH_AUTH_SOCK":  target,
			}))
			if err == nil && status != ForwardedAgentAvailable {
				err = errors.New("refresh did not report availability")
			}
			errorsChannel <- err
		}(listener.Addr().String())
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	target, err := os.Readlink(agent.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if !validTargets[target] {
		t.Fatalf("final link target = %q, want one of the valid sockets", target)
	}
	if info, err := os.Stat(agent.socketPath); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("final stable link is not a reachable socket: info=%v err=%v", info, err)
	}
}

func refreshTestAgent(t *testing.T, agent *ForwardedAgent, target string) {
	t.Helper()
	status, err := agent.RefreshFromEnvironment(environmentLookup(map[string]string{
		"SSH_CONNECTION": "present",
		"SSH_AUTH_SOCK":  target,
	}))
	if err != nil || status != ForwardedAgentAvailable {
		t.Fatalf("refresh status/error = %v/%v", status, err)
	}
}

func assertSocketAcceptsThrough(t *testing.T, listener net.Listener, path string) {
	t.Helper()
	accepted := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			err = connection.Close()
		}
		accepted <- err
	}()
	connection, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("forwarded socket did not accept through stable link")
	}
}

func newTestUnixSocket(t *testing.T, path string) net.Listener {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "ca-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func environmentLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
