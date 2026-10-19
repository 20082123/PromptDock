//go:build windows

package main

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestWindowsBootPath(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("PROMPTDOCK_DATA_DIR", t.TempDir())
	r, err := NewRepository()
	if err != nil && r == nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Log(err)
	}
	a := &App{repo: r}
	if err := a.initWindowsWithShell(false); err != nil {
		t.Fatal(err)
	}
	popup := a.popup.hwnd
	if !isWindow(popup) {
		t.Fatal("startup did not create the popup window")
	}
	a.shutdown()
	if isWindow(popup) {
		t.Fatal("shutdown did not destroy the popup window")
	}
	unregisterTestClasses()
}

func unregisterTestClasses() {
	for _, name := range []string{"PromptDockPopup", "PromptDockManager", "PromptDockUpdate"} {
		user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(utf16Ptr(name))), getModuleHandle())
	}
}
