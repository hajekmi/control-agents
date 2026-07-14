package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type historyE2EPage struct {
	SnapshotID      string `json:"snapshotId"`
	Mode            string `json:"mode"`
	Columns         int    `json:"columns"`
	AlternateScreen bool   `json:"alternateScreen"`
	Lines           []struct {
		Runs []struct {
			Text string `json:"text"`
		} `json:"runs"`
	} `json:"lines"`
}

func TestHistoryRealTmuxFixturesPaneGenerationAndSSHIsolation(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real History tmux fixtures")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")
	requireCommand(t, "script")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName, stateDir := compactRealProcessFixturePaths(t, root, "hf")
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(stateDir, "history-command-trace.log")
	shimDir := installHistoryTmuxTraceShim(t, stateDir)
	port := freePort(t)
	app := exec.CommandContext(ctx, "go", "run", "../../cmd/server")
	app.Env = append(os.Environ(),
		"HOME="+homeDir,
		"CONTROL_AGENTS_PASSWORD=secret",
		"CONTROL_AGENTS_BIND_ADDR=127.0.0.1",
		fmt.Sprintf("CONTROL_AGENTS_PORT=%d", port),
		"CONTROL_AGENTS_STATE_DIR="+stateDir,
		"CONTROL_AGENTS_TEST_REAL_TMUX="+realTmux,
		"CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG="+tracePath,
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()
	client := insecureHTTPSClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port)
	waitForHTTP(t, ctx, client, baseURL+"/login")
	cookie := login(t, client, port)

	created := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions", `{"name":"`+sessionName+`"}`)
	if created.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(created.Body)
		created.Body.Close()
		t.Fatalf("create status/body = %d/%q; log=%s", created.StatusCode, body, appLog.String())
	}
	var createPayload struct {
		Session map[string]any `json:"session"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createPayload); err != nil {
		created.Body.Close()
		t.Fatal(err)
	}
	created.Body.Close()
	sessionRef, _ := createPayload.Session["id"].(string)
	paneRef, _ := createPayload.Session["activePaneRef"].(string)
	if sessionRef == "" || paneRef == "" {
		t.Fatalf("create payload = %#v", createPayload)
	}

	historyReady := fmt.Sprintf("control-agents-history-%d", os.Getpid())
	fillCommand := `seq -f 'h%05.0f value' 1 60000; printf 'tabs\tvalue   \n'; tmux wait-for -S ` + historyReady
	mustTmuxRun(t, "send-keys", "-t", sessionName, fillCommand, "C-m")
	mustTmuxRunContext(t, ctx, "wait-for", historyReady)
	waitForTmuxHistoryAtLeast(t, ctx, sessionName, 49900)
	if got := tmuxFormatInt(t, sessionName, "#{history_size}"); got < 49900 || got > 50000 {
		t.Fatalf("rollover history size = %d, want the bounded 50,000-line ring", got)
	}
	assertTmuxPaneHistoryLimit(t, sessionName, 50000)
	rolled := string(mustTmuxOutput(t, "capture-pane", "-p", "-S", "-", "-t", sessionName))
	if strings.Contains(rolled, "h00001") || !strings.Contains(rolled, "h60000") || !strings.Contains(rolled, "tabs") {
		t.Fatal("50,000-line rollover did not discard the oldest line and retain the newest line")
	}
	whitespaceViewer := "viewer-550e8400-e29b-41d4-a716-446655440003"
	whitespace := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, whitespaceViewer, "reflow")
	if !historyE2EHasExactLine(whitespace, "tabs\tvalue   ") {
		t.Fatal("History did not preserve the exact tab and trailing-space fixture line")
	}
	deleteHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, whitespace.SnapshotID, whitespaceViewer)

	directClient := exec.CommandContext(ctx, "script", "-qefc", fmt.Sprintf("%q %q", filepath.Join(root, "bin", "control-agents"), sessionName), "/dev/null")
	directClient.Env = environmentWith(map[string]string{"CONTROL_AGENTS_STATE_DIR": stateDir, "HOME": homeDir})
	var directTranscript lockedBuffer
	directClient.Stdout = &directTranscript
	directClient.Stderr = &directTranscript
	directInput, err := directClient.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	clientNamesBefore := tmuxClientNames(t, sessionName)
	clientCount := len(clientNamesBefore)
	if err := directClient.Start(); err != nil {
		t.Fatal(err)
	}
	directExit := make(chan error, 1)
	go func() { directExit <- directClient.Wait() }()
	waitForTranscriptCount(t, ctx, &directTranscript, "Detach with Ctrl-b d.", 1)
	waitForTmuxClientOrExit(t, ctx, sessionName, clientCount+1, directExit, &directTranscript)
	sshClientName := newTmuxClientName(t, sessionName, clientNamesBefore)
	t.Cleanup(func() {
		_ = directInput.Close()
		if directClient.Process != nil && processAlive(directClient.Process.Pid) {
			_ = directClient.Process.Kill()
		}
	})

	viewerOne := "viewer-550e8400-e29b-41d4-a716-446655440001"
	viewerTwo := "viewer-550e8400-e29b-41d4-a716-446655440002"
	shellCommand := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{pane_current_command}")))
	echoReady := fmt.Sprintf("control-agents-history-echo-%d", os.Getpid())
	mustTmuxRun(t, "send-keys", "-t", sessionName, "stty -echo; tmux wait-for -S "+echoReady, "C-m")
	mustTmuxRunContext(t, ctx, "wait-for", echoReady)
	for _, width := range []int{80, 120, 240} {
		mustTmuxRun(t, "resize-window", "-t", sessionName+":", "-x", fmt.Sprint(width), "-y", "32")
		wrapText := fmt.Sprintf("WRAP_%03d_BEGIN:%s:WRAP_%03d_END", width, strings.Repeat("W", 300), width)
		wrapReady := fmt.Sprintf("control-agents-history-wrap-%d-%d", os.Getpid(), width)
		wrapCommand := "printf '\\033[2J\\033[H" + wrapText + "\\n'; tmux wait-for -S " + wrapReady + "; sleep 10"
		mustTmuxRun(t, "send-keys", "-t", sessionName, wrapCommand, "C-m")
		mustTmuxRunContext(t, ctx, "wait-for", wrapReady)
		page := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, viewerOne, "reflow")
		if page.Columns != width || !historyE2EHasExactLine(page, wrapText) {
			t.Fatalf("%d-column wrap snapshot did not preserve its exact marked output line at columns=%d", width, page.Columns)
		}
		deleteHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, page.SnapshotID, viewerOne)
		mustTmuxRun(t, "send-keys", "-t", sessionName, "C-c")
		waitForTmuxFormatWithin(t, ctx, sessionName, "#{pane_current_command}", shellCommand, 5*time.Second)
	}
	mustTmuxRun(t, "send-keys", "-t", sessionName, "stty echo", "C-m")

	alternateCommand := `printf '\033[?1049h\033[2J'; i=1; while [ "$i" -le 250 ]; do printf '\033[Hgrid-%03d' "$i"; i=$((i+1)); done; sleep 2; printf '\033[?1049l'`
	mustTmuxRun(t, "send-keys", "-t", sessionName, alternateCommand, "C-m")
	waitForTmuxFormat(t, ctx, sessionName, "#{alternate_on}", "1")
	alternate := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, viewerOne, "reflow")
	if !alternate.AlternateScreen || alternate.Mode != "fixed" || !strings.Contains(historyE2EText(alternate), "grid-") {
		t.Fatalf("alternate redraw snapshot = mode %q alternate %v", alternate.Mode, alternate.AlternateScreen)
	}
	deleteHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, alternate.SnapshotID, viewerOne)
	waitForTmuxFormat(t, ctx, sessionName, "#{alternate_on}", "0")

	exerciseOptionalFullscreenHistory(t, ctx, client, cookie, baseURL, sessionName, paneRef, shellCommand)

	beforePaneMode := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{pane_in_mode}")))
	beforeWindowPane := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{window_id}|#{pane_id}")))
	beforeSize := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{window_width}|#{window_height}")))
	beforeSSHState := tmuxClientDisplay(t, sshClientName)
	firstViewer := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, viewerOne, "fixed")
	secondViewer := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, viewerTwo, "fixed")
	if firstViewer.SnapshotID == secondViewer.SnapshotID || historyE2EText(firstViewer) != historyE2EText(secondViewer) {
		t.Fatal("two browser viewers did not receive isolated snapshots of the same pane generation")
	}
	if got := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{pane_in_mode}"))); got != beforePaneMode {
		t.Fatalf("History changed pane_in_mode from %q to %q", beforePaneMode, got)
	}
	if got := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{window_id}|#{pane_id}"))); got != beforeWindowPane {
		t.Fatalf("History changed active window/pane from %q to %q", beforeWindowPane, got)
	}
	if got := strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-t", sessionName, "#{window_width}|#{window_height}"))); got != beforeSize {
		t.Fatalf("History changed tmux window size from %q to %q", beforeSize, got)
	}
	if got := tmuxClientDisplay(t, sshClientName); got != beforeSSHState {
		t.Fatalf("History changed SSH client window/pane/viewport from %q to %q", beforeSSHState, got)
	}
	if trace, err := os.ReadFile(tracePath); err != nil {
		t.Fatal(err)
	} else if historyTraceHasForbiddenMutation(string(trace)) {
		t.Fatalf("History emitted a forbidden tmux command trace: %q", trace)
	}
	deleteHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, secondViewer.SnapshotID, viewerTwo)

	oldSnapshotID := firstViewer.SnapshotID
	oldPaneID := strings.Split(beforeWindowPane, "|")[1]
	mustTmuxRun(t, "split-window", "-d", "-t", sessionName)
	mustTmuxRun(t, "kill-pane", "-t", oldPaneID)
	refreshed := fetchPublicSessionByName(t, ctx, client, cookie, baseURL, sessionName)
	newPaneRef, _ := refreshed["activePaneRef"].(string)
	if newPaneRef == "" || newPaneRef == paneRef {
		t.Fatalf("pane recreation did not rotate the opaque pane ref: old=%q new=%q", paneRef, newPaneRef)
	}
	stale := historyE2ERequest(t, ctx, client, cookie, http.MethodGet, baseURL+"/api/v1/history-snapshots/"+oldSnapshotID+"/pages", "", viewerOne)
	stale.Body.Close()
	if stale.StatusCode != http.StatusGone {
		t.Fatalf("stale pane snapshot status = %d, want 410", stale.StatusCode)
	}

	newSnapshot := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, newPaneRef, viewerTwo, "reflow")
	mustTmuxRun(t, "kill-server")
	afterRestart := historyE2ERequest(t, ctx, client, cookie, http.MethodGet, baseURL+"/api/v1/history-snapshots/"+newSnapshot.SnapshotID+"/pages", "", viewerTwo)
	afterRestart.Body.Close()
	if afterRestart.StatusCode != http.StatusGone {
		t.Fatalf("tmux-server restart snapshot status = %d, want 410", afterRestart.StatusCode)
	}
	select {
	case <-directExit:
	case <-ctx.Done():
		t.Fatalf("attached SSH-style client did not exit after tmux server restart: %v", ctx.Err())
	}
}

func installHistoryTmuxTraceShim(t *testing.T, stateDir string) string {
	t.Helper()
	directory := filepath.Join(stateDir, "test-bin")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(directory, "tmux")
	script := `#!/bin/sh
marker=""
for argument in "$@"; do
  case "$argument" in
    copy-mode|resize-window|send-keys|refresh-client|-X|-U|-D|-L|-R) marker="$marker $argument" ;;
  esac
