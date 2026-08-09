package opencode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var fakeOpenCodeBinary string

func TestMain(m *testing.M) {
	root := filepath.Clean(filepath.Join(packageDir(), "..", "..", "..", ".."))
	dir, err := os.MkdirTemp("", "takt-fake-opencode-")
	if err != nil {
		panic(err)
	}
	fakeOpenCodeBinary = filepath.Join(dir, "takt-fake-opencode")
	if runtime.GOOS == "windows" {
		fakeOpenCodeBinary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", fakeOpenCodeBinary, "./internal/testsupport/cmd/takt-fake-opencode")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build fake OpenCode: " + err.Error() + ": " + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}
