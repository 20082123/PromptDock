//go:build windows

package main

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"
)

type HWND uintptr
type HINSTANCE uintptr
type HDC uintptr
type HBRUSH uintptr
type HICON uintptr
type HCURSOR uintptr
type HMENU uintptr
type HGLOBAL uintptr
type HMONITOR uintptr

const (
	WS_OVERLAPPED    = 0x00000000
	WS_CAPTION       = 0x00C00000
	WS_SYSMENU       = 0x00080000
	WS_MINIMIZEBOX   = 0x00020000
	WS_POPUP         = 0x80000000
	WS_CHILD         = 0x40000000
	WS_VISIBLE       = 0x10000000
	WS_BORDER        = 0x00800000
	WS_THICKFRAME    = 0x00040000
	WS_VSCROLL       = 0x00200000
	WS_TABSTOP       = 0x00010000
	WS_CLIPCHILDREN  = 0x02000000
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_CLIENTEDGE = 0x00000200

	SW_HIDE       = 0
	SW_SHOWNORMAL = 1
	SW_SHOW       = 5
	SW_MINIMIZE   = 6
	SW_RESTORE    = 9

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOACTIVATE   = 0x0010
	SWP_SHOWWINDOW   = 0x0040
	SWP_FRAMECHANGED = 0x0020

	HWND_TOPMOST   = ^uintptr(0)
	HWND_NOTOPMOST = ^uintptr(1)

	WM_NULL          = 0x0000
	WM_CREATE        = 0x0001
	WM_DESTROY       = 0x0002
	WM_CLOSE         = 0x0010
	WM_QUIT          = 0x0012
	WM_PAINT         = 0x000F
	WM_SIZE          = 0x0005
	WM_COMMAND       = 0x0111
	WM_HOTKEY        = 0x0312
	WM_LBUTTONDOWN   = 0x0201
	WM_LBUTTONUP     = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205
	WM_MOUSEWHEEL    = 0x020A
	WM_MOUSEMOVE     = 0x0200
	WM_ACTIVATE      = 0x0006
	WM_SETFOCUS      = 0x0007
	WM_KILLFOCUS     = 0x0008
	WM_ERASEBKGND    = 0x0014
	WM_CTLCOLOREDIT  = 0x0133
	WM_SETFONT       = 0x0030
	EM_SETCUEBANNER  = 0x1501
	EM_GETSEL        = 0x00B0
	EM_SETLIMITTEXT  = 0x00C5
	EM_LINESCROLL    = 0x00B6
	WM_GETTEXT       = 0x000D
	WM_GETTEXTLENGTH = 0x000E
	WM_SETTEXT       = 0x000C
	WM_KEYDOWN       = 0x0100
	WM_CHAR          = 0x0102
	WM_NCHITTEST     = 0x0084
	WM_NCCALCSIZE    = 0x0083
	WM_CONTEXTMENU   = 0x007B

	WA_INACTIVE = 0

	VK_BACK    = 0x08
	VK_TAB     = 0x09
	VK_RETURN  = 0x0D
	VK_ESCAPE  = 0x1B
	VK_SPACE   = 0x20
	VK_PRIOR   = 0x21
	VK_NEXT    = 0x22
	VK_END     = 0x23
	VK_HOME    = 0x24
	VK_LEFT    = 0x25
	VK_UP      = 0x26
	VK_RIGHT   = 0x27
	VK_DOWN    = 0x28
	VK_DELETE  = 0x2E
	VK_CONTROL = 0x11
	VK_MENU    = 0x12
	VK_SHIFT   = 0x10
	VK_LWIN    = 0x5B
	VK_RWIN    = 0x5C
	VK_C       = 0x43
	VK_D       = 0x44
	VK_V       = 0x56

	MOD_ALT      = 0x0001
	MOD_CONTROL  = 0x0002
	MOD_NOREPEAT = 0x4000

	EN_CHANGE     = 0x0300
	LBN_SELCHANGE = 1
	BN_CLICKED    = 0

	IDHOTKEY                 = 1001
	ID_SEARCH                = 1002
	ID_DETAIL_TEXT           = 1003
	ID_DETAIL_INSERT         = 1004
	ID_DETAIL_EDIT           = 1005
	ID_DETAIL_COPY           = 1006
	ID_MANAGER_LIST          = 1101
	ID_MANAGER_TITLE         = 1102
	ID_MANAGER_DESC          = 1103
	ID_MANAGER_BODY          = 1104
	ID_MANAGER_TASK          = 1105
	ID_MANAGER_SAVE          = 1106
	ID_MANAGER_NEW           = 1107
	ID_MANAGER_DELETE        = 1108
	ID_MANAGER_FAV           = 1109
	ID_MANAGER_EXPORT        = 1110
	ID_MANAGER_IMPORT        = 1111
	ID_MANAGER_EXIT          = 1112
	ID_MANAGER_TAGS          = 1113
	ID_MANAGER_SOURCE        = 1114
	ID_MANAGER_SEARCH        = 1115
	ID_MANAGER_PACK          = 1116
	ID_MANAGER_COPY          = 1117
	ID_MANAGER_INSERT        = 1118
	ID_MANAGER_SOURCE_TOGGLE = 1119

	LB_SETITEMHEIGHT = 0x01A0
	LB_GETITEMHEIGHT = 0x01A1

	TRAY_SHOW           = 2001
	TRAY_MANAGE         = 2002
	TRAY_EXIT           = 2003
	TRAY_TOGGLE_TOPMOST = 2004

	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	INPUT_KEYBOARD  = 1
	KEYEVENTF_KEYUP = 0x0002

	DT_LEFT         = 0x00000000
	DT_TOP          = 0x00000000
	DT_CENTER       = 0x00000001
	DT_VCENTER      = 0x00000004
	DT_WORDBREAK    = 0x00000010
	DT_SINGLELINE   = 0x00000020
	DT_NOPREFIX     = 0x00000800
	DT_END_ELLIPSIS = 0x00008000
	DT_CALCRECT     = 0x00000400

	TRANSPARENT      = 1
	DEFAULT_GUI_FONT = 17

	MF_STRING       = 0x00000000
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100

	NIM_ADD        = 0x00000000
	NIM_DELETE     = 0x00000002
	NIM_SETVERSION = 0x00000004
	NIF_MESSAGE    = 0x00000001
	NIF_ICON       = 0x00000002
	NIF_TIP        = 0x00000004
	WM_TRAY        = 0x8001

	IDI_APPLICATION      = 32512
	ERROR_ALREADY_EXISTS = 183

	MONITOR_DEFAULTTONEAREST = 2
	SPI_GETWORKAREA          = 0x0030

	GWLP_WNDPROC  = -4
	GWL_STYLE     = -16
	HTCLIENT      = 1
	HTCAPTION     = 2
	HTLEFT        = 10
	HTRIGHT       = 11
	HTTOP         = 12
	HTTOPLEFT     = 13
	HTTOPRIGHT    = 14
	HTBOTTOM      = 15
	HTBOTTOMLEFT  = 16
	HTBOTTOMRIGHT = 17

	GCS_COMPSTR = 8

	IDYES           = 6
	IDNO            = 7
	IDCANCEL        = 2
	MB_YESNOCANCEL  = 0x00000003
	MB_ICONQUESTION = 0x00000020
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	imm32    = syscall.NewLazyDLL("imm32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procUpdateWindow             = user32.NewProc("UpdateWindow")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procSetWindowTextW           = user32.NewProc("SetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetActiveWindow          = user32.NewProc("SetActiveWindow")
	procGetFocus                 = user32.NewProc("GetFocus")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsWindowEnabled          = user32.NewProc("IsWindowEnabled")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetGUIThreadInfo         = user32.NewProc("GetGUIThreadInfo")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procRegisterHotKey           = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey         = user32.NewProc("UnregisterHotKey")
	procGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	procGetKeyState              = user32.NewProc("GetKeyState")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procMonitorFromPoint         = user32.NewProc("MonitorFromPoint")
	procBeginPaint               = user32.NewProc("BeginPaint")
	procEndPaint                 = user32.NewProc("EndPaint")
	procFillRect                 = user32.NewProc("FillRect")
	procCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
	procSelectObject             = gdi32.NewProc("SelectObject")
	procGetStockObject           = gdi32.NewProc("GetStockObject")
	procSetBkMode                = gdi32.NewProc("SetBkMode")
	procSetTextColor             = gdi32.NewProc("SetTextColor")
	procDrawTextW                = user32.NewProc("DrawTextW")
	procGetSysColorBrush         = user32.NewProc("GetSysColorBrush")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory            = kernel32.NewProc("RtlMoveMemory")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procSendInput                = user32.NewProc("SendInput")
	procMoveFileExW              = kernel32.NewProc("MoveFileExW")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW            = shell32.NewProc("ShellExecuteW")
	procImmGetContext            = imm32.NewProc("ImmGetContext")
	procImmReleaseContext        = imm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStringW = imm32.NewProc("ImmGetCompositionStringW")
	procGetSaveFileNameW         = comdlg32.NewProc("GetSaveFileNameW")
	procGetOpenFileNameW         = comdlg32.NewProc("GetOpenFileNameW")
	procCallWindowProcW          = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procSetProcessDPIAware       = user32.NewProc("SetProcessDPIAware")
)

type Point struct{ X, Y int32 }
type Rect struct{ Left, Top, Right, Bottom int32 }

type Message struct {
	Hwnd           HWND
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             Point
}

type PaintStruct struct {
	Hdc       HDC
	Erase     int32
	Paint     Rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type WndClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   HINSTANCE
	Icon       HICON
	Cursor     HCURSOR
	Background HBRUSH
	MenuName   *uint16
	ClassName  *uint16
	IconSm     HICON
}

type MonitorInfo struct {
	CbSize  uint32
	Monitor Rect
	Work    Rect
	Flags   uint32
}

type GuiThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    HWND
	HwndFocus     HWND
	HwndCapture   HWND
	HwndMenuOwner HWND
	HwndMoveSize  HWND
	HwndCaret     HWND
	RcCaret       Rect
}

