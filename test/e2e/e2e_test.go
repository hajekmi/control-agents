package e2e

import (
	"bufio"
	"bytes"
	"context"
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
	waitForHTTP(t, ctx, fmt.Sprintf("http://127.0.0.1:%d/login", port))

	wrapper := exec.CommandContext(ctx, "../../bin/client_mirror", sessionName)
	wrapper.Env = append(os.Environ(), "MIRROR_STATE_DIR="+stateDir, "MIRROR_NO_ATTACH=1")
	if output, err := wrapper.CombinedOutput(); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, output)
	}

	waitForSession(t, ctx, port, sessionName)
	assertTmuxWindowSize(t, sessionName, "largest")

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/terminal/%s/", port, sessionName))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated terminal status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
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

func killRegisteredTtyd(stateDir, sessionName string) {
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		return
	}
	var session struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &session) != nil || session.PID <= 0 {
		return
	}
	process, err := os.FindProcess(session.PID)
	if err == nil {
		_ = process.Kill()
	}
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

func waitForHTTP(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
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

func waitForSession(t *testing.T, ctx context.Context, port int, sessionName string) {
	t.Helper()
	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		cookie := login(t, &client, port)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/sessions", port), nil)
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
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/login", port), "application/x-www-form-urlencoded", body)
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
