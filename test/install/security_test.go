package install_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDeploymentFilesContainNoEmbeddedPrivateKeysOrKnownSecretShapes(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, "install.sh"), filepath.Join(root, "Containerfile"),
		filepath.Join(root, "systemd"), filepath.Join(root, ".github", "workflows"),
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`-----BEGIN (?:OPENSSH |RSA |EC |DSA )?PRIVATE KEY-----`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{32,}\b`),
	}
	for _, path := range paths {
		err := filepath.WalkDir(path, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(candidate)
			if err != nil {
				return err
			}
			for _, pattern := range patterns {
				if pattern.Match(data) {
					t.Errorf("deployment file %s contains forbidden secret shape %s", candidate, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestServerSourceDoesNotEnablePprofOrHeapDumpEndpoints(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(root, "cmd"), filepath.Join(root, "internal")} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"net/http/pprof", "WriteHeapDump", "/debug/pprof", "/api/heap-dump"} {
				if strings.Contains(string(data), forbidden) {
					t.Errorf("production source %s contains forbidden diagnostic surface %q", path, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