type KeybdInput struct {
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
}

type MouseInput struct {
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type HardwareInput struct {
	Message uint32
	ParamL  uint16
	ParamH  uint16
}

// inputUnion reserves the largest member of Win32's INPUT union while keeping
// pointer alignment. On 64-bit Windows it is 32 bytes and aligned to 8; on
// 32-bit Windows it is 24 bytes and aligned to 4.
type inputUnion [unsafe.Sizeof(MouseInput{}) / unsafe.Sizeof(uintptr(0))]uintptr

type Input struct {
	Type  uint32
	Union inputUnion
}

type NotifyIconData struct {
	CbSize           uint32
	Wnd              HWND
	ID               uint32
	Flags            uint32
	Callback         uint32
	Icon             HICON
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	Guid             [16]byte
	BalloonIcon      HICON
}

type OpenFileName struct {
	StructSize    uint32
	Owner         HWND
	Instance      HINSTANCE
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	Hook          uintptr
	TemplateName  *uint16
	Reserved      uintptr
	Reserved2     uint32
	FlagsEx       uint32
}

func loword(v uintptr) uint16    { return uint16(v & 0xffff) }
func hiword(v uintptr) uint16    { return uint16((v >> 16) & 0xffff) }
func getX(l uintptr) int32       { return int32(int16(l & 0xffff)) }
func getY(l uintptr) int32       { return int32(int16((l >> 16) & 0xffff)) }
func wheelDelta(w uintptr) int16 { return int16((w >> 16) & 0xffff) }

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func makeCallback(fn func(HWND, uint32, uintptr, uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		return fn(HWND(hwnd), uint32(msg), wParam, lParam)
	})
}

