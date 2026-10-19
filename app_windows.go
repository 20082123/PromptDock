//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

type ViewKind int

const (
	viewHome ViewKind = iota
	viewTasks
	viewTask
	viewSpecial
	viewDetail
)

type TargetSession struct {
	HWND      HWND
	PID       uint32
	ThreadID  uint32
	FocusHWND HWND
}

func (t TargetSession) Valid() bool { return t.HWND != 0 && t.PID != 0 }

type Popup struct {
	hwnd         HWND
	search       HWND
	visible      bool
	view         ViewKind
	mode         string
	taskID       string
	selected     int
	detailID     string
	detailScroll int32
	listScroll   int
	searchText   string
	detailText   HWND
	detailInsert HWND
	detailEdit   HWND
	detailCopy   HWND
	detailFrom   popupLocation
	restoring    bool
}

type popupLocation struct {
	view                     ViewKind
	mode, taskID, searchText string
	selected, listScroll     int
}

type Manager struct {
	hwnd           HWND
	list           HWND
	title          HWND
	description    HWND
	body           HWND
	task           HWND
	tags           HWND
	source         HWND
	search         HWND
	taskIDs        []string
	query          string
	buttons        map[int]HWND
	labels         []HWND
	target         TargetSession
	ids            []string
	selected       int
	loadedID       string
	dirty          bool
	loadingForm    bool
	taskChanged    bool
	isNew          bool
	sourceExpanded bool
}

type App struct {
	repo             *Repository
	popup            *Popup
	manager          *Manager
	target           TargetSession
	mutex            uintptr
	dataLock         *fileLock
	updater          *softwareUpdater
	backgroundCancel context.CancelFunc
	hotkeyError      error
	shuttingDown     bool
	shellIntegrated  bool
	inserting        bool
	clipboardWriter  func(HWND, string) error
	releaseWaiter    func() bool
	pasteSender      func() bool
	insertionMessage func(HWND, string, string)
	// managerLeaveChoice lets the native-control regression exercise the three
	// confirmation outcomes without showing a modal MessageBox. Normal runs use
	// saveDiscardCancelBox below.
	managerLeaveChoice func(string) int
	originalEditProc   uintptr
}

var activeApp *App

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getenv("PROMPTDOCK_TEST") == "1" {
		return
	}
	setProcessDPIAware()
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "--apply-update":
			if len(os.Args) == 3 {
				err = applyUpdate(os.Args[2])
			} else {
				err = errors.New("缺少更新作业")
			}
			if err != nil {
				messageBox(0, "软件更新未完成", err.Error())
			}
			return
		case "--uninstall":
			err = uninstallMain()
			if err != nil {
				messageBox(0, "无法卸载", err.Error())
			}
			return
		case "--finish-uninstall":
			if len(os.Args) == 3 {
				err = finishUninstall(os.Args[2])
			} else {
				err = errors.New("缺少卸载作业")
			}
			if err != nil {
				messageBox(0, "卸载未完成", err.Error())
			}
			return
		}
	}
	dir, err := dataDirectory()
	if err != nil {
		messageBox(0, "无法启动", err.Error())
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		messageBox(0, "无法启动", err.Error())
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	var healthJob *updateJob
	if len(os.Args) == 3 && os.Args[1] == "--update-health" {
		job, err := validateHealthLaunch(os.Args[2], exe, dir)
		if err != nil {
			messageBox(0, "更新启动失败", err.Error())
			return
		}
		healthJob = &job
	}
	var gate *fileLock
	if healthJob == nil {
		gate, err = acquireFileLock(filepath.Join(filepath.Dir(exe), ".promptdock-install.lock"))
		if err != nil {
			messageBox(0, "无法启动", "软件正在更新、卸载，或程序目录无写入权限。请稍后重试，或解压到可写目录。")
			return
		}
		defer gate.Close()
	}
	dataLock, err := acquireFileLock(filepath.Join(dir, "library.lock"))
	if err != nil {
		messageBox(0, "PromptDock 无法启动", "个人库正在使用或数据目录不可写。请退出其他实例后重试。\n"+err.Error())
		return
	}
	defer dataLock.Close()
	mutex, first := createMutex("Local\\PromptDock.Singleton-" + hashBytes([]byte(strings.ToLower(dir)))[:20])
	if !first {
		closeHandle(mutex)
		messageBox(0, "PromptDock 已在运行", "请使用 Ctrl+Alt+P 唤出已有的 PromptDock。")
		return
	}
	repo, repositoryWarning := NewRepository()
	if repositoryWarning != nil {
		// Never replace an unreadable or incompatible personal library with defaults.
		if repo == nil {
			closeHandle(mutex)
			messageBox(0, "PromptDock 无法启动", repositoryWarning.Error())
			return
		}
	}

	app := &App{repo: repo, mutex: mutex, dataLock: dataLock}
	activeApp = app
	if err := app.initWindows(); err != nil {
		closeHandle(mutex)
		messageBox(0, "PromptDock 无法启动", err.Error())
		return
	}
	if healthJob != nil {
		if err := completeHealthStartup(*healthJob); err != nil {
			app.shutdown()
			messageBox(0, "更新启动确认失败", err.Error())
			return
		}
	} else {
		if message := pendingUpdateMessage(exe); message != "" {
			messageBox(app.popup.hwnd, "软件更新恢复提示", message)
		}
	}
	if gate != nil {
		gate.Close()
	}
	app.scheduleSoftwareCheck()
	if repositoryWarning != nil {
		messageBox(app.popup.hwnd, "本地数据提示", repositoryWarning.Error())
	}
	if app.hotkeyError != nil {
		messageBox(app.popup.hwnd, "快捷键注册失败", "默认快捷键 Ctrl+Alt+P 已被占用。请关闭占用它的程序后重启 PromptDock；当前仍可从托盘打开。")
	}
	if len(os.Args) > 1 && os.Args[1] == "--manage" {
		app.openManager()
	}
	app.run()
}

func (a *App) initWindows() error {
	return a.initWindowsWithShell(true)
}

func (a *App) initWindowsWithShell(integrate bool) error {
	if err := a.initSoftwareUpdater(); err != nil {
		return err
	}
	popupProc := makeCallback(a.popupWndProc)
	managerProc := makeCallback(a.managerWndProc)
	if err := registerClass("PromptDockPopup", popupProc); err != nil {
		return fmt.Errorf("注册浮窗失败: %w", err)
	}
	if err := registerClass("PromptDockManager", managerProc); err != nil {
		return fmt.Errorf("注册管理窗口失败: %w", err)
	}
	a.popup = &Popup{view: viewTasks, mode: popupEntryCategories}
	a.popup.hwnd = createWindow(WS_EX_TOOLWINDOW, "PromptDockPopup", "PromptDock", WS_POPUP|WS_THICKFRAME|WS_CLIPCHILDREN, 0, 0, 340, 420, 0, 0, popupProc)
	if a.popup.hwnd == 0 {
		return fmt.Errorf("创建浮窗失败")
	}
	if !integrate {
		return nil
	}
	a.shellIntegrated = true
	if err := registerHotkey(a.popup.hwnd, IDHOTKEY, MOD_CONTROL|MOD_ALT|MOD_NOREPEAT, 'P'); err != nil {
		a.hotkeyError = err
	}
	if !trayAdd(a.popup.hwnd, "PromptDock：Ctrl+Alt+P 唤出") {
		// The tray icon is optional; the hotkey and popup remain usable.
	}
	return nil
}

