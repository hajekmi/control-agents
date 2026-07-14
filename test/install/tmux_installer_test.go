package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTmuxInstallerSupportsCustomDestinationAndRepeatedInstall(t *testing.T) {
	harness := newTmuxInstallerHarness(t)
	customBin := filepath.Join(harness.root, "custom", "commands")

	for _, marker := range []string{"first", "second"} {
		output, err := harness.run(customBin, "tmux 3.7b", marker, false)
		if err != nil {
			t.Fatalf("custom tmux install %s failed: %v\n%s", marker, err, output)
		}
		installed := filepath.Join(customBin, "tmux")
		if got := runVersionCommand(t, installed, "--marker"); got != marker {
			t.Fatalf("installed marker = %q, want %q", got, marker)
		}
		if got := runVersionCommand(t, installed, "-V"); got != "tmux 3.7b" {
			t.Fatalf("installed version = %q, want tmux 3.7b", got)
		}
		assertNoTmuxInstallTemporary(t, customBin)
	}

	configureLog := readTextFile(t, harness.configureLog)
	if count := strings.Count(strings.TrimSpace(configureLog), "\n") + 1; count != 2 {
		t.Fatalf("configure prefix count = %d, want 2: %q", count, configureLog)
	}
	if strings.Contains(configureLog, customBin) {
		t.Fatalf("build installed directly into the live destination: %q", configureLog)
	}
	if _, err := os.Stat(harness.unusedPrefix); !os.IsNotExist(err) {
		t.Fatalf("legacy PREFIX affected installation: %v", err)
	}
}

func TestTmuxInstallerDoesNotReplaceLiveBinaryAfterStagingFailure(t *testing.T) {
	harness := newTmuxInstallerHarness(t)
	customBin := filepath.Join(harness.root, "custom", "commands")
	installed := filepath.Join(customBin, "tmux")
	if err := os.MkdirAll(customBin, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, installed, "#!/bin/sh\nprintf '%s\\n' 'live-binary'\n")
	before, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}

	output, runErr := harness.run(customBin, "tmux 3.7b", "candidate", true)
	if runErr == nil {
		t.Fatalf("tmux installer accepted a corrupted staged binary:\n%s", output)
	}
	after, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("live tmux changed after staging failure: before %q, after %q", before, after)
	}
	assertNoTmuxInstallTemporary(t, customBin)
}

type tmuxInstallerHarness struct {
	root         string
	fakeBin      string
	home         string
	tmp          string
	configureLog string
	unusedPrefix string
	installer    string
}

func newTmuxInstallerHarness(t *testing.T) tmuxInstallerHarness {
	t.Helper()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	harness := tmuxInstallerHarness{
		root:         root,
		fakeBin:      filepath.Join(root, "fake-bin"),
		home:         filepath.Join(root, "home"),
		tmp:          filepath.Join(root, "tmp"),
		configureLog: filepath.Join(root, "configure.log"),
		unusedPrefix: filepath.Join(root, "unused-prefix"),
		installer:    filepath.Join(repository, "install-tmux.sh"),
	}
	for _, directory := range []string{harness.fakeBin, harness.home, harness.tmp} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	writeExecutable(t, filepath.Join(harness.fakeBin, "curl"), `#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$output" ]
: > "$output"
`)
	writeExecutable(t, filepath.Join(harness.fakeBin, "sha256sum"), "#!/bin/sh\ncat >/dev/null\n")
	writeExecutable(t, filepath.Join(harness.fakeBin, "tar"), `#!/bin/sh
set -eu
destination=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -C) destination="$2"; shift 2 ;;
    *) shift ;;
  esac
done
source_dir="$destination/tmux-3.7b"
mkdir -p "$source_dir"
cat > "$source_dir/configure" <<'EOF'
#!/bin/sh
set -eu
prefix=
for argument in "$@"; do
  case "$argument" in
    --prefix=*) prefix="${argument#--prefix=}" ;;
  esac
done
[ -n "$prefix" ]
printf '%s\n' "$prefix" > .stage-prefix
printf '%s\n' "$prefix" >> "$FAKE_CONFIGURE_LOG"
EOF
chmod 700 "$source_dir/configure"
`)
	writeExecutable(t, filepath.Join(harness.fakeBin, "make"), `#!/bin/sh
set -eu
if [ "${1:-}" != "install" ]; then
  exit 0
fi
prefix="$(cat .stage-prefix)"
mkdir -p "$prefix/bin"
cat > "$prefix/bin/tmux" <<EOF
#!/bin/sh
case "\${1:-}" in
  -V) printf '%s\\n' '$FAKE_BUILD_VERSION' ;;
  --marker) printf '%s\\n' '$FAKE_BUILD_MARKER' ;;
esac
EOF
chmod 700 "$prefix/bin/tmux"
`)
	writeExecutable(t, filepath.Join(harness.fakeBin, "install"), `#!/bin/sh
set -eu
if [ "${FAKE_CORRUPT_STAGED:-0}" = "1" ]; then
  destination="$4"
  cat > "$destination" <<'EOF'
#!/bin/sh
printf '%s\n' 'tmux corrupted'
EOF
  chmod 700 "$destination"
  exit 0
fi
exec /usr/bin/install "$@"
`)
	for _, command := range []string{"pkg-config", "bison", "cc"} {
		writeExecutable(t, filepath.Join(harness.fakeBin, command), "#!/bin/sh\nexit 0\n")
	}
	return harness
}

func (h tmuxInstallerHarness) run(binDir, version, marker string, corruptStaged bool) (string, error) {
	corrupt := "0"
	if corruptStaged {
		corrupt = "1"
	}
	command := exec.Command("sh", h.installer)
	command.Env = []string{
		"HOME=" + h.home,
		"TMPDIR=" + h.tmp,
		"BIN_DIR=" + binDir,
		"PREFIX=" + h.unusedPrefix,
		"MAKE_JOBS=1",
		"FAKE_BUILD_VERSION=" + version,
		"FAKE_BUILD_MARKER=" + marker,
		"FAKE_CONFIGURE_LOG=" + h.configureLog,
		"FAKE_CORRUPT_STAGED=" + corrupt,
		"PATH=" + h.fakeBin + ":/usr/bin:/bin",
	}
	output, err := command.CombinedOutput()
	return string(output), err
}

func runVersionCommand(t *testing.T, path string, argument string) string {
	t.Helper()
	output, err := exec.Command(path, argument).CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %s: %v\n%s", path, argument, err, output)
	}
	return strings.TrimSpace(string(output))
}

func assertNoTmuxInstallTemporary(t *testing.T, binDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(binDir, ".tmux.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary tmux files remain: %v", matches)
	}
}