func registerClass(name string, proc uintptr) error {
	class := utf16Ptr(name)
	cur := appIcon()
	wc := WndClassEx{CbSize: uint32(unsafe.Sizeof(WndClassEx{})), Style: 0x0008 /* CS_DBLCLKS */, WndProc: proc, Instance: HINSTANCE(getModuleHandle()), Icon: HICON(cur), IconSm: HICON(cur), ClassName: class}
	ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 && err != syscall.Errno(1410) {
		return err
	}
	return nil
}

func getModuleHandle() uintptr {
	proc := kernel32.NewProc("GetModuleHandleW")
	v, _, _ := proc.Call(0)
	return v
}

func createWindow(ex uint32, class, title string, style uint32, x, y, w, h int32, parent HWND, menu uintptr, proc uintptr) HWND {
	v, _, _ := procCreateWindowExW.Call(uintptr(ex), uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(parent), menu, getModuleHandle(), 0)
	return HWND(v)
}

func showWindow(hwnd HWND, cmd int32) { procShowWindow.Call(uintptr(hwnd), uintptr(cmd)) }
func updateWindow(hwnd HWND)          { procUpdateWindow.Call(uintptr(hwnd)) }
func destroyWindow(hwnd HWND)         { procDestroyWindow.Call(uintptr(hwnd)) }
func setWindowText(hwnd HWND, text string) {
	procSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func windowsText(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

func setMultilineText(hwnd HWND, text string) { setWindowText(hwnd, windowsText(text)) }

func enableWindow(hwnd HWND, enabled bool) {
	v := uintptr(0)
	if enabled {
		v = 1
	}
	user32.NewProc("EnableWindow").Call(uintptr(hwnd), v)
}

func getWindowText(hwnd HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	buf := make([]uint16, n+1)
	if n > 0 {
		procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), n+1)
	}
	return syscall.UTF16ToString(buf)
}