func (a *App) run() {
	var msg Message
	for {
		v, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(v) <= 0 {
			break
		}
		if msg.Message == WM_KEYDOWN && a.popup.view == viewDetail &&
			(msg.Hwnd == a.popup.detailText || msg.Hwnd == a.popup.detailInsert || msg.Hwnd == a.popup.detailEdit || msg.Hwnd == a.popup.detailCopy) {
			if msg.WParam == VK_ESCAPE {
				a.backOrHide()
				continue
			}
			if msg.WParam == VK_RETURN && msg.Hwnd == a.popup.detailInsert {
				a.insertSelected()
				continue
			}
		}
		if a.manager != nil && a.manager.hwnd != 0 {
			handled, _, _ := user32.NewProc("IsDialogMessageW").Call(uintptr(a.manager.hwnd), uintptr(unsafe.Pointer(&msg)))
			if handled != 0 {
				continue
			}
		}
		if a.updater != nil && a.updater.hwnd != 0 {
			handled, _, _ := user32.NewProc("IsDialogMessageW").Call(uintptr(a.updater.hwnd), uintptr(unsafe.Pointer(&msg)))
			if handled != 0 {
				continue
			}
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (a *App) shutdown() {
	if a.shuttingDown {
		return
	}
	a.shuttingDown = true
	if a.backgroundCancel != nil {
		a.backgroundCancel()
	}
	if a.updater != nil {
		if a.updater.cancel != nil {
			a.updater.cancel()
		}
		if a.updater.hwnd != 0 {
			destroyWindow(a.updater.hwnd)
		}
	}
	if a.popup != nil && a.shellIntegrated {
		unregisterHotkey(a.popup.hwnd, IDHOTKEY)
		trayDelete(a.popup.hwnd)
	}
	if a.manager != nil && a.manager.hwnd != 0 && isWindow(a.manager.hwnd) {
		destroyWindow(a.manager.hwnd)
	}
	if a.popup != nil && a.popup.hwnd != 0 && isWindow(a.popup.hwnd) {
		destroyWindow(a.popup.hwnd)
	}
	if a.mutex != 0 {
		closeHandle(a.mutex)
		a.mutex = 0
	}
	a.dataLock.Close()
	// DestroyWindow normally produces WM_DESTROY, but posting explicitly also
	// terminates the message loop when a window has already disappeared.
	procPostQuitMessage.Call(0)
}

func (a *App) requestExit() {
	if a.manager != nil && a.manager.hwnd != 0 && !a.confirmManagerLeave("退出 PromptDock") {
		return
	}
	a.shutdown()
}

func (a *App) captureTarget() TargetSession {
	return a.captureTargetWindow(foregroundWindow())
}

func (a *App) captureTargetWindow(hwnd HWND) TargetSession {
	if hwnd == 0 || hwnd == a.popup.hwnd || (a.manager != nil && hwnd == a.manager.hwnd) || (a.updater != nil && hwnd == a.updater.hwnd) || getWindowPID(hwnd) == uint32(os.Getpid()) {
		return TargetSession{}
	}
	threadID, pid := getWindowThreadPID(hwnd)
	if threadID == 0 || pid == 0 {
		return TargetSession{}
	}
	focus := getThreadFocus(threadID)
	if focus != 0 && (!isWindow(focus) || getWindowPID(focus) != pid) {
		focus = 0
	}
	return TargetSession{HWND: hwnd, PID: pid, ThreadID: threadID, FocusHWND: focus}
}

func (a *App) alwaysOnTop() bool {
	return a.repo != nil && a.repo.Library.Settings.AlwaysOnTop
}

func (a *App) toggleAlwaysOnTop() {
	if a.repo == nil {
		return
	}
	alwaysOnTop := !a.alwaysOnTop()
	if err := a.repo.SetAlwaysOnTop(alwaysOnTop); err != nil {
		messageBox(a.popup.hwnd, "保存置顶设置失败", err.Error())
		return
	}
	insertAfter := HWND(HWND_NOTOPMOST)
	if alwaysOnTop {
		insertAfter = HWND(HWND_TOPMOST)
	}
	setWindowPos(a.popup.hwnd, insertAfter, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	invalidate(a.popup.hwnd)
}

func (a *App) togglePopup() {
	if a.popup.visible {
		a.hidePopup()
		return
	}
	a.target = a.captureTarget()
	a.showPopup()
}

func (a *App) showPopup() {
	a.popup.visible = true
	a.selectPopupEntry(a.repo.PopupEntry(), false)
	pt := Point{}
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	work := monitorWorkArea(pt.X, pt.Y)
	w, h := int32(340), int32(420)
	x, y := pt.X+12, pt.Y+12
	if x+w > work.Right {
		x = work.Right - w - 8
	}
	if y+h > work.Bottom {
		y = work.Bottom - h - 8
	}
	if x < work.Left {
		x = work.Left + 8
	}
	if y < work.Top {
		y = work.Top + 8
	}
	insertAfter := HWND(HWND_NOTOPMOST)
	if a.alwaysOnTop() {
		insertAfter = HWND(HWND_TOPMOST)
	}
	setWindowPos(a.popup.hwnd, insertAfter, x, y, w, h, SWP_SHOWWINDOW)
	showWindow(a.popup.hwnd, SW_SHOW)
	setForeground(a.popup.hwnd)
	setFocus(a.popup.search)
	invalidate(a.popup.hwnd)
}

func (a *App) selectPopupEntry(entry string, remember bool) {
	p := a.popup
	if p == nil {
		return
	}
	entry = normalizePopupEntry(entry)
	p.mode, p.taskID = entry, ""
	p.selected, p.detailID, p.detailScroll = 0, "", 0
	p.resetListState()
	switch entry {
	case popupEntryFavorite, popupEntryRecent:
		p.view = viewSpecial
	case popupEntryCategories:
		p.view = viewTasks
	}
	p.restoring = true
	p.searchText = ""
	setWindowText(p.search, "")
	p.restoring = false
	if remember {
		_ = a.repo.SetPopupEntry(entry)
	}
	a.syncPopupDetail()
	invalidate(p.hwnd)
}

func (p *Popup) resetListState() { p.listScroll = 0 }

func (a *App) activePopupEntry() string {
	switch a.popup.view {
	case viewTasks, viewTask:
		return popupEntryCategories
	case viewSpecial:
		if a.popup.mode == popupEntryFavorite || a.popup.mode == popupEntryRecent {
			return a.popup.mode
		}
	}
	return ""
}

func (a *App) hidePopup() {
	if a.popup == nil || !a.popup.visible {
		return
	}
	a.popup.visible = false
	showWindow(a.popup.hwnd, SW_HIDE)
	setFocus(0)
}

func (a *App) openManager() {
	a.openManagerPrompt("")
}

func (a *App) ensureManager() {
	if a.manager != nil && a.manager.hwnd != 0 {
		return
	}
	proc := makeCallback(a.managerWndProc)
	a.manager = &Manager{selected: 0}
	a.manager.hwnd = createWindow(0, "PromptDockManager", "PromptDock · 管理提示词", WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX|WS_CLIPCHILDREN, 0, 0, 930, 720, 0, 0, proc)
	if a.manager.hwnd != 0 {
		managerRefresh(a)
	}
}

func (a *App) openManagerPrompt(id string) {
	a.ensureManager()
	if a.manager == nil || a.manager.hwnd == 0 {
		return
	}
	if id != "" && id != a.manager.loadedID {
		if !a.confirmManagerLeave("编辑这条提示词") {
			return
		}
		a.manager.loadingForm = true
		setWindowText(a.manager.search, "")
		a.manager.loadingForm = false
		a.manager.query = ""
		a.manager.loadedID = id
		managerRefresh(a)
	}
	// Only the explicit popup edit action carries an insertion target into
	// the manager. Ordinary tray access does not reuse a stale target.
	if id != "" {
		a.manager.target = a.target
	} else {
		a.manager.target = TargetSession{}
	}
	a.target = TargetSession{}
	a.hidePopup()
	centerWindow(a.manager.hwnd, 930, 720)
	showWindow(a.manager.hwnd, SW_SHOW)
	setForeground(a.manager.hwnd)
}

func (a *App) insertSelected() {
	if a.popup.view == viewDetail {
		a.insertPrompt(a.popup.detailID)
		return
	}
	results := a.popupResults()
	if a.popup.selected < 0 || a.popup.selected >= len(results) {
		return
	}
	a.insertPrompt(results[a.popup.selected].Prompt.ID)
}

func (a *App) insertPrompt(id string) {
	if a.inserting {
		return
	}
	a.inserting = true
	defer func() { a.inserting = false }()
	if err := a.copyPromptBody(a.popup.hwnd, id); err != nil {
		a.showInsertionMessage(a.popup.hwnd, "复制失败", err.Error())
		return
	}
	a.insertCopiedPrompt(a.popup.hwnd, id, a.target, func() {
		a.target = TargetSession{}
		a.hidePopup()
	})
}

func (a *App) copyPromptBody(owner HWND, id string) error {
	p := a.repo.PromptByID(id)
	if p == nil || strings.TrimSpace(p.Body) == "" {
		return errors.New("这条提示词没有可插入的正文。")
	}
	if a.clipboardWriter != nil {
		return a.clipboardWriter(owner, p.Body)
	}
	return copyToClipboard(owner, p.Body)
}

// insertCopiedPrompt waits while the calling window still owns focus. commit
// hides that window and clears its insertion context only after key release.
func (a *App) insertCopiedPrompt(owner HWND, id string, target TargetSession, commit func()) bool {
	if !a.waitForInsertKeys() {
		a.showInsertionMessage(owner, "已复制提示词", "选择按键或修饰键仍未释放，已停止自动粘贴；正文已复制，可在目标输入框中手动粘贴。")
		return false
	}
	commit()
	if !target.Valid() {
		a.showInsertionMessage(0, "已复制提示词", "没有可用的唤出前输入框，请在目标输入框中手动粘贴。")
		return true
	}
	if !isWindow(target.HWND) || getWindowPID(target.HWND) != target.PID {
		a.showInsertionMessage(0, "已复制提示词", "原目标窗口已关闭或发生变化，请手动粘贴。")
		return true
	}
	if !restoreTargetFocus(target) {
		a.showInsertionMessage(0, "已复制提示词", "无法恢复原输入框焦点，请手动粘贴。")
		return true
	}
	sleepMillis(70)
	if foregroundWindow() != target.HWND {
		a.showInsertionMessage(0, "已复制提示词", "原输入窗口没有回到前台，请手动粘贴。")
		return true
	}
	if !a.sendInsertPaste() {
		a.showInsertionMessage(0, "已复制提示词", "已保留在剪贴板，但粘贴命令发送失败，请手动粘贴。")
		return true
	}
	_ = a.repo.RecordUse(id)
	return true
}

func (a *App) waitForInsertKeys() bool {
	if a.releaseWaiter != nil {
		return a.releaseWaiter()
	}
	return waitModifiersReleased()
}

func (a *App) sendInsertPaste() bool {
	if a.pasteSender != nil {
		return a.pasteSender()
	}
	return sendPaste()
}

func (a *App) showInsertionMessage(owner HWND, title, text string) {
	if a.insertionMessage != nil {
		a.insertionMessage(owner, title, text)
		return
	}
	messageBox(owner, title, text)
}

func (a *App) popupResults() []SearchResult {
	query := a.popup.searchText
	if query != "" {
		return a.repo.VisiblePrompts("all", "", query)
	}
	switch a.popup.view {
	case viewTask:
		return a.repo.VisiblePromptsForTasks(a.taskFilterIDs(a.popup.taskID), "")
	case viewSpecial:
		return a.repo.VisiblePrompts(a.popup.mode, "", "")
	default:
		return nil
	}
}

func (a *App) taskFilters() []TaskFilter {
	filters := a.repo.TaskFilters()
	browsable := filters[:0]
	for _, filter := range filters {
		if len(a.repo.VisiblePromptsForTasks(filter.TaskIDs, "")) > 0 {
			browsable = append(browsable, filter)
		}
	}
	return browsable
}

func (a *App) taskFilterIDs(id string) []string {
	for _, filter := range a.taskFilters() {
		if filter.ID == id {
			return filter.TaskIDs
		}
	}
	return nil
}

const (
	popupHeaderHeight  int32 = 39
	popupSearchTop     int32 = 44
	popupSearchHeight  int32 = 27
	popupEntryTop      int32 = 78
	popupEntryHeight   int32 = 31
	popupContentTop    int32 = 115
	popupBottomPadding int32 = 8
	taskRowHeight      int32 = 47
	promptRowHeight    int32 = 58
)

func (a *App) showsTaskNavigation() bool {
	return a.popup.view == viewTasks && a.popup.searchText == ""
}

func (a *App) listRowHeight() int32 {
	if a.showsTaskNavigation() {
		return taskRowHeight
	}
	return promptRowHeight
}

func (a *App) listItemCount() int {
	if a.showsTaskNavigation() {
		return len(a.taskFilters())
	}
	return len(a.popupResults())
}

func (a *App) listVisibleRows() int {
	r := getClientRect(a.popup.hwnd)
	available := r.Bottom - popupContentTop - popupBottomPadding
	if available <= 0 {
		return 1
	}
	rows := int(available / a.listRowHeight())
	if rows < 1 {
		return 1
	}
	return rows
}

func (a *App) maxListScroll() int {
	maxScroll := a.listItemCount() - a.listVisibleRows()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (a *App) clampListScroll() {
	if a.popup.listScroll < 0 {
		a.popup.listScroll = 0
	}
	if maxScroll := a.maxListScroll(); a.popup.listScroll > maxScroll {
		a.popup.listScroll = maxScroll
	}
}

func (a *App) ensureSelectionVisible() {
	if a.popup.selected < a.popup.listScroll {
		a.popup.listScroll = a.popup.selected
	}
	lastVisible := a.popup.listScroll + a.listVisibleRows() - 1
	if a.popup.selected > lastVisible {
		a.popup.listScroll = a.popup.selected - a.listVisibleRows() + 1
	}
	a.clampListScroll()
}

func (a *App) scrollListBy(rows int) {
	a.popup.listScroll += rows
	a.clampListScroll()
	invalidate(a.popup.hwnd)
}

func (a *App) resetListScroll() { a.popup.listScroll = 0 }

func (a *App) showsEmptyReuseState() bool {
	p := a.popup
	return p.searchText == "" && p.view == viewSpecial &&
		(p.mode == popupEntryFavorite || p.mode == popupEntryRecent) && len(a.popupResults()) == 0
}

func (a *App) openEmptyReuseCategories() bool {
	if !a.showsEmptyReuseState() {
		return false
	}
	a.selectPopupEntry(popupEntryCategories, true)
	return true
}

func (a *App) enterSelectedTask() {
	filters := a.taskFilters()
	if !a.showsTaskNavigation() || a.popup.selected < 0 || a.popup.selected >= len(filters) {
		return
	}
	a.popup.view = viewTask
	a.popup.taskID = filters[a.popup.selected].ID
	a.popup.selected = 0
	a.resetListScroll()
	invalidate(a.popup.hwnd)
}

func (a *App) selectedPromptID() string {
	if a.popup.view == viewDetail {
		return a.popup.detailID
	}
	results := a.popupResults()
	if a.popup.selected < 0 || a.popup.selected >= len(results) {
		return ""
	}
	return results[a.popup.selected].Prompt.ID
}

func (a *App) showSelectedDetail() {
	id := a.selectedPromptID()
	if id == "" || a.popup.view == viewDetail {
		return
	}
	p := a.popup
	p.detailFrom = popupLocation{p.view, p.mode, p.taskID, p.searchText, p.selected, p.listScroll}
	a.popup.view = viewDetail
	a.popup.detailID = id
	a.popup.detailScroll = 0
	if prompt := a.repo.PromptByID(id); prompt != nil {
		setMultilineText(p.detailText, prompt.Body+"\n\n—— 用途与来源（不参与插入）——\n\n"+a.repo.promptInfo(*prompt))
	}
	a.syncPopupDetail()
	invalidate(a.popup.hwnd)
}

func (a *App) syncPopupDetail() {
	p := a.popup
	cmd := int32(SW_HIDE)
	if p.view == viewDetail {
		cmd = SW_SHOW
	}
	for _, hwnd := range []HWND{p.detailText, p.detailInsert, p.detailEdit, p.detailCopy} {
		if hwnd != 0 {
			showWindow(hwnd, cmd)
		}
	}
	a.resizePopupSearch()
}

func (a *App) restoreDetailList() {
	p := a.popup
	from := p.detailFrom
	p.view, p.mode, p.taskID, p.searchText = from.view, from.mode, from.taskID, from.searchText
	p.selected, p.listScroll, p.detailID = from.selected, from.listScroll, ""
	p.restoring = true
	setWindowText(p.search, p.searchText)
	p.restoring = false
	a.syncPopupDetail()
}

func (a *App) popupWndProc(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	p := a.popup
	switch msg {
	case WM_CREATE:
		p.hwnd = hwnd
		p.search = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0001, 8, 374, 280, 28, hwnd, ID_SEARCH, 0)
		p.detailText = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_TABSTOP|WS_VSCROLL|0x0004|0x0040|0x0800, 10, 81, 310, 282, hwnd, ID_DETAIL_TEXT, 0)
		p.detailInsert = createButton(hwnd, "插入正文", ID_DETAIL_INSERT, 10, 46, 95, 28)
		p.detailEdit = createButton(hwnd, "编辑", ID_DETAIL_EDIT, 112, 46, 80, 28)
		p.detailCopy = createButton(hwnd, "复制正文", ID_DETAIL_COPY, 199, 46, 100, 28)
		for _, control := range []HWND{p.detailText, p.detailInsert, p.detailEdit, p.detailCopy} {
			sendMessage(control, WM_SETFONT, uiFont(), 1)
			showWindow(control, SW_HIDE)
		}
		sendMessage(p.detailText, EM_SETLIMITTEXT, 0x7ffffffe, 0)
		sendMessage(p.search, WM_SETFONT, uiFont(), 1)
		sendMessage(p.search, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(utf16Ptr("搜索全部提示词…"))))
		a.originalEditProc = setWindowLongPtr(p.search, GWLP_WNDPROC, makeCallback(a.searchWndProc))
		return 0
	case WM_SIZE:
		a.resizePopupSearch()
		invalidate(hwnd)
		return 0
	case WM_PAINT:
		a.paintPopup(hwnd)
		return 0
	case WM_COMMAND:
		switch int(loword(wParam)) {
		case ID_DETAIL_INSERT:
			a.insertSelected()
		case ID_DETAIL_EDIT:
			a.openManagerPrompt(p.detailID)
		case ID_DETAIL_COPY:
			a.copySelected()
		case ID_SEARCH:
			if hiword(wParam) != EN_CHANGE || p.restoring {
				return 0
			}
			query := strings.TrimSpace(getWindowText(p.search))
			if p.view == viewDetail {
				a.restoreDetailList()
				p.restoring = true
				setWindowText(p.search, query)
				p.restoring = false
			}
			p.searchText = query
			p.selected = 0
			p.listScroll = 0
			invalidate(hwnd)
		}
		return 0
	case WM_HOTKEY:
		if int(wParam) == IDHOTKEY {
			a.togglePopup()
		}
		return 0
	case WM_SOFTWARE_UPDATE:
		a.handleSoftwareEvents()
		return 0
	case WM_TRAY:
		trayEvent := trayNotificationEvent(lParam)
		if trayEvent == WM_LBUTTONDBLCLK || trayEvent == WM_LBUTTONUP {
			if !p.visible {
				a.target = a.captureTarget()
			}
			a.showPopup()
			return 0
		}
		if trayEvent == WM_RBUTTONUP || trayEvent == WM_CONTEXTMENU {
			a.showTrayMenu()
			return 0
		}
	case WM_LBUTTONDOWN, WM_LBUTTONDBLCLK:
		a.handlePopupClick(msg == WM_LBUTTONDBLCLK, getX(lParam), getY(lParam))
		return 0
	case WM_RBUTTONUP:
		a.handlePopupRightClick(getX(lParam), getY(lParam))
		return 0
	case WM_MOUSEWHEEL:
		if p.view == viewDetail {
			sendMessage(p.detailText, EM_LINESCROLL, 0, uintptr(-int(wheelDelta(wParam))/40))
		} else {
			delta := -int(wheelDelta(wParam) / 120)
			if delta != 0 {
				a.scrollListBy(delta * 3)
			}
		}
		return 0
	case WM_KEYDOWN:
		if a.handlePopupKey(wParam) {
			return 0
		}
	case WM_NCHITTEST:
		return a.popupHitTest(hwnd, getX(lParam), getY(lParam))
	case WM_ACTIVATE:
		if loword(wParam) != WA_INACTIVE && p.visible {
			// lParam identifies the window that lost activation. Capture it before
			// the popup's search control becomes the new focus owner.
			if target := a.captureTargetWindow(HWND(lParam)); target.Valid() {
				a.target = target
			}
		}
		if loword(wParam) == WA_INACTIVE && p.visible && !a.alwaysOnTop() {
			a.hidePopup()
		}
		return 0
	case WM_CLOSE:
		a.hidePopup()
		return 0
	case WM_DESTROY:
		if !a.shuttingDown {
			a.shutdown()
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	v, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return v
}

func trayNotificationEvent(lParam uintptr) uint32 { return uint32(loword(lParam)) }

func (a *App) searchWndProc(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if a.popup != nil && hwnd == a.popup.search && msg == WM_KEYDOWN && !isImeComposing(hwnd) {
		switch wParam {
		case VK_UP:
			if a.popup.view == viewDetail {
				return 0
			}
			a.moveSelection(-1)
			return 0
		case VK_DOWN:
			if a.popup.view == viewDetail {
				return 0
			}
			a.moveSelection(1)
			return 0
		case VK_RETURN:
			if a.popup.view == viewDetail {
				return 0
			}
			if isKeyDown(VK_CONTROL) {
				a.showSelectedDetail()
				return 0
			}
			if a.showsTaskNavigation() {
				a.enterSelectedTask()
				return 0
			}
			if !a.openEmptyReuseCategories() {
				a.insertSelected()
			}
			return 0
		case VK_ESCAPE:
			a.backOrHide()
			return 0
		case VK_D:
			if isKeyDown(VK_CONTROL) {
				a.toggleFavoriteSelected()
				return 0
			}
		case VK_C:
			if isKeyDown(VK_CONTROL) && !editHasSelection(hwnd) {
				a.copySelected()
				return 0
			}
		}
	}
	return callWindowProc(a.originalEditProc, hwnd, msg, wParam, lParam)
}

func (a *App) resizePopupSearch() {
	if a.popup == nil || a.popup.search == 0 {
		return
	}
	r := getClientRect(a.popup.hwnd)
	setWindowPos(a.popup.search, 0, 10, popupSearchTop, r.Right-20, popupSearchHeight, SWP_NOACTIVATE)
	if a.popup.detailText != 0 {
		buttonTop := popupContentTop + 4
		setWindowPos(a.popup.detailInsert, 0, 10, buttonTop, 95, 28, SWP_NOACTIVATE)
		setWindowPos(a.popup.detailEdit, 0, 112, buttonTop, 80, 28, SWP_NOACTIVATE)
		setWindowPos(a.popup.detailCopy, 0, 199, buttonTop, 100, 28, SWP_NOACTIVATE)
		textTop := buttonTop + 36
		setWindowPos(a.popup.detailText, 0, 10, textTop, r.Right-20, r.Bottom-textTop-popupBottomPadding, SWP_NOACTIVATE)
	}
}

func (a *App) paintPopup(hwnd HWND) {
	var ps PaintStruct
	v, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	hdc := HDC(v)
	r := getClientRect(hwnd)
	bg := createBrush(rgb(250, 250, 252))
	fillRect(hdc, &r, bg)
	deleteObject(uintptr(bg))
	selectObject(hdc, uiFont())
	setBkMode(hdc, TRANSPARENT)
	setTextColor(hdc, rgb(35, 35, 40))
	a.drawPopupHeader(hdc, r.Right)
	a.drawPopupEntries(hdc, r.Right)
	a.drawPopupContent(hdc, r.Right, r.Bottom)
	procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
}

func (a *App) drawPopupHeader(hdc HDC, width int32) {
	header := Rect{Right: width, Bottom: popupHeaderHeight}
	brush := createBrush(rgb(247, 247, 250))
	fillRect(hdc, &header, brush)
	deleteObject(uintptr(brush))
	setTextColor(hdc, rgb(35, 35, 40))
	if a.popup.view == viewTask || a.popup.view == viewDetail {
		drawText(hdc, "‹", &Rect{Left: 10, Top: 7, Right: 32, Bottom: 32}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	}
	title := "PromptDock"
	if a.popup.view == viewTask {
		title = a.repo.TaskTitle(a.popup.taskID)
	}
	if a.popup.view == viewSpecial {
		switch a.popup.mode {
		case "favorite":
			title = "收藏"
		case "recent":
			title = "最近调用"
		default:
			if a.popup.mode == "legacy" {
				title = "旧开发示例"
			} else {
				title = "全部提示词"
			}
		}
	}
	if a.popup.view == viewDetail {
		if p := a.repo.PromptByID(a.popup.detailID); p != nil {
			title = p.Title
		}
	}
	titleLeft := int32(12)
	if a.popup.view == viewTask || a.popup.view == viewDetail {
		titleLeft = 38
	}
	drawText(hdc, title, &Rect{Left: titleLeft, Top: 8, Right: width - 116, Bottom: 31}, DT_SINGLELINE|DT_END_ELLIPSIS|DT_VCENTER)
	drawText(hdc, "⋯", &Rect{Left: width - 116, Top: 7, Right: width - 80, Bottom: 32}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	pinColor := rgb(142, 142, 152)
	if a.alwaysOnTop() {
		pinColor = rgb(45, 105, 185)
	}
	setTextColor(hdc, pinColor)
	drawText(hdc, "置顶", &Rect{Left: width - 78, Top: 7, Right: width - 40, Bottom: 32}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	setTextColor(hdc, rgb(35, 35, 40))
	drawText(hdc, "×", &Rect{Left: width - 31, Top: 7, Right: width - 9, Bottom: 32}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
}

func (a *App) drawPopupEntries(hdc HDC, width int32) {
	bar := Rect{Left: 0, Top: popupEntryTop - 4, Right: width, Bottom: popupContentTop - 5}
	brush := createBrush(rgb(247, 247, 250))
	fillRect(hdc, &bar, brush)
	deleteObject(uintptr(brush))
	entries := []struct {
		id, title string
	}{
		{popupEntryFavorite, "收藏"},
		{popupEntryRecent, "最近"},
		{popupEntryCategories, "分类"},
	}
	left, totalWidth := int32(8), width-int32(16)
	entryWidth := totalWidth / int32(len(entries))
	active := a.activePopupEntry()
	for i, entry := range entries {
		right := left + entryWidth
		if i == len(entries)-1 {
			right = width - 8
		}
		rect := Rect{Left: left, Top: popupEntryTop, Right: right, Bottom: popupEntryTop + popupEntryHeight}
		if active == entry.id {
			selected := createBrush(rgb(228, 237, 252))
			fillRect(hdc, &rect, selected)
			deleteObject(uintptr(selected))
			setTextColor(hdc, rgb(45, 105, 185))
		} else {
			setTextColor(hdc, rgb(80, 80, 90))
		}
		drawText(hdc, entry.title, &rect, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
		left = right
	}
}

func (a *App) drawPopupContent(hdc HDC, width, height int32) {
	if a.popup.view == viewDetail {
		a.drawDetail(hdc, width, height)
		return
	}
	if a.showsTaskNavigation() {
		filters := a.taskFilters()
		a.clampListScroll()
		end := a.popup.listScroll + a.listVisibleRows()
		if end > len(filters) {
			end = len(filters)
		}
		for i := a.popup.listScroll; i < end; i++ {
			task := filters[i]
			top := popupContentTop + int32(i-a.popup.listScroll)*taskRowHeight
			a.drawRow(hdc, Rect{Left: 0, Top: top, Right: width, Bottom: top + 46}, task.Title, "", i == a.popup.selected, true, false)
		}
		a.drawListScrollbar(hdc, width, height)
		return
	}
	results := a.popupResults()
	if len(results) == 0 {
		if a.showsEmptyReuseState() {
			setTextColor(hdc, rgb(120, 120, 128))
			message := "还没有收藏，去分类看看"
			if a.popup.mode == popupEntryRecent {
				message = "还没有使用记录，去分类看看"
			}
			drawText(hdc, message, &Rect{Left: 18, Top: popupContentTop + 18, Right: width - 18, Bottom: popupContentTop + 45}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
			setTextColor(hdc, rgb(45, 105, 185))
			drawText(hdc, "去分类看看  ›", &Rect{Left: 18, Top: popupContentTop + 48, Right: width - 18, Bottom: popupContentTop + 75}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
		} else {
			setTextColor(hdc, rgb(120, 120, 128))
			drawText(hdc, "没有找到，试试更短的词", &Rect{Left: 18, Top: popupContentTop + 18, Right: width - 18, Bottom: popupContentTop + 52}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
		}
		return
	}
	a.clampListScroll()
	end := a.popup.listScroll + a.listVisibleRows()
	if end > len(results) {
		end = len(results)
	}
	for i := a.popup.listScroll; i < end; i++ {
		result := results[i]
		top := popupContentTop + int32(i-a.popup.listScroll)*promptRowHeight
		scenario := result.Prompt.Description
		if strings.TrimSpace(scenario) == "" {
			scenario = "未填写使用场景"
		}
		a.drawRow(hdc, Rect{Left: 0, Top: top, Right: width, Bottom: top + 57}, result.Prompt.Title, scenario, i == a.popup.selected, false, result.Prompt.Favorite)
	}
	a.drawListScrollbar(hdc, width, height)
}

func (a *App) drawListScrollbar(hdc HDC, width, height int32) {
	count, visible := a.listItemCount(), a.listVisibleRows()
	if count <= visible {
		return
	}
	top, bottom := popupContentTop, height-popupBottomPadding
	track := Rect{Left: width - 5, Top: top + 4, Right: width - 2, Bottom: bottom - 4}
	trackBrush := createBrush(rgb(232, 232, 236))
	fillRect(hdc, &track, trackBrush)
	deleteObject(uintptr(trackBrush))
	trackHeight := track.Bottom - track.Top
	thumbHeight := trackHeight * int32(visible) / int32(count)
	if thumbHeight < 18 {
		thumbHeight = 18
	}
	maxScroll := a.maxListScroll()
	thumbTop := track.Top
	if maxScroll > 0 {
		thumbTop += (trackHeight - thumbHeight) * int32(a.popup.listScroll) / int32(maxScroll)
	}
	thumb := Rect{Left: track.Left, Top: thumbTop, Right: track.Right, Bottom: thumbTop + thumbHeight}
	thumbBrush := createBrush(rgb(160, 160, 170))
	fillRect(hdc, &thumb, thumbBrush)
	deleteObject(uintptr(thumbBrush))
}

func (a *App) drawRow(hdc HDC, rect Rect, title, description string, selected, task, favorite bool) {
	if selected {
		brush := createBrush(rgb(228, 237, 252))
		fillRect(hdc, &rect, brush)
		deleteObject(uintptr(brush))
	}
	setTextColor(hdc, rgb(38, 38, 44))
	titleRect, descriptionRect := popupRowTextRects(rect, selected, task, favorite)
	drawText(hdc, title, &titleRect, DT_SINGLELINE|DT_END_ELLIPSIS|DT_VCENTER|DT_NOPREFIX)
	if description != "" {
		setTextColor(hdc, rgb(112, 112, 122))
		drawText(hdc, description, &descriptionRect, DT_SINGLELINE|DT_END_ELLIPSIS|DT_VCENTER|DT_NOPREFIX)
	}
	setTextColor(hdc, rgb(100, 100, 110))
	if task {
		drawText(hdc, "›", &Rect{Left: rect.Right - 31, Top: rect.Top + 7, Right: rect.Right - 10, Bottom: rect.Top + 30}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	}
	if favorite && !task {
		drawText(hdc, "★", &Rect{Left: rect.Right - 56, Top: rect.Top + 7, Right: rect.Right - 37, Bottom: rect.Top + 28}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	}
	if selected && !task {
		drawText(hdc, "插入", &Rect{Left: rect.Right - 43, Top: rect.Top + 28, Right: rect.Right - 7, Bottom: rect.Bottom - 5}, DT_SINGLELINE|DT_CENTER|DT_VCENTER)
	}
}

func (a *App) drawDetail(hdc HDC, width, height int32) {
	// Native read-only EDIT renders and scrolls the complete text and provenance.
}

func (a *App) popupHitTest(hwnd HWND, x, y int32) uintptr {
	r := getWindowRect(hwnd)
	// Coordinates here are screen coordinates; convert to the window origin.
	lx, ly := x-r.Left, y-r.Top
	if ly < popupHeaderHeight && lx > 36 && lx < r.Right-r.Left-116 {
		return HTCAPTION
	}
	v, _, _ := procDefWindowProcW.Call(uintptr(hwnd), WM_NCHITTEST, 0, uintptr(uint32(uint16(x))|uint32(uint16(y))<<16))
	return v
}

func (a *App) handlePopupClick(doubleClick bool, x, y int32) {
	r := getClientRect(a.popup.hwnd)
	if y < popupHeaderHeight {
		if x > r.Right-42 {
			a.hidePopup()
			return
		}
		if x >= r.Right-80 && x <= r.Right-42 {
			a.toggleAlwaysOnTop()
			return
		}
		if x >= r.Right-116 && x < r.Right-80 {
			a.showPopupMenu()
			return
		}
		if x < 37 && (a.popup.view == viewTask || a.popup.view == viewDetail) {
			a.backOrHide()
			return
		}
		return
	}
	if y >= popupEntryTop && y < popupContentTop {
		entryWidth := (r.Right - 16) / 3
		idx := int((x - 8) / entryWidth)
		if idx < 0 {
			idx = 0
		}
		if idx > 2 {
			idx = 2
		}
		a.selectPopupEntry([]string{popupEntryFavorite, popupEntryRecent, popupEntryCategories}[idx], true)
		return
	}
	if a.popup.view == viewDetail {
		return
	}
	if a.showsTaskNavigation() {
		idx := a.popup.listScroll + int((y-popupContentTop)/taskRowHeight)
		if idx >= 0 && idx < len(a.taskFilters()) {
			a.popup.selected = idx
			a.enterSelectedTask()
		}
		return
	}
	results := a.popupResults()
	if len(results) == 0 && a.openEmptyReuseCategories() {
		return
	}
	idx := a.popup.listScroll + int((y-popupContentTop)/promptRowHeight)
	if idx >= 0 && idx < len(results) {
		wasSelected := a.popup.selected == idx
		a.popup.selected = idx
		rowTop := popupContentTop + int32(idx-a.popup.listScroll)*promptRowHeight
		if doubleClick || (wasSelected && x > r.Right-47 && y >= rowTop+28) {
			a.insertSelected()
		}
		invalidate(a.popup.hwnd)
	}
}

func (a *App) handlePopupRightClick(x, y int32) {
	if a.popup.view == viewDetail || a.showsTaskNavigation() {
		return
	}
	results := a.popupResults()
	idx := a.popup.listScroll + int((y-popupContentTop)/promptRowHeight)
	if idx < 0 || idx >= len(results) {
		return
	}
	a.popup.selected = idx
	a.showPromptContext(results[idx].Prompt.ID)
}

func (a *App) handlePopupKey(key uintptr) bool {
	if isImeComposing(a.popup.hwnd) {
		return false
	}
	if key == VK_RETURN && isKeyDown(VK_CONTROL) {
		a.showSelectedDetail()
		return true
	}
	switch key {
	case VK_ESCAPE:
		a.backOrHide()
		return true
	case VK_RETURN:
		if a.showsTaskNavigation() {
			a.enterSelectedTask()
		} else if a.popup.view == viewDetail {
			return true
		} else if !a.openEmptyReuseCategories() {
			a.insertSelected()
		}
		return true
	case VK_UP:
		if a.popup.view == viewDetail {
			sendMessage(a.popup.detailText, EM_LINESCROLL, 0, ^uintptr(0))
			return true
		}
		a.moveSelection(-1)
		return true
	case VK_DOWN:
		if a.popup.view == viewDetail {
			sendMessage(a.popup.detailText, EM_LINESCROLL, 0, 1)
			return true
		}
		a.moveSelection(1)
		return true
	case VK_C:
		if isKeyDown(VK_CONTROL) {
			a.copySelected()
			return true
		}
	case VK_D:
		if isKeyDown(VK_CONTROL) {
			a.toggleFavoriteSelected()
			return true
		}
	case VK_PRIOR:
		if a.popup.view == viewDetail {
			sendMessage(a.popup.detailText, EM_LINESCROLL, 0, ^uintptr(9))
			return true
		}
	case VK_NEXT:
		if a.popup.view == viewDetail {
			sendMessage(a.popup.detailText, EM_LINESCROLL, 0, 10)
			return true
		}
	}
	return false
}

func (a *App) moveSelection(delta int) {
	if a.showsTaskNavigation() {
		max := len(a.taskFilters()) - 1
		a.popup.selected += delta
		if a.popup.selected < 0 {
			a.popup.selected = max
		}
		if a.popup.selected > max {
			a.popup.selected = 0
		}
	} else {
		max := len(a.popupResults()) - 1
		if max < 0 {
			return
		}
		a.popup.selected += delta
		if a.popup.selected < 0 {
			a.popup.selected = max
		}
		if a.popup.selected > max {
			a.popup.selected = 0
		}
	}
	a.ensureSelectionVisible()
	invalidate(a.popup.hwnd)
}

func (a *App) backOrHide() {
	if a.popup.view == viewDetail {
		a.restoreDetailList()
		invalidate(a.popup.hwnd)
		return
	}
	if a.popup.searchText != "" {
		a.popup.searchText = ""
		setWindowText(a.popup.search, "")
		a.resetListScroll()
		invalidate(a.popup.hwnd)
		return
	}
	if a.popup.view == viewTask {
		a.selectPopupEntry(popupEntryCategories, false)
		return
	}
	a.hidePopup()
}

func (a *App) copySelected() {
	id := a.selectedPromptID()
	if p := a.repo.PromptByID(id); p != nil {
		if err := copyToClipboard(a.popup.hwnd, p.Body); err != nil {
			messageBox(a.popup.hwnd, "复制失败", err.Error())
		}
	}
}

func (a *App) toggleFavoriteSelected() {
	id := a.selectedPromptID()
	if id != "" {
		if err := a.repo.ToggleFavorite(id); err != nil {
			messageBox(a.popup.hwnd, "收藏失败", err.Error())
		}
		invalidate(a.popup.hwnd)
	}
}

func (a *App) showPromptContext(id string) {
	menu := createPopupMenu()
	appendMenu(menu, 3001, "查看全文")
	appendMenu(menu, 3002, "复制提示词")
	appendMenu(menu, 3003, "收藏 / 取消收藏")
	appendMenu(menu, 3004, "编辑提示词")
	pt := Point{}
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd := trackPopupMenu(menu, pt.X, pt.Y, a.popup.hwnd)
	switch cmd {
	case 3001:
		a.popup.detailID = id
		a.showSelectedDetail()
	case 3002:
		if p := a.repo.PromptByID(id); p != nil {
			_ = copyToClipboard(a.popup.hwnd, p.Body)
		}
	case 3003:
		if err := a.repo.ToggleFavorite(id); err != nil {
			messageBox(a.popup.hwnd, "收藏失败", err.Error())
		}
		invalidate(a.popup.hwnd)
	case 3004:
		a.openManagerPrompt(id)
	}
	destroyMenu(menu)
}

func (a *App) showPopupMenu() {
	menu := createPopupMenu()
	appendMenu(menu, 3101, "收藏")
	appendMenu(menu, 3102, "最近调用")
	appendMenu(menu, 3103, "所有提示词")
	appendMenu(menu, 3104, "按分类筛选")
	appendMenu(menu, 3105, "查看旧开发示例")
	pinLabel := "开启始终置顶"
	if a.alwaysOnTop() {
		pinLabel = "取消始终置顶"
	}
	appendMenu(menu, 3106, pinLabel)
	appendMenu(menu, 3107, "管理提示词")
	appendMenu(menu, ID_SOFTWARE_UPDATE, "检查软件更新")
	appendMenu(menu, 3108, "退出 PromptDock")
	pt := Point{}
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd := trackPopupMenu(menu, pt.X, pt.Y, a.popup.hwnd)
	switch cmd {
	case 3101:
		a.selectPopupEntry(popupEntryFavorite, true)
	case 3102:
		a.selectPopupEntry(popupEntryRecent, true)
	case 3103:
		a.popup.view, a.popup.mode, a.popup.selected = viewSpecial, "all", 0
		a.popup.searchText = ""
		a.resetListScroll()
		invalidate(a.popup.hwnd)
	case 3104:
		a.selectPopupEntry(popupEntryCategories, true)
	case 3105:
		a.popup.view, a.popup.mode, a.popup.selected = viewSpecial, "legacy", 0
		a.popup.searchText = ""
		a.resetListScroll()
		setWindowText(a.popup.search, "")
		invalidate(a.popup.hwnd)
	case 3106:
		a.toggleAlwaysOnTop()
	case 3107:
		a.openManager()
	case ID_SOFTWARE_UPDATE:
		a.openSoftwareUpdate()
	case 3108:
		a.requestExit()
	}
	if !a.shuttingDown {
		a.syncPopupDetail()
	}
	destroyMenu(menu)
}

func (a *App) showTrayMenu() {
	menu := createPopupMenu()
	appendMenu(menu, TRAY_SHOW, "打开 PromptDock")
	pinLabel := "开启始终置顶"
	if a.alwaysOnTop() {
		pinLabel = "取消始终置顶"
	}
	appendMenu(menu, TRAY_TOGGLE_TOPMOST, pinLabel)
	appendMenu(menu, TRAY_MANAGE, "管理提示词")
	appendMenu(menu, ID_SOFTWARE_UPDATE, "检查软件更新")
	appendMenu(menu, TRAY_EXIT, "退出")
	pt := Point{}
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	target := a.captureTarget()
	setForeground(a.popup.hwnd)
	cmd := trackPopupMenu(menu, pt.X, pt.Y, a.popup.hwnd)
	switch cmd {
	case TRAY_SHOW:
		a.target = target
		a.showPopup()
	case TRAY_TOGGLE_TOPMOST:
		a.toggleAlwaysOnTop()
	case TRAY_MANAGE:
		a.openManager()
	case ID_SOFTWARE_UPDATE:
		a.openSoftwareUpdate()
	case TRAY_EXIT:
		a.requestExit()
	}
	destroyMenu(menu)
}

func (a *App) managerWndProc(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	m := a.manager
	switch msg {
	case WM_CREATE:
		m.hwnd = hwnd
		a.createManagerControls(hwnd)
		return 0
	case WM_SIZE:
		a.resizeManager()
		return 0
	case WM_COMMAND:
		a.handleManagerCommand(int(loword(wParam)), int(hiword(wParam)))
		return 0
	case 0x002B: // WM_DRAWITEM, fixed-height manager list rows.
		if a.drawManagerItem(lParam) {
			return 1
		}
	case WM_CLOSE:
		if a.shuttingDown || a.confirmManagerLeave("关闭管理窗口") {
			showWindow(hwnd, SW_HIDE)
		}
		return 0
	case WM_DESTROY:
		m.hwnd = 0
		return 0
	}
	v, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return v
}

func (a *App) createManagerControls(hwnd HWND) {
	m := a.manager
	editStyle := uint32(WS_CHILD | WS_VISIBLE | WS_TABSTOP | 0x0080)
	m.search = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", editStyle, 12, 35, 248, 26, hwnd, ID_MANAGER_SEARCH, 0)
	sendMessage(m.search, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(utf16Ptr("搜索标题、用途、场景标签…"))))
	// LBS_NOTIFY, without LBS_SORT: m.ids must follow the displayed order.
	m.list = createWindow(WS_EX_CLIENTEDGE, "LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_VSCROLL|WS_TABSTOP|0x0001|0x0100|0x0010|0x0040, 12, 70, 248, 480, hwnd, ID_MANAGER_LIST, 0)
	m.title = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", editStyle, 280, 32, 600, 26, hwnd, ID_MANAGER_TITLE, 0)
	m.description = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", editStyle, 280, 86, 600, 26, hwnd, ID_MANAGER_DESC, 0)
	m.task = createWindow(WS_EX_CLIENTEDGE, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_VSCROLL|0x0003, 280, 142, 238, 240, hwnd, ID_MANAGER_TASK, 0)
	m.tags = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", editStyle, 530, 142, 350, 26, hwnd, ID_MANAGER_TAGS, 0)
	sendMessage(m.tags, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(utf16Ptr("例如：论文、学术润色（用逗号分隔）"))))
	m.body = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_VSCROLL|0x0004|0x0040|0x1000, 280, 198, 600, 230, hwnd, ID_MANAGER_BODY, 0)
	m.source = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_TABSTOP|WS_VSCROLL|0x0004|0x0040|0x0800, 280, 456, 600, 116, hwnd, ID_MANAGER_SOURCE, 0)
	for _, control := range []HWND{m.title, m.description, m.tags, m.body, m.source} {
		sendMessage(control, EM_SETLIMITTEXT, 0x7ffffffe, 0)
	}
	for _, control := range []HWND{m.list, m.search, m.title, m.description, m.body, m.task, m.tags, m.source} {
		sendMessage(control, WM_SETFONT, uiFont(), 1)
	}
	sendMessage(m.list, LB_SETITEMHEIGHT, 0, 48)
	for _, label := range []string{"提示词", "标题", "一句话用途", "任务分类", "场景标签", "完整正文（仅这部分会插入）", "作者、来源、许可与版本说明（可滚动查看）"} {
		m.labels = append(m.labels, createStatic(hwnd, label, 0, 0, 400, 22))
	}
	m.buttons = make(map[int]HWND)
	for _, button := range []struct {
		id   int
		text string
	}{
		{ID_MANAGER_NEW, "新建"}, {ID_MANAGER_DELETE, "删除"}, {ID_MANAGER_FAV, "收藏"},
		{ID_MANAGER_SAVE, "保存"}, {ID_MANAGER_COPY, "复制正文"}, {ID_MANAGER_INSERT, "保存并插入"},
		{ID_MANAGER_SOURCE_TOGGLE, "展开来源信息"},
		{ID_MANAGER_IMPORT, "导入 JSON"}, {ID_MANAGER_EXPORT, "导出 JSON"},
		{ID_MANAGER_PACK, "检查内置内容更新"}, {ID_MANAGER_EXIT, "退出程序"},
		{ID_SOFTWARE_UPDATE, "检查软件更新"},
	} {
		m.buttons[button.id] = createButton(hwnd, button.text, button.id, 0, 0, 100, 30)
	}
	a.resizeManager()
}

func createStatic(parent HWND, text string, x, y, w, h int32) HWND {
	hwnd := createWindow(0, "STATIC", text, WS_CHILD|WS_VISIBLE, x, y, w, h, parent, 0, 0)
	sendMessage(hwnd, WM_SETFONT, uiFont(), 1)
	return hwnd
}
func createButton(parent HWND, text string, id int, x, y, w, h int32) HWND {
	hwnd := createWindow(0, "BUTTON", text, WS_CHILD|WS_VISIBLE|WS_TABSTOP, x, y, w, h, parent, uintptr(id), 0)
	sendMessage(hwnd, WM_SETFONT, uiFont(), 1)
	return hwnd
}

func (a *App) resizeManager() {
	m := a.manager
	if m == nil || m.hwnd == 0 || m.source == 0 || len(m.labels) != 7 {
		return
	}
	r := getClientRect(m.hwnd)
	move := func(hwnd HWND, x, y, w, h int32) {
		setWindowPos(hwnd, 0, x, y, w, h, SWP_NOACTIVATE|0x0004 /* SWP_NOZORDER */)
	}
	move(m.search, 12, 35, 248, 26)
	move(m.list, 12, 70, 248, r.Bottom-232)
	move(m.title, 280, 32, r.Right-296, 26)
	move(m.description, 280, 86, r.Right-296, 26)
	move(m.task, 280, 142, 238, 240)
	move(m.tags, 530, 142, r.Right-546, 26)
	bodyHeight := r.Bottom - 294
	if m.sourceExpanded {
		bodyHeight = r.Bottom - 422
		move(m.source, 280, r.Bottom-195, r.Right-296, 111)
	}
	move(m.body, 280, 198, r.Right-296, bodyHeight)
	positions := [][2]int32{{12, 12}, {280, 12}, {280, 66}, {280, 122}, {530, 122}, {280, 178}, {280, r.Bottom - 217}}
	for i, p := range positions {
		if i != 6 || m.sourceExpanded {
			width := r.Right - p[0] - 16
			if i == 0 {
				width = 248
			}
			if i == 3 {
				width = 238
			}
			move(m.labels[i], p[0], p[1], width, 22)
		}
	}
	sourceCmd := int32(SW_HIDE)
	if m.sourceExpanded {
		sourceCmd = SW_SHOW
	}
	showWindow(m.source, sourceCmd)
	showWindow(m.labels[6], sourceCmd)
	if m.sourceExpanded {
		setWindowText(m.buttons[ID_MANAGER_SOURCE_TOGGLE], "收起来源信息")
	} else {
		setWindowText(m.buttons[ID_MANAGER_SOURCE_TOGGLE], "展开来源信息")
	}
	move(m.buttons[ID_MANAGER_NEW], 12, r.Bottom-116, 76, 30)
	move(m.buttons[ID_MANAGER_DELETE], 98, r.Bottom-116, 76, 30)
	move(m.buttons[ID_MANAGER_FAV], 184, r.Bottom-116, 76, 30)
	move(m.buttons[ID_MANAGER_SAVE], 280, r.Bottom-74, 86, 30)
	move(m.buttons[ID_MANAGER_COPY], 378, r.Bottom-74, 100, 30)
	move(m.buttons[ID_MANAGER_INSERT], 490, r.Bottom-74, 128, 30)
	move(m.buttons[ID_MANAGER_SOURCE_TOGGLE], r.Right-184, r.Bottom-74, 168, 30)
	move(m.buttons[ID_MANAGER_IMPORT], 12, r.Bottom-152, 118, 30)
	move(m.buttons[ID_MANAGER_EXPORT], 142, r.Bottom-152, 118, 30)
	move(m.buttons[ID_MANAGER_PACK], 12, r.Bottom-80, 248, 30)
	move(m.buttons[ID_SOFTWARE_UPDATE], 12, r.Bottom-38, 164, 30)
	move(m.buttons[ID_MANAGER_EXIT], r.Right-116, r.Bottom-38, 100, 30)
}

func managerRefresh(a *App) {
	m := a.manager
	if m == nil || m.list == 0 || m.dirty || m.isNew {
		return
	}
	previousID := m.loadedID
	m.loadingForm = true
	sendMessage(m.list, 0x0184, 0, 0)
	sendMessage(m.task, 0x014B, 0, 0) // CB_RESETCONTENT
	m.taskIDs = nil
	for _, filter := range a.repo.TaskFilters() {
		m.taskIDs = append(m.taskIDs, filter.ID)
		sendMessage(m.task, 0x0143, 0, uintptr(unsafe.Pointer(utf16Ptr(filter.Title))))
	}
	m.ids = nil
	for _, result := range a.repo.VisiblePrompts("all", "", m.query) {
		p := result.Prompt
		m.ids = append(m.ids, p.ID)
		text := p.Title + "  ·  " + a.repo.TaskTitle(p.PrimaryTaskID)
		if strings.Contains(p.ID, "-import-") {
			text += "（导入副本）"
		}
		sendMessage(m.list, 0x0180, 0, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	if len(m.ids) == 0 {
		m.selected = -1
		m.loadedID = ""
		for _, control := range []HWND{m.title, m.description, m.body, m.tags, m.source} {
			setWindowText(control, "")
		}
		sendMessage(m.task, 0x014E, ^uintptr(0), 0)
	} else {
		m.selected = 0
		for i, id := range m.ids {
			if id == previousID {
				m.selected = i
				break
			}
		}
		sendMessage(m.list, 0x0186, uintptr(m.selected), 0)
	}
	m.loadingForm = false
	m.taskChanged = false
	for _, control := range []HWND{m.title, m.description, m.body, m.task, m.tags,
		m.buttons[ID_MANAGER_SAVE], m.buttons[ID_MANAGER_COPY], m.buttons[ID_MANAGER_INSERT],
		m.buttons[ID_MANAGER_DELETE], m.buttons[ID_MANAGER_FAV]} {
		enableWindow(control, len(m.ids) > 0)
	}
	managerLoadForm(a)
}

func enableManagerDraftControls(m *Manager) {
	for _, control := range []HWND{m.title, m.description, m.body, m.task, m.tags,
		m.buttons[ID_MANAGER_SAVE], m.buttons[ID_MANAGER_COPY], m.buttons[ID_MANAGER_INSERT]} {
		enableWindow(control, true)
	}
	// A draft has no persisted prompt to delete or favorite yet.
	for _, control := range []HWND{m.buttons[ID_MANAGER_DELETE], m.buttons[ID_MANAGER_FAV]} {
		enableWindow(control, false)
	}
}

func managerLoadForm(a *App) {
	m := a.manager
	if m == nil || m.selected < 0 || m.selected >= len(m.ids) {
		return
	}
	p := a.repo.PromptByID(m.ids[m.selected])
	if p == nil {
		return
	}
	m.loadingForm = true
	defer func() {
		m.loadedID = p.ID
		m.dirty = false
		m.isNew = false
		m.taskChanged = false
		m.loadingForm = false
	}()
	setWindowText(m.title, p.Title)
	setWindowText(m.description, p.Description)
	setMultilineText(m.body, p.Body)
	setWindowText(m.tags, strings.Join(p.Tags, "、"))
	setMultilineText(m.source, a.repo.promptInfo(*p))
	idx := -1
	for i, id := range m.taskIDs {
		if id == canonicalTaskID(p.PrimaryTaskID) {
			idx = i
			break
		}
	}
	sendMessage(m.task, 0x014E, uintptr(idx), 0)
}

func (a *App) handleManagerCommand(id, notify int) {
	m := a.manager
	if m == nil {
		return
	}
	switch id {
	case ID_MANAGER_SEARCH:
		if notify == EN_CHANGE && !m.loadingForm {
			query := strings.TrimSpace(getWindowText(m.search))
			if !a.confirmManagerLeave("筛选提示词") {
				m.loadingForm = true
				setWindowText(m.search, m.query)
				m.loadingForm = false
				return
			}
			m.query = query
			managerRefresh(a)
		}
	case ID_MANAGER_LIST:
		if notify == LBN_SELCHANGE {
			v := int(sendMessage(m.list, 0x0188, 0, 0))
			if v >= 0 && v < len(m.ids) && v != m.selected {
				targetID := m.ids[v]
				if !a.confirmManagerLeave("切换到另一条提示词") {
					sendMessage(m.list, 0x0186, uintptr(m.selected), 0)
					return
				}
				for i, promptID := range m.ids {
					if promptID == targetID {
						m.selected = i
						sendMessage(m.list, 0x0186, uintptr(i), 0)
						managerLoadForm(a)
						break
					}
				}
			}
		}
	case ID_MANAGER_TITLE, ID_MANAGER_DESC, ID_MANAGER_BODY, ID_MANAGER_TASK, ID_MANAGER_TAGS:
		if !m.loadingForm && (notify == EN_CHANGE || (id == ID_MANAGER_TASK && notify == LBN_SELCHANGE)) {
			m.dirty = true
			if id == ID_MANAGER_TASK {
				m.taskChanged = true
			}
		}
	case ID_MANAGER_SAVE:
		a.managerSave(true)
	case ID_MANAGER_COPY:
		if body := getWindowText(m.body); strings.TrimSpace(body) != "" {
			if err := copyToClipboard(m.hwnd, body); err != nil {
				messageBox(m.hwnd, "复制失败", err.Error())
			}
		}
	case ID_MANAGER_INSERT:
		if err := a.managerInsert(); err != nil {
			messageBox(m.hwnd, "无法插入", err.Error())
		}
	case ID_MANAGER_SOURCE_TOGGLE:
		m.sourceExpanded = !m.sourceExpanded
		a.resizeManager()
	case ID_MANAGER_NEW:
		if !a.confirmManagerLeave("新建提示词") {
			return
		}
		a.managerNewDraft()
	case ID_MANAGER_DELETE:
		id := m.loadedID
		if id != "" && a.confirmManagerLeave("删除提示词") && confirmBox(m.hwnd, "删除提示词", "确定从列表移除这条提示词吗？本地会保留删除记录，后续导入不会自动恢复。") {
			if err := a.repo.DeletePrompt(id); err == nil {
				if m.selected > 0 {
					m.selected--
				}
				managerRefresh(a)
			} else {
				messageBox(m.hwnd, "删除失败", err.Error())
			}
		}
	case ID_MANAGER_FAV:
		id := m.loadedID
		if id != "" {
			if err := a.repo.ToggleFavorite(id); err != nil {
				messageBox(m.hwnd, "收藏失败", err.Error())
			}
		}
	case ID_MANAGER_EXPORT:
		if !a.confirmManagerLeave("导出已保存的提示词") {
			return
		}
		path := openFileDialog(m.hwnd, true)
		if path != "" {
			if err := a.repo.Export(path, true); err != nil {
				messageBox(m.hwnd, "导出失败", err.Error())
			} else {
				messageBox(m.hwnd, "导出完成", "已写入 JSON 文件；导出包含收藏与最近使用信息。")
			}
		}
	case ID_MANAGER_IMPORT:
		path := openFileDialog(m.hwnd, false)
		if path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				messageBox(m.hwnd, "读取失败", err.Error())
			} else {
				a.managerImport(data)
			}
		}
	case ID_MANAGER_PACK:
		a.managerImport(bundledContent)
	case ID_SOFTWARE_UPDATE:
		a.openSoftwareUpdate()
	case ID_MANAGER_EXIT:
		a.requestExit()
	}
}

func (a *App) managerNewDraft() {
	m := a.manager
	if m == nil {
		return
	}
	m.loadedID = fmt.Sprintf("personal-%d", time.Now().UnixNano())
	m.selected = -1
	m.isNew = true
	m.dirty = false
	m.taskChanged = false
	m.query = ""
	m.loadingForm = true
	sendMessage(m.list, 0x0186, ^uintptr(0), 0)
	setWindowText(m.search, "")
	setWindowText(m.title, "")
	setWindowText(m.description, "")
	setMultilineText(m.body, "")
	setWindowText(m.tags, "")
	setMultilineText(m.source, "个人提示词尚无来源信息。")
	for i, id := range m.taskIDs {
		if id == unclassifiedTaskID {
			sendMessage(m.task, 0x014E, uintptr(i), 0)
			break
		}
	}
	m.loadingForm = false
	enableManagerDraftControls(m)
	setFocus(m.title)
}

// managerInsert keeps the editor and its original target available until the
// body has reached the clipboard. This lets a clipboard-busy failure be
// retried in place instead of discarding the insertion context.
func (a *App) managerInsert() error {
	m := a.manager
	if m == nil || m.loadedID == "" {
		return errors.New("没有可插入的提示词")
	}
	if a.inserting {
		return errors.New("正在插入，请稍候")
	}
	if !a.managerSave(false) {
		return errors.New("提示词保存失败，未开始插入")
	}
	if err := a.copyPromptBody(m.hwnd, m.loadedID); err != nil {
		return err
	}
	a.inserting = true
	defer func() { a.inserting = false }()
	target := m.target
	a.insertCopiedPrompt(m.hwnd, m.loadedID, target, func() {
		m.target = TargetSession{}
		showWindow(m.hwnd, SW_HIDE)
	})
	return nil
}

func countImportConflicts(r *Repository, data []byte) int {
	report, _ := r.PreviewImport(data)
	return len(report.Conflicts)
}

func (a *App) managerImport(data []byte) {
	m := a.manager
	if !a.confirmManagerLeave("导入提示词") {
		return
	}
	report, err := a.repo.PreviewImport(data)
	if err != nil {
		messageBox(m.hwnd, "导入失败", err.Error())
		return
	}
	if report.Added == 0 && report.TasksAdded == 0 && len(report.TaskConflicts) == 0 {
		messageBox(m.hwnd, "无需导入", "内容已存在或已被你删除，本地修改已保留。")
		return
	}
	summary := fmt.Sprintf("将新增 %d 条（其中冲突副本 %d 条），跳过 %d 条，新增分类 %d 个。\n\n已有内容和个人修改会保留。", report.Added, len(report.Conflicts), report.Skipped, report.TasksAdded)
	if len(report.Conflicts) > 0 {
		summary += "\n\n以下条目的新版本将另存为副本：\n" + strings.Join(report.Conflicts, "\n")
	}
	if len(report.TaskConflicts) > 0 {
		summary += "\n\n以下分类存在同 ID 名称差异，保留本地名称：\n" + strings.Join(report.TaskConflicts, "\n")
	}
	if !confirmBox(m.hwnd, "确认导入", summary+"\n\n是否继续？") {
		return
	}
	added, skipped, conflicts, err := a.repo.Import(data)
	if err != nil {
		messageBox(m.hwnd, "导入失败", err.Error())
		return
	}
	managerRefresh(a)
	messageBox(m.hwnd, "导入完成", fmt.Sprintf("新增 %d 条，跳过 %d 条；新增条目中包含 %d 个冲突副本。", added, skipped, conflicts))
}

func (a *App) confirmManagerLeave(action string) bool {
	m := a.manager
	if m == nil {
		return true
	}
	// A blank draft is still a temporary editing state. It must be abandoned
	// before a search, selection, or close can refresh the manager list.
	if m.isNew && !m.dirty {
		a.discardManagerDraft()
		return true
	}
	if !m.dirty {
		return true
	}
	choice := 0
	if a.managerLeaveChoice != nil {
		choice = a.managerLeaveChoice(action)
	} else {
		choice = saveDiscardCancelBox(m.hwnd, "未保存的修改", "当前提示词有未保存的修改。\n\n选择“是”保存后继续；选择“否”放弃修改；选择“取消”留在当前界面。\n\n继续："+action)
	}
	switch choice {
	case IDYES:
		return a.managerSave(false)
	case IDNO:
		if m.isNew {
			a.discardManagerDraft()
			return true
		}
		m.dirty = false
		managerLoadForm(a)
		return true
	default:
		return false
	}
}

// discardManagerDraft ends the temporary new-item state before rebuilding the
// current result set. It intentionally does not touch repository data.
func (a *App) discardManagerDraft() {
	m := a.manager
	if m == nil || !m.isNew {
		return
	}
	m.loadedID = ""
	m.selected = -1
	m.dirty = false
	m.isNew = false
	m.taskChanged = false
	managerRefresh(a)
}

func (a *App) managerSave(showFeedback bool) bool {
	m := a.manager
	if m == nil || m.loadedID == "" {
		return false
	}
	var p *Prompt
	if m.isNew {
		p = &Prompt{
			ID:             m.loadedID,
			PrimaryTaskID:  unclassifiedTaskID,
			Origin:         "personal",
			Status:         "personal",
			ContentVersion: 1,
		}
	} else {
		p = a.repo.PromptByID(m.loadedID)
		if p == nil {
			return false
		}
	}
	p.Title = strings.TrimSpace(getWindowText(m.title))
	p.Description = strings.TrimSpace(getWindowText(m.description))
	// Win32 EDIT needs CRLF for display. Preserve the saved body byte-for-byte
	// if the user only changes metadata; normalize new edits back to LF.
	body := getWindowText(m.body)
	if body != windowsText(p.Body) {
		p.Body = strings.ReplaceAll(body, "\r\n", "\n")
	}
	p.Tags = parseTags(getWindowText(m.tags))
	idx := int(sendMessage(m.task, 0x0147, 0, 0))
	if idx >= 0 && idx < len(m.taskIDs) {
		selectedTask := m.taskIDs[idx]
		if m.isNew || m.taskChanged || canonicalTaskID(p.PrimaryTaskID) != selectedTask {
			p.PrimaryTaskID = selectedTask
		}
	} else {
		messageBox(m.hwnd, "保存失败", "请选择任务分类。")
		return false
	}
	if err := a.repo.UpsertPrompt(*p); err != nil {
		messageBox(m.hwnd, "保存失败", err.Error())
		return false
	}
	m.dirty = false
	m.isNew = false
	m.taskChanged = false
	if m.query != "" && promptScore(*p, m.query) == 0 {
		m.query = ""
		m.loadingForm = true
		setWindowText(m.search, "")
		m.loadingForm = false
	}
	managerRefresh(a)
	if showFeedback {
		messageBox(m.hwnd, "已保存", "提示词已保存到本地数据。")
	}
	return true
}

func centerWindow(hwnd HWND, w, h int32) {
	var rect Rect
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	work := monitorWorkArea((rect.Left+rect.Right)/2, (rect.Top+rect.Bottom)/2)
	x := work.Left + (work.Right-work.Left-w)/2
	y := work.Top + (work.Bottom-work.Top-h)/2
	setWindowPos(hwnd, 0, x, y, w, h, SWP_NOACTIVATE)
}

func rgb(r, g, b uint32) uint32 { return r | (g << 8) | (b << 16) }
func invalidate(hwnd HWND) {
	user32.NewProc("InvalidateRect").Call(uintptr(hwnd), 0, 1)
	user32.NewProc("UpdateWindow").Call(uintptr(hwnd))
}