done
if [ -n "${CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG:-}" ]; then
  printf '%s\n' "$marker" >> "$CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG"
fi
exec "$CONTROL_AGENTS_TEST_REAL_TMUX" "$@"
`
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func createHistoryE2ESnapshot(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, paneRef, viewer, mode string) historyE2EPage {
	t.Helper()
	response := historyE2ERequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/v1/panes/"+paneRef+"/history-snapshots", `{"mode":"`+mode+`"}`, viewer)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("History create status/body = %d/%q", response.StatusCode, body)
	}
	var page historyE2EPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	return page
}

func deleteHistoryE2ESnapshot(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, snapshotID, viewer string) {
	t.Helper()
	response := historyE2ERequest(t, ctx, client, cookie, http.MethodDelete, baseURL+"/api/v1/history-snapshots/"+snapshotID, "", viewer)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("History delete status = %d", response.StatusCode)
	}
}

func historyE2ERequest(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, method, url, body, viewer string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Control-Agents-Viewer-ID", viewer)
	request.AddCookie(cookie)
	if method == http.MethodPost || method == http.MethodDelete {
		origin := request.URL.Scheme + "://" + request.URL.Host
		request.Header.Set("Origin", origin)
		request.Header.Set("X-Control-Agents-CSRF-Token", fetchCSRFToken(t, ctx, client, cookie, origin))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func historyE2EText(page historyE2EPage) string {
	var text strings.Builder
	for _, line := range page.Lines {
		for _, run := range line.Runs {
			text.WriteString(run.Text)
		}
		text.WriteByte('\n')
	}
	return text.String()
}

func historyE2EHasExactLine(page historyE2EPage, expected string) bool {
	for _, line := range page.Lines {
		var text strings.Builder
		for _, run := range line.Runs {
			text.WriteString(run.Text)
		}
		if text.String() == expected {
			return true
		}
	}
	return false
}

func exerciseOptionalFullscreenHistory(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, sessionName, paneRef, shellCommand string) {
	t.Helper()
	fixtureDir := t.TempDir()
	textPath := filepath.Join(fixtureDir, "fullscreen-smoke.txt")
	var textFixture strings.Builder
	for index := 1; index <= 200; index++ {
		fmt.Fprintf(&textFixture, "fullscreen-smoke-line-%03d\n", index)
	}
	if err := os.WriteFile(textPath, []byte(textFixture.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	pcapPath := writeHistorySIPPCAP(t, fixtureDir)

	type applicationSpec struct {
		name       string
		command    string
		process    string
		exitKeys   []string
		wantScreen string
		alternate  bool
	}
	applications := []applicationSpec{
		{name: "mc", command: "LC_ALL=C mc -u", process: "mc", exitKeys: []string{"F10"}, wantScreen: "Command", alternate: true},
		{name: "sngrep", command: "LC_ALL=C sngrep -F -I " + shellSingleQuote(pcapPath), process: "sngrep", exitKeys: []string{"q", "C-m"}, wantScreen: "sngrep", alternate: true},
		{name: "vim", command: "LC_ALL=C vim -Nu NONE -n " + shellSingleQuote(textPath), process: "vim", exitKeys: []string{"Escape", ":qa!", "C-m"}, wantScreen: "fullscreen-smoke-line-001", alternate: true},
		{name: "less", command: "LC_ALL=C less " + shellSingleQuote(textPath), process: "less", exitKeys: []string{"q"}, wantScreen: "fullscreen-smoke-line-001", alternate: true},
		{name: "top", command: "LC_ALL=C top -d 60", process: "top", exitKeys: []string{"q"}, wantScreen: "PID"},
	}
	for index, application := range applications {
		if _, err := exec.LookPath(application.name); err != nil {
			t.Logf("optional_fullscreen application=%s status=unsupported_dependency", application.name)
			continue
		}
		mustTmuxRun(t, "send-keys", "-t", sessionName, application.command, "C-m")
		applicationRunning := true
		exitArgs := append([]string{"send-keys", "-t", sessionName}, application.exitKeys...)
		defer func() {
			if applicationRunning {
				_ = exec.Command("tmux", exitArgs...).Run()
			}
		}()
		waitForTmuxFormatWithin(t, ctx, sessionName, "#{pane_current_command}", application.process, 5*time.Second)
		wantAlternate := "0"
		wantMode := "reflow"
		if application.alternate {
			wantAlternate = "1"
			wantMode = "fixed"
		}
		waitForTmuxFormatWithin(t, ctx, sessionName, "#{alternate_on}", wantAlternate, 5*time.Second)
		waitForTmuxScreenContainsWithin(t, ctx, sessionName, application.wantScreen, 5*time.Second)
		viewer := fmt.Sprintf("viewer-550e8400-e29b-41d4-a716-4466554401%02d", index)
		page := createHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, paneRef, viewer, "reflow")
		if page.AlternateScreen != application.alternate || page.Mode != wantMode || !strings.Contains(historyE2EText(page), application.wantScreen) {
			t.Fatalf("optional full-screen History smoke failed for %s: mode=%q alternate=%v expected-screen-content=%v", application.name, page.Mode, page.AlternateScreen, strings.Contains(historyE2EText(page), application.wantScreen))
		}
		deleteHistoryE2ESnapshot(t, ctx, client, cookie, baseURL, page.SnapshotID, viewer)
		mustTmuxRun(t, exitArgs...)
		waitForTmuxFormatWithin(t, ctx, sessionName, "#{alternate_on}", "0", 5*time.Second)
		waitForTmuxFormatWithin(t, ctx, sessionName, "#{pane_current_command}", shellCommand, 5*time.Second)
		applicationRunning = false
		t.Logf("optional_fullscreen application=%s status=passed", application.name)
	}
}

func writeHistorySIPPCAP(t *testing.T, directory string) string {
	t.Helper()
	payload := []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-smoke\r\n" +
		"From: <sip:alice@example.com>;tag=1\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Call-ID: smoke@example.com\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:alice@192.0.2.1>\r\n" +
		"Content-Length: 0\r\n\r\n")
	ipv4 := make([]byte, 20)
	ipv4[0] = 0x45
	binary.BigEndian.PutUint16(ipv4[2:4], uint16(len(ipv4)+8+len(payload)))
	ipv4[8] = 64
	ipv4[9] = 17
	copy(ipv4[12:16], []byte{192, 0, 2, 1})
	copy(ipv4[16:20], []byte{198, 51, 100, 2})
	binary.BigEndian.PutUint16(ipv4[10:12], historyIPv4Checksum(ipv4))
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:2], 5060)
	binary.BigEndian.PutUint16(udp[2:4], 5060)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)+len(payload)))
	packet := append([]byte{0, 0, 0x5e, 0, 0x53, 1, 0, 0, 0x5e, 0, 0x53, 2, 0x08, 0x00}, ipv4...)
	packet = append(packet, udp...)
	packet = append(packet, payload...)

	var capture bytes.Buffer
	for _, value := range []any{
		uint32(0xa1b2c3d4), uint16(2), uint16(4), int32(0), uint32(0), uint32(65535), uint32(1),
		uint32(1), uint32(0), uint32(len(packet)), uint32(len(packet)),
	} {
		if err := binary.Write(&capture, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	capture.Write(packet)
	path := filepath.Join(directory, "fullscreen-smoke.pcap")
	if err := os.WriteFile(path, capture.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func historyIPv4Checksum(header []byte) uint16 {
	var sum uint32
	for index := 0; index+1 < len(header); index += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[index : index+2]))
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func historyTraceHasForbiddenMutation(trace string) bool {
	for _, line := range strings.Split(trace, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			switch field {
			case "copy-mode", "resize-window", "send-keys", "-X":
				return true
			case "refresh-client":
				for _, flag := range fields[index+1:] {
					if flag == "-U" || flag == "-D" || flag == "-L" || flag == "-R" {
						return true
					}
				}
			}
		}
	}
	return false
}

func waitForTmuxFormat(t *testing.T, ctx context.Context, target, format, want string) {
	t.Helper()
	for {
		output, err := exec.Command("tmux", "display-message", "-p", "-t", target, format).Output()
		if err == nil && strings.TrimSpace(string(output)) == want {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("tmux format %q did not become %q: %v", format, want, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForTmuxFormatWithin(t *testing.T, parent context.Context, target, format, want string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	waitForTmuxFormat(t, ctx, target, format, want)
}

func waitForTmuxScreenContainsWithin(t *testing.T, parent context.Context, target, want string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	for {
		output, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", target).Output()
		if err == nil && strings.Contains(string(output), want) {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("tmux screen for %q did not contain its expected smoke marker: %v", target, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func tmuxClientNames(t *testing.T, sessionName string) map[string]bool {
	t.Helper()
	output := mustTmuxOutput(t, "list-clients", "-t", sessionName, "-F", "#{client_name}")
	names := make(map[string]bool)
	for _, name := range strings.Fields(string(output)) {
		names[name] = true
	}
	return names
}

func newTmuxClientName(t *testing.T, sessionName string, before map[string]bool) string {
	t.Helper()
	for name := range tmuxClientNames(t, sessionName) {
		if !before[name] {
			return name
		}
	}
	t.Fatal("attached SSH-style tmux client was not identifiable")
	return ""
}

func tmuxClientDisplay(t *testing.T, clientName string) string {
	t.Helper()
	return strings.TrimSpace(string(mustTmuxOutput(t, "display-message", "-p", "-c", clientName,
		"#{window_id}|#{pane_id}|#{client_width}|#{client_height}|#{window_width}|#{window_height}|#{window_offset_x}|#{window_offset_y}")))
}

func mustTmuxRun(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func mustTmuxRunContext(t *testing.T, ctx context.Context, args ...string) {
	t.Helper()
	if output, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput(); err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
