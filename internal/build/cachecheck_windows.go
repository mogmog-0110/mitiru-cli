package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// 見えない端末の中で chcp 65001 してから server を前景で走らせる。
// mitiru が終わっても server は生き残る。
func startUTF8CacheServer(exe string) error {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /d /s /c "chcp 65001 >NUL & "` + filepath.FromSlash(exe) + `""`,
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
	cmd.Env = append(os.Environ(), "SCCACHE_START_SERVER=1", "SCCACHE_NO_DAEMON=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
