//go:build windows

// Local input surface for real paste recordings; never reads personal data.
package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

var u = syscall.NewLazyDLL("user32.dll")

func s(v string) uintptr { p, _ := syscall.UTF16PtrFromString(v); return uintptr(unsafe.Pointer(p)) }

type msg struct {
	H    uintptr
	M    uint32
	W, L uintptr
	T    uint32
	X, Y int32
	P    uint32
}
type cls struct {
	Size, Style                   uint32
	Proc                          uintptr
	A, B                          int32
	Instance, Icon, Cursor, Brush uintptr
	Menu, Name                    *uint16
	Small                         uintptr
}

func main() {
	runtime.LockOSThread()
	u.NewProc("SetProcessDPIAware").Call()
	inst, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	name, _ := syscall.UTF16PtrFromString("PromptDockDemoInput")
	callback := syscall.NewCallback(func(h uintptr, m uint32, w, l uintptr) uintptr {
		if m == 2 {
			u.NewProc("PostQuitMessage").Call(0)
			return 0
		}
		v, _, _ := u.NewProc("DefWindowProcW").Call(h, uintptr(m), w, l)
		return v
	})
	c := cls{Proc: callback, Instance: inst, Brush: 6, Name: name}
	c.Size = uint32(unsafe.Sizeof(c))
	u.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&c)))
	h, _, _ := u.NewProc("CreateWindowExW").Call(0, s("PromptDockDemoInput"), s("PromptDock 演示输入框（虚构数据）"), 0x10cf0000, 180, 120, 1040, 720, 0, 0, inst, 0)
	u.NewProc("CreateWindowExW").Call(0, s("STATIC"), s("本地演示 · 先点击输入框，再按 Ctrl+Alt+P，选择提示词后按 Enter。"), 0x50000000, 24, 22, 970, 28, h, 0, inst, 0)
	edit, _, _ := u.NewProc("CreateWindowExW").Call(0x200, s("EDIT"), 0, 0x50211044, 24, 70, 970, 560, h, 0, inst, 0)
	font, _, _ := syscall.NewLazyDLL("gdi32.dll").NewProc("CreateFontW").Call(^uintptr(15), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, s("Microsoft YaHei UI"))
	u.NewProc("SendMessageW").Call(edit, 0x30, font, 1)
	u.NewProc("SetFocus").Call(edit)
	var message msg
	for {
		v, _, _ := u.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(v) <= 0 {
			break
		}
		u.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		u.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}