func editHasSelection(hwnd HWND) bool {
	var start, end uint32
	sendMessage(hwnd, EM_GETSEL, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&end)))
	return start != end
}

func sendMessage(hwnd HWND, msg uint32, w, l uintptr) uintptr {
	v, _, _ := procSendMessageW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return v
}

func setWindowLongPtr(hwnd HWND, index int32, value uintptr) uintptr {
	v, _, _ := procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index), value)
	return v
}

func callWindowProc(proc uintptr, hwnd HWND, msg uint32, w, l uintptr) uintptr {
	v, _, _ := procCallWindowProcW.Call(proc, uintptr(hwnd), uintptr(msg), w, l)
	return v
}

func setFocus(hwnd HWND) { procSetFocus.Call(uintptr(hwnd)) }
func focusedWindow() HWND {
	v, _, _ := procGetFocus.Call()
	return HWND(v)
}
func setActiveWindow(hwnd HWND) {
	procSetActiveWindow.Call(uintptr(hwnd))
}
func setWindowPos(hwnd HWND, insert HWND, x, y, w, h int32, flags uint32) {
	procSetWindowPos.Call(uintptr(hwnd), uintptr(insert), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

func getClientRect(hwnd HWND) Rect {
	var r Rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

func getWindowRect(hwnd HWND) Rect {
	var r Rect
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

func foregroundWindow() HWND  { v, _, _ := procGetForegroundWindow.Call(); return HWND(v) }
func isWindow(hwnd HWND) bool { v, _, _ := procIsWindow.Call(uintptr(hwnd)); return v != 0 }
func isWindowVisible(hwnd HWND) bool {
	v, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return v != 0
}
func isWindowEnabled(hwnd HWND) bool {
	v, _, _ := procIsWindowEnabled.Call(uintptr(hwnd))
	return v != 0
}
func setForeground(hwnd HWND) bool {
	v, _, _ := procSetForegroundWindow.Call(uintptr(hwnd))
	return v != 0
}

func getWindowPID(hwnd HWND) uint32 {
	_, pid := getWindowThreadPID(hwnd)
	return pid
}

func getWindowThreadPID(hwnd HWND) (uint32, uint32) {
	var pid uint32
	threadID, _, _ := procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	return uint32(threadID), pid
}

func getCurrentThreadID() uint32 {
	v, _, _ := procGetCurrentThreadId.Call()
	return uint32(v)
}

func getThreadFocus(threadID uint32) HWND {
	info := GuiThreadInfo{CbSize: uint32(unsafe.Sizeof(GuiThreadInfo{}))}
	v, _, _ := procGetGUIThreadInfo.Call(uintptr(threadID), uintptr(unsafe.Pointer(&info)))
	if v == 0 {
		return 0
	}
	return info.HwndFocus
}

func attachThreadInput(from, to uint32, attach bool) bool {
	if from == 0 || to == 0 || from == to {
		return from == to && from != 0
	}
	attachFlag := uintptr(0)
	if attach {
		attachFlag = 1
	}
	v, _, _ := procAttachThreadInput.Call(uintptr(from), uintptr(to), attachFlag)
	return v != 0
}

// restoreTargetFocus reconnects the original input queue long enough to
// restore both the window and its focused child control. Merely foregrounding
// the top-level browser window is insufficient after the popup took focus.
func restoreTargetFocus(target TargetSession) bool {
	threadID, pid := getWindowThreadPID(target.HWND)
	if threadID == 0 || pid != target.PID || (target.ThreadID != 0 && threadID != target.ThreadID) {
		return false
	}
	if !setForeground(target.HWND) {
		return false
	}
	currentThreadID := getCurrentThreadID()
	attached := attachThreadInput(currentThreadID, threadID, true)
	if attached {
		defer attachThreadInput(currentThreadID, threadID, false)
	}
	setActiveWindow(target.HWND)
	if target.FocusHWND == 0 {
		return true
	}
	if !isWindow(target.FocusHWND) || getWindowPID(target.FocusHWND) != target.PID {
		return false
	}
	setFocus(target.FocusHWND)
	return getThreadFocus(threadID) == target.FocusHWND
}

func registerHotkey(hwnd HWND, id int, modifiers, key uint32) error {
	v, _, err := procRegisterHotKey.Call(uintptr(hwnd), uintptr(id), uintptr(modifiers), uintptr(key))
	if v == 0 {
		return err
	}
	return nil
}

func unregisterHotkey(hwnd HWND, id int) { procUnregisterHotKey.Call(uintptr(hwnd), uintptr(id)) }

func isKeyDown(key uint32) bool {
	v, _, _ := procGetAsyncKeyState.Call(uintptr(key))
	return int16(v) < 0
}
func keyState(key uint32) int16 { v, _, _ := procGetKeyState.Call(uintptr(key)); return int16(v) }

func monitorWorkArea(x, y int32) Rect {
	var pt Point
	pt.X, pt.Y = x, y
	m, _, _ := procMonitorFromPoint.Call(uintptr(pt.X), uintptr(pt.Y), MONITOR_DEFAULTTONEAREST)
	info := MonitorInfo{CbSize: uint32(unsafe.Sizeof(MonitorInfo{}))}
	if m != 0 {
		procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&info)))
		return info.Work
	}
	return Rect{Right: 1920, Bottom: 1080}
}

func createBrush(color uint32) HBRUSH {
	v, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return HBRUSH(v)
}
func deleteObject(obj uintptr) { procDeleteObject.Call(obj) }

func fillRect(hdc HDC, rect *Rect, brush HBRUSH) {
	procFillRect.Call(uintptr(hdc), uintptr(unsafe.Pointer(rect)), uintptr(brush))
}

func drawText(hdc HDC, text string, rect *Rect, format uint32) {
	procDrawTextW.Call(uintptr(hdc), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(rect)), uintptr(format))
}

