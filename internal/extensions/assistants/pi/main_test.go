package pi

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var fakePiBinary string

func TestMain(m *testing.M) {
	root := filepath.Clean(filepath.Join(packageDir(), "..", "..", "..", ".."))
	dir, err := os.MkdirTemp("", "takt-fake-pi-")
	if err != nil {
		panic(err)
	}
	fakePiBinary = filepath.Join(dir, "takt-fake-pi")
	if runtime.GOOS == "windows" {
		fakePiBinary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", fakePiBinary, "./internal/testsupport/cmd/takt-fake-pi")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build fake Pi: " + err.Error() + ": " + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}
