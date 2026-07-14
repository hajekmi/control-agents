package install_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerSelectsMatchingClientAndServerAssets(t *testing.T) {
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is required for installer integration tests")
	}
	for _, test := range []struct {
		name     string
		machine  string
		platform string
	}{
		{name: "amd64", machine: "x86_64", platform: "linux-amd64"},
		{name: "arm64", machine: "aarch64", platform: "linux-arm64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runInstaller(t, test.machine, test.platform, false)
			if result.err != nil {
				t.Fatalf("installer failed: %v\n%s", result.err, result.output)
			}
			assertFileContents(t, filepath.Join(result.prefix, "bin", "control-agents-server"), "server-"+test.platform)
			assertFileContents(t, filepath.Join(result.prefix, "bin", "control-agents"), "client-"+test.platform)
			if !strings.Contains(result.output, "  control-agents\n\n") {
				t.Fatalf("installer output does not lead to the selector workflow:\n%s", result.output)
			}
			if strings.Contains(result.output, "control-agents main") {
				t.Fatalf("installer output still requires a named session:\n%s", result.output)
			}
			service := filepath.Join(result.configHome, "systemd", "user", "control-agents.service")
			data, err := os.ReadFile(service)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "UMask=0077") || !strings.Contains(string(data), result.prefix+"/bin/control-agents-server") {
				t.Fatalf("generated service is not private or does not use the matching installed server:\n%s", data)
			}
			for _, directive := range []string{
				"NoNewPrivileges=true", "PrivateTmp=false", "ProtectSystem=full",
				"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "LimitCORE=0",
				"EnvironmentFile=" + filepath.Join(result.configHome, "control-agents", "env"),
				"ExecStart=/usr/bin/env PATH=" + filepath.Join(result.prefix, "bin") + ":",
				" LANG=C.UTF-8 LC_ALL=C.UTF-8 " + filepath.Join(result.prefix, "bin", "control-agents-server"),
			} {
				if !strings.Contains(string(data), directive) {
					t.Fatalf("generated service lacks %s:\n%s", directive, data)
				}
			}
		})
	}
}

func TestInstallerRejectsUbuntuTmuxBeforeWritingControlAgentsFiles(t *testing.T) {
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is required for installer integration tests")
	}
	result := runInstallerWithTmuxVersion(t, "x86_64", "linux-amd64", false, "tmux 3.4")
	if result.err == nil {
		t.Fatalf("installer accepted Ubuntu tmux 3.4:\n%s", result.output)
	}
	if !strings.Contains(result.output, "tmux 3.7b is required") {
		t.Fatalf("installer did not identify the incompatible tmux version:\n%s", result.output)
	}
	for _, path := range []string{
		filepath.Join(result.prefix, "bin", "control-agents-server"),
		filepath.Join(result.prefix, "bin", "control-agents"),
		filepath.Join(result.configHome, "control-agents", "env"),
		filepath.Join(result.configHome, "systemd", "user", "control-agents.service"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("installer wrote %s before tmux verification failed: %v", path, err)
		}
	}
}

