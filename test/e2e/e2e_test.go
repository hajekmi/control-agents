package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealTmuxAndTtydSessionAppears(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("e2e-%d", os.Getpid())
	stateDir := filepath.Join(root, ".cache", "e2e-"+sessionName)
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	port := freePort(t)
	app := exec.CommandContext(ctx, "go", "run", "../../cmd/server")
	app.Env = append(os.Environ(),
		"MIRROR_PASSWORD=secret",
		"MIRROR_BIND_ADDR=127.0.0.1",
		fmt.Sprintf("MIRROR_PORT=%d", port),
		"MIRROR_STATE_DIR="+stateDir,
	)
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()
	client := insecureHTTPSClient()
	waitForHTTP(t, ctx, client, fmt.Sprintf("https://127.0.0.1:%d/login", port))

	wrapper := exec.CommandContext(ctx, "../../bin/control-agents", sessionName)
	wrapper.Env = append(os.Environ(),
		"MIRROR_STATE_DIR="+stateDir,
		"MIRROR_NO_ATTACH=1",
		"MIRROR_WEB_SCROLLBACK_LINES=2345",
	)
	if output, err := wrapper.CombinedOutput(); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, output)
	}

	waitForSession(t, ctx, client, port, sessionName)
	assertTmuxWindowSize(t, sessionName, "largest")
	assertTmuxMouse(t, sessionName, "off")
	assertTmuxOption(t, sessionName, "status-left-length", "80")
	assertTmuxOption(t, sessionName, "status-left", "["+sessionName+"] ")
	assertTmuxOption(t, sessionName, "status-right", "#{pane_current_path}")
	assertTtydCommandLineContains(t, stateDir, sessionName, "scrollback", "2345")

	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/terminal/%s/", port, sessionName))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated terminal status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestClientDefaultsSessionNameToCurrentDirectory(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "default-session-dir")
	if err := os.Mkdir(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, ".cache", "e2e-default-session-"+fmt.Sprint(os.Getpid()))
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", "default-session-dir").Run()
	defer killRegisteredTtyd(stateDir, "default-session-dir")

	wrapper := exec.CommandContext(ctx, filepath.Join(root, "bin", "control-agents"))
	wrapper.Dir = workDir
	wrapper.Env = append(os.Environ(),
		"MIRROR_STATE_DIR="+stateDir,
		"MIRROR_NO_ATTACH=1",
	)
	output, err := wrapper.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "default-session-dir" {
		t.Fatalf("wrapper output = %q, want default-session-dir", got)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", "default-session-dir.json"))
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TmuxName string `json:"tmuxName"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.ID != "default-session-dir" || session.Name != "default-session-dir" || session.TmuxName != "default-session-dir" {
		t.Fatalf("session = %+v, want default-session-dir names", session)
	}
}

func assertTmuxWindowSize(t *testing.T, sessionName, want string) {
	t.Helper()
	cmd := exec.Command("tmux", "show-options", "-w", "-t", sessionName+":", "window-size")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(output))
	if got != "window-size "+want {
		t.Fatalf("tmux window-size = %q, want %q", got, "window-size "+want)
	}
}

func assertTmuxMouse(t *testing.T, sessionName, want string) {
	t.Helper()
	assertTmuxOption(t, sessionName, "mouse", want)
}

func assertTmuxOption(t *testing.T, sessionName, option, want string) {
	t.Helper()
	cmd := exec.Command("tmux", "show-options", "-v", "-t", sessionName, option)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(output), "\r\n")
	if got != want {
		t.Fatalf("tmux %s = %q, want %q", option, got, want)
	}
}

func killRegisteredTtyd(stateDir, sessionName string) {
	pid := readRegisteredTtydPID(stateDir, sessionName)
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func assertTtydCommandLineContains(t *testing.T, stateDir, sessionName string, wantParts ...string) {
	t.Helper()
	pid := readRegisteredTtydPID(stateDir, sessionName)
	if pid <= 0 {
		t.Fatalf("missing registered ttyd pid for %s", sessionName)
	}
	data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "cmdline"))
	if err != nil {
		t.Fatal(err)
	}
	cmdline := strings.ReplaceAll(string(data), "\x00", " ")
	for _, want := range wantParts {
		if !strings.Contains(cmdline, want) {
			t.Fatalf("ttyd cmdline %q does not contain %q", cmdline, want)
		}
	}
}

func readRegisteredTtydPID(stateDir, sessionName string) int {
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		return 0
	}
	var session struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &session) != nil || session.PID <= 0 {
		return 0
	}
	return session.PID
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHTTP(t *testing.T, ctx context.Context, client *http.Client, url string) {
	t.Helper()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			return
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForSession(t *testing.T, ctx context.Context, baseClient *http.Client, port int, sessionName string) {
	t.Helper()
	client := *baseClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	for {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		cookie := login(t, &client, port)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/api/sessions", port), nil)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err == nil {
			var payload struct {
				Sessions []struct {
					ID string `json:"id"`
				} `json:"sessions"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr == nil {
				for _, session := range payload.Sessions {
					if session.ID == sessionName {
						resp.Body.Close()
						return
					}
				}
			}
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func login(t *testing.T, client *http.Client, port int) *http.Cookie {
	t.Helper()
	body := strings.NewReader("password=secret")
	resp, err := client.Post(fmt.Sprintf("https://127.0.0.1:%d/login", port), "application/x-www-form-urlencoded", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "terminal_mirror_session" {
			return cookie
		}
	}
	scanner := bufio.NewScanner(resp.Body)
	if scanner.Scan() {
		t.Fatalf("missing auth cookie, response starts with %q", scanner.Text())
	}
	t.Fatal("missing auth cookie")
	return nil
}

func insecureHTTPSClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