func setTextColor(hdc HDC, color uint32) { procSetTextColor.Call(uintptr(hdc), uintptr(color)) }
func setBkMode(hdc HDC, mode int32)      { procSetBkMode.Call(uintptr(hdc), uintptr(mode)) }
func selectObject(hdc HDC, obj uintptr)  { procSelectObject.Call(uintptr(hdc), obj) }
func stockObject(index int32) uintptr    { v, _, _ := procGetStockObject.Call(uintptr(index)); return v }

func copyToClipboard(owner HWND, text string) error {
	if text == "" {
		return errors.New("正文为空，无法插入")
	}
	if v, _, _ := procOpenClipboard.Call(uintptr(owner)); v == 0 {
		return errors.New("剪贴板正被其他程序占用，请稍后重试")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	data, _ := syscall.UTF16FromString(windowsText(text))
	bytes := uintptr(len(data) * 2)
	h, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, bytes)
	if h == 0 {
		return errors.New("分配剪贴板内存失败")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return errors.New("锁定剪贴板内存失败")
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), bytes)
	procGlobalUnlock.Call(h)
	if v, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, h); v == 0 {
		return errors.New("写入剪贴板失败")
	}
	return nil
}

func sendPaste() bool {
	inputs := pasteInputs()
	v, _, _ := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(Input{}))
	return v == uintptr(len(inputs))
}

