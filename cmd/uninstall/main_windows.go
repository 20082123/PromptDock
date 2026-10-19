//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

// The small launcher always invokes the installed main program's current
// uninstaller, so it does not require replacement during a software update.
func main() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(filepath.Join(filepath.Dir(self), "PromptDock.exe"), "--uninstall")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		title, _ := syscall.UTF16PtrFromString("PromptDock 卸载")
		text, _ := syscall.UTF16PtrFromString("无法启动卸载程序。请退出 PromptDock 后手动删除解压目录中的程序文件；个人库不会受影响。")
		syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0)
		return
	}
	_ = cmd.Process.Release()
}
