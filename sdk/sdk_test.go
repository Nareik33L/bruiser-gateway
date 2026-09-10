package sdk_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}

func TestNodeSDK(t *testing.T) {
	root := repoRoot(t)
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	cmd := exec.Command("node", "--test", "test.js")
	cmd.Dir = filepath.Join(root, "sdk", "node")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node sdk: %v\n%s", err, out)
	}
}

func TestPythonSDK(t *testing.T) {
	root := repoRoot(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	if err := exec.Command("python3", "-c", "import cryptography").Run(); err != nil {
		t.Skip("python cryptography not installed")
	}
	cmd := exec.Command("python3", "-m", "unittest", "tests/test_protect.py")
	cmd.Dir = filepath.Join(root, "sdk", "python")
	cmd.Env = append(os.Environ(), "CI=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python sdk: %v\n%s", err, out)
	}
}