func pasteInputs() []Input {
	return []Input{
		newKeyboardInput(VK_CONTROL, 0),
		newKeyboardInput(VK_V, 0),
		newKeyboardInput(VK_V, KEYEVENTF_KEYUP),
		newKeyboardInput(VK_CONTROL, KEYEVENTF_KEYUP),
	}
}

func newKeyboardInput(vk uint16, flags uint32) Input {
	input := Input{Type: INPUT_KEYBOARD}
	*(*KeybdInput)(unsafe.Pointer(&input.Union[0])) = KeybdInput{Vk: vk, Flags: flags}
	return input
}

var insertReleaseKeys = []uint32{VK_RETURN, VK_CONTROL, VK_MENU, VK_SHIFT, VK_LWIN, VK_RWIN, 'P'}

// waitModifiersReleased keeps the key that chose the prompt, the hotkey, and
// related modifiers out of the restored target input queue. A timeout is a
// safety failure: callers must not send the paste chord afterwards.
func waitModifiersReleased() bool {
	return waitForKeysReleased(insertReleaseKeys, 160, isKeyDown, func() { sleepMillis(5) })
}

// waitForKeysReleased is deliberately parameterized so the release and
// timeout behavior can be checked without reading real keyboard state.
// waits is the number of 5 ms pauses permitted after observing held keys.
func waitForKeysReleased(keys []uint32, waits int, isDown func(uint32) bool, pause func()) bool {
	for {
		allReleased := true
		for _, key := range keys {
			if isDown(key) {
				allReleased = false
				break
			}
		}
		if allReleased {
			return true
		}
		if waits == 0 {
			return false
		}
		pause()
		waits--
	}
}

func sleepMillis(ms uint32) {
	proc := kernel32.NewProc("Sleep")
	proc.Call(uintptr(ms))
}

func moveFileReplace(source, target string) error {
	v, _, err := procMoveFileExW.Call(uintptr(unsafe.Pointer(utf16Ptr(source))), uintptr(unsafe.Pointer(utf16Ptr(target))), 1|8)
	if v == 0 {
		return err
	}
	return nil
}

func replaceFile(source, target string) error { return moveFileReplace(source, target) }

func createMutex(name string) (uintptr, bool) {
	v, _, _ := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(utf16Ptr(name))))
	if v == 0 {
		return 0, false
	}
	last, _, _ := kernel32.NewProc("GetLastError").Call()
	return v, last != ERROR_ALREADY_EXISTS
}