func TestInstalledServiceOverridesConflictingOperatorEnvironment(t *testing.T) {
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is required for installer integration tests")
	}
	operatorEnvironment := "CONTROL_AGENTS_PASSWORD=preserved\nPATH=/usr/bin:/bin\nLANG=C\nLC_ALL=C\n"
	result := runInstallerWithEnvironment(t, "x86_64", "linux-amd64", false, "tmux 3.7b", operatorEnvironment)
	if result.err != nil {
		t.Fatalf("installer failed: %v\n%s", result.err, result.output)
	}
	environmentPath := filepath.Join(result.configHome, "control-agents", "env")
	if got := readTextFile(t, environmentPath); got != operatorEnvironment {
		t.Fatalf("operator environment was rewritten:\n%s", got)
	}

	probeServer := filepath.Join(result.prefix, "bin", "control-agents-server")
	writeExecutable(t, probeServer, `#!/bin/sh
printf '%s\n' "$PATH" "$LANG" "$LC_ALL" "$(command -v tmux)" "$(tmux -V)"
`)
	servicePath := filepath.Join(result.configHome, "systemd", "user", "control-agents.service")
	service := readTextFile(t, servicePath)
	execStart := serviceDirective(t, service, "ExecStart=")
	arguments := strings.Fields(strings.TrimPrefix(execStart, "ExecStart="))
	if len(arguments) == 0 {
		t.Fatalf("generated service has an empty ExecStart: %s", service)
	}
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Env = []string{
		"HOME=" + filepath.Join(result.prefix, "operator-home"),
		"PATH=/usr/bin:/bin",
		"LANG=C",
		"LC_ALL=C",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("service command failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	wantPath := filepath.Join(result.prefix, "bin") + ":/home/linuxbrew/.linuxbrew/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	want := []string{wantPath, "C.UTF-8", "C.UTF-8", filepath.Join(result.prefix, "bin", "tmux"), "tmux 3.7b"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("service runtime environment = %q, want %q", lines, want)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	committed := readTextFile(t, filepath.Join(root, "systemd", "user", "control-agents.service"))
	committedExec := serviceDirective(t, committed, "ExecStart=")
	for _, required := range []string{"ExecStart=/usr/bin/env PATH=%h/.local/bin:", " LANG=C.UTF-8 LC_ALL=C.UTF-8 %h/.local/bin/control-agents-server"} {
		if !strings.Contains(committedExec, required) {
			t.Fatalf("committed service does not enforce %q after EnvironmentFile:\n%s", required, committed)
		}
	}
}

func TestTmuxInstallerIsSharedByQuickInstallAndCI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	installer := readTextFile(t, filepath.Join(root, "install-tmux.sh"))
	for _, required := range []string{
		`TMUX_VERSION="3.7b"`,
		`TMUX_SHA256="87f2e99e3b685973f2ca002ffd6ed7e51a5744f7009daae5a15670b6d532db96"`,
		"sha256sum --check",
		`BIN_DIR="${BIN_DIR:-"$HOME/.local/bin"}"`,
		`./configure --prefix="$stage_dir"`,
		`built_version="$("$built_tmux" -V)"`,
		`mv -f "$destination_tmp" "$BIN_DIR/tmux"`,
		`[ "$installed_version" = "tmux $TMUX_VERSION" ]`,
		`LANG="C.UTF-8"`,
		`LC_ALL="C.UTF-8"`,
	} {
		if !strings.Contains(installer, required) {
			t.Fatalf("tmux installer lacks %q", required)
		}
	}
	if strings.Contains(installer, `PREFIX="${PREFIX:`) {
		t.Fatal("tmux installer still has an independent PREFIX destination")
	}

	readme := readTextFile(t, filepath.Join(root, "README.md"))
	if !strings.Contains(readme, "raw.githubusercontent.com/hajekmi/control-agents/main/install-tmux.sh") {
		t.Fatal("Quick Install does not use the repository tmux installer")
	}
	if strings.Contains(readme, "tmux 3.7b or newer") || !strings.Contains(readme, "Exactly `tmux` 3.7b") {
		t.Fatal("README does not state the exact tmux 3.7b runtime contract")
	}
	workflow := readTextFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if !strings.Contains(workflow, "sh ./install-tmux.sh") {
		t.Fatal("CI does not use the repository tmux installer")
	}
	if !strings.Contains(workflow, `test "$(tmux -V)" = "tmux 3.7b"`) {
		t.Fatal("CI does not verify the exact supported tmux version")
	}
	for _, duplicated := range []string{"tmux_sha256=", "tmux_archive=", "./configure --prefix="} {
		if strings.Contains(workflow, duplicated) {
			t.Fatalf("CI duplicates tmux installer behavior with %q", duplicated)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "TERM=$(E2E_TERM) RUN_E2E=1 go test -count=1 ./test/e2e") {
		t.Fatal("the E2E target does not own its capable terminal type")
	}
	if strings.Contains(workflow, "TERM: xterm-256color") {
		t.Fatal("CI duplicates the E2E terminal capability")
	}

	plan := readTextFile(t, filepath.Join(root, "INSTALL_SIMPLIFICATION_PLAN.md"))
	if strings.Contains(plan, "sudo apt install tmux ttyd") {
		t.Fatal("installation plan still presents the incompatible Ubuntu tmux package as supported")
	}
	if count := strings.Count(plan, "raw.githubusercontent.com/hajekmi/control-agents/main/install-tmux.sh"); count != 2 {
		t.Fatalf("installation plan shared tmux installer references = %d, want 2", count)
	}
	for _, dependency := range []string{"bison", "build-essential", "libevent-dev", "libncurses-dev", "pkg-config", "ttyd"} {
		if count := strings.Count(plan, dependency); count < 2 {
			t.Fatalf("installation plan dependency %q appears %d times, want both install paths", dependency, count)
		}
	}
}

func TestInstallerRejectsChecksumMismatch(t *testing.T) {
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is required for installer integration tests")
	}
	result := runInstaller(t, "x86_64", "linux-amd64", true)
	if result.err == nil {
		t.Fatalf("installer accepted a checksum mismatch:\n%s", result.output)
	}
	for _, name := range []string{"control-agents-server", "control-agents"} {
		if _, err := os.Stat(filepath.Join(result.prefix, "bin", name)); !os.IsNotExist(err) {
			t.Fatalf("%s was installed after checksum failure: %v", name, err)
		}
	}
}

type installerResult struct {
	prefix     string
	configHome string
	output     string
	err        error
}

func runInstaller(t *testing.T, machine, platform string, corruptChecksum bool) installerResult {
	return runInstallerWithTmuxVersion(t, machine, platform, corruptChecksum, "tmux 3.7b")
}

func runInstallerWithTmuxVersion(t *testing.T, machine, platform string, corruptChecksum bool, tmuxVersion string) installerResult {
	return runInstallerWithEnvironment(t, machine, platform, corruptChecksum, tmuxVersion, "")
}

func runInstallerWithEnvironment(t *testing.T, machine, platform string, corruptChecksum bool, tmuxVersion, environmentContents string) installerResult {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	fakeBin := filepath.Join(temporary, "fake-bin")
	releaseDir := filepath.Join(temporary, "release")
	home := filepath.Join(temporary, "home")
	tmp := filepath.Join(temporary, "tmp")
	prefix := filepath.Join(temporary, "prefix")
	configHome := filepath.Join(temporary, "config")
	for _, directory := range []string{fakeBin, releaseDir, home, tmp, filepath.Join(prefix, "bin")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if environmentContents != "" {
		environmentPath := filepath.Join(configHome, "control-agents", "env")
		if err := os.MkdirAll(filepath.Dir(environmentPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(environmentPath, []byte(environmentContents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "${1:-}" in
  -s) printf '%s\n' Linux ;;
  -m) printf '%s\n' "$FAKE_UNAME_MACHINE" ;;
  *) exit 1 ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/bin/sh
set -eu
url=
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    -*) shift ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
[ -n "$url" ] && [ -n "$output" ]
cp "$FAKE_RELEASE_DIR/${url##*/}" "$output"
`)
	writeExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "ttyd"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "tmux"), `#!/bin/sh
if [ "${1:-}" = "-V" ]; then
  printf '%s\n' "$FAKE_TMUX_VERSION"
fi
exit 0
`)
	writeExecutable(t, filepath.Join(prefix, "bin", "tmux"), fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "-V" ]; then
  printf '%%s\n' '%s'
fi
exit 0
`, tmuxVersion))

	serverAsset := "control-agents-server-" + platform
	clientAsset := "control-agents-" + platform
	serverContents := []byte("server-" + platform)
	clientContents := []byte("client-" + platform)
	if err := os.WriteFile(filepath.Join(releaseDir, serverAsset), serverContents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, clientAsset), clientContents, 0o600); err != nil {
		t.Fatal(err)
	}
	serverChecksum := sha256.Sum256(serverContents)
	clientChecksum := sha256.Sum256(clientContents)
	if corruptChecksum {
		serverChecksum = sha256.Sum256([]byte("different server"))
	}
	manifest := fmt.Sprintf("%x  %s\n%x  %s\n", serverChecksum, serverAsset, clientChecksum, clientAsset)
	if err := os.WriteFile(filepath.Join(releaseDir, "sha256sums.txt"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", filepath.Join(root, "install.sh"))
	command.Env = append(os.Environ(),
		"HOME="+home,
		"TMPDIR="+tmp,
		"PREFIX="+prefix,
		"XDG_CONFIG_HOME="+configHome,
		"BIN_DIR=",
		"CONFIG_DIR=",
		"SYSTEMD_USER_DIR=",
		"ENV_FILE=",
		"SERVICE_FILE=",
		"FAKE_RELEASE_DIR="+releaseDir,
		"FAKE_UNAME_MACHINE="+machine,
		"FAKE_TMUX_VERSION="+tmuxVersion,
		"LOCAL_BIN_DIR=",
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, runErr := command.CombinedOutput()
	return installerResult{
		prefix:     prefix,
		configHome: configHome,
		output:     string(output),
		err:        runErr,
	}
}

func serviceDirective(t *testing.T, unit, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("service lacks %s directive:\n%s", prefix, unit)
	return ""
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s contents = %q, want %q", path, data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("%s mode = %o, want 755", path, info.Mode().Perm())
	}
}
