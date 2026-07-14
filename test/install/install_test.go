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
			} {
				if !strings.Contains(string(data), directive) {
					t.Fatalf("generated service lacks %s:\n%s", directive, data)
				}
			}
		})
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
	for _, directory := range []string{fakeBin, releaseDir, home, tmp} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
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
	for _, command := range []string{"systemctl", "tmux", "ttyd"} {
		writeExecutable(t, filepath.Join(fakeBin, command), "#!/bin/sh\nexit 0\n")
	}

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

	prefix := filepath.Join(temporary, "prefix")
	configHome := filepath.Join(temporary, "config")
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