func closeHandle(handle uintptr) {
	if handle != 0 {
		procCloseHandle.Call(handle)
	}
}

func trayAdd(hwnd HWND, tip string) bool {
	icon := appIcon()
	n := NotifyIconData{CbSize: uint32(unsafe.Sizeof(NotifyIconData{})), Wnd: hwnd, ID: 1, Flags: NIF_MESSAGE | NIF_ICON | NIF_TIP, Callback: WM_TRAY, Icon: HICON(icon)}
	copy(n.Tip[:], mustUTF16(tip))
	v, _, _ := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&n)))
	if v == 0 {
		return false
	}
	n.TimeoutOrVersion = 4
	procShellNotifyIconW.Call(NIM_SETVERSION, uintptr(unsafe.Pointer(&n)))
	return true
}

func trayDelete(hwnd HWND) {
	n := NotifyIconData{CbSize: uint32(unsafe.Sizeof(NotifyIconData{})), Wnd: hwnd, ID: 1}
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&n)))
}

func mustUTF16(s string) []uint16 { v, _ := syscall.UTF16FromString(s); return v }

func isImeComposing(hwnd HWND) bool {
	v, _, _ := procImmGetContext.Call(uintptr(hwnd))
	if v == 0 {
		return false
	}
	defer procImmReleaseContext.Call(uintptr(hwnd), v)
	length, _, _ := procImmGetCompositionStringW.Call(v, GCS_COMPSTR, 0, 0)
	return int64(length) > 0
}

func openFileDialog(owner HWND, save bool) string {
	buf := make([]uint16, 512)
	filter := mustUTF16("PromptDock JSON (*.json)\x00*.json\x00所有文件 (*.*)\x00*.*\x00\x00")
	defExt := utf16Ptr("json")
	of := OpenFileName{StructSize: uint32(unsafe.Sizeof(OpenFileName{})), Owner: owner, Filter: &filter[0], File: &buf[0], MaxFile: uint32(len(buf)), DefExt: defExt, Flags: 0x00080000 | 0x00000008}
	var v uintptr
	if save {
		v, _, _ = procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&of)))
	} else {
		v, _, _ = procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&of)))
	}
	if v == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// openExternalURL delegates to Windows' registered default browser. It does
// not embed the page, fetch its content, or touch clipboard/input state.
func openExternalURL(owner HWND, url string) bool {
	v, _, _ := procShellExecuteW.Call(
		uintptr(owner),
		0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(url))),
		0,
		SW_SHOWNORMAL,
	)
	return v > 32
}

func messageBox(owner HWND, title, text string) {
	procMessageBoxW.Call(uintptr(owner), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), 0x00000040)
}

func confirmBox(owner HWND, title, text string) bool {
	v, _, _ := procMessageBoxW.Call(uintptr(owner), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), 0x00000034)
	return v == 6
}

func saveDiscardCancelBox(owner HWND, title, text string) int {
	v, _, _ := procMessageBoxW.Call(uintptr(owner), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), MB_YESNOCANCEL|MB_ICONQUESTION)
	return int(v)
}

func setProcessDPIAware() { procSetProcessDPIAware.Call() }

func createPopupMenu() HMENU {
	v, _, _ := procCreatePopupMenu.Call()
	return HMENU(v)
}

func appendMenu(menu HMENU, id int, text string) {
	procAppendMenuW.Call(uintptr(menu), MF_STRING, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func trackPopupMenu(menu HMENU, x, y int32, owner HWND) int {
	v, _, _ := procTrackPopupMenu.Call(uintptr(menu), TPM_RIGHTBUTTON|TPM_RETURNCMD, uintptr(x), uintptr(y), 0, uintptr(owner), 0)
	return int(v)
}

func destroyMenu(menu HMENU) { procDestroyMenu.Call(uintptr(menu)) }
