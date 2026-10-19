//go:build windows

package main

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// These tests create isolated controls in this process. They never inject
// keys, touch the system clipboard or load the user's library.
func TestWindowsContentControls(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("PROMPTDOCK_DATA_DIR", t.TempDir())
	r, err := NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	a := &App{repo: r}
	if err := a.initWindowsWithShell(false); err != nil {
		t.Fatal(err)
	}
	defer func() { a.shutdown(); unregisterTestClasses() }()
	a.ensureManager()
	m := a.manager
	if m == nil || m.hwnd == 0 {
		t.Fatal("manager was not created")
	}
	// t.Run creates a new goroutine. Keep all Win32 calls on the owning thread.
	run := func(name string, check func(*testing.T)) { t.Log(name); check(t) }

	run("list order and source", func(t *testing.T) {
		buf := make([]uint16, 512)
		sendMessage(m.list, 0x0189, 0, uintptr(unsafe.Pointer(&buf[0]))) // LB_GETTEXT
		if !strings.HasPrefix(syscall.UTF16ToString(buf), r.PromptByID(m.ids[0]).Title) {
			t.Fatal("list sorting broke ID selection")
		}
		source := getWindowText(m.source)
		if !strings.Contains(source, "liulifeng8918") || !strings.Contains(source, "MIT License") || !strings.Contains(source, "中文整理版") {
			t.Fatal("complete provenance is missing")
		}
		if height := int(sendMessage(m.list, LB_GETITEMHEIGHT, 0, 0)); height < 30 {
			t.Fatalf("manager list row height = %d, want at least 30", height)
		}
	})

	run("cancel software install keeps unsaved edits and target", func(t *testing.T) {
		setWindowText(m.title, "未保存的虚构修改")
		a.updater.offer = &UpdateOffer{Version: "0.3.0"}
		before := a.target
		a.managerLeaveChoice = func(string) int { return IDCANCEL }
		a.startSoftwareInstall()
		if !m.dirty || getWindowText(m.title) != "未保存的虚构修改" || a.target != before || a.updater.busy || a.shuttingDown {
			t.Fatal("cancel lost edits or started install")
		}
		a.managerLeaveChoice = nil
		m.dirty = false
		managerLoadForm(a)
		a.updater.offer = nil
	})
	run("cancel after download completion cannot install", func(t *testing.T) {
		u := a.updater
		u.cancelRequested = true
		u.busy, u.download = true, true
		u.events <- updateEvent{kind: "download", stage: "must-not-install", version: "0.3.0"}
		a.handleSoftwareEvents()
		if u.busy || u.download || a.shuttingDown {
			t.Fatal("cancelled completed download installed")
		}
		u.cancelRequested = false
	})
	run("turning off auto checks ignores an already queued background result", func(t *testing.T) {
		u := a.updater
		if err := r.SetAutoUpdateCheck(false); err != nil {
			t.Fatal(err)
		}
		u.offer = nil
		u.statusText = "unchanged"
		u.events <- updateEvent{kind: "check", offer: &UpdateOffer{Version: "0.3.0"}}
		a.handleSoftwareEvents()
		if u.offer != nil || u.statusText != "unchanged" {
			t.Fatal("disabled automatic check surfaced a pending result")
		}
		if err := r.SetAutoUpdateCheck(true); err != nil {
			t.Fatal(err)
		}
	})

	run("detail returns to originating list and Enter cannot paste", func(t *testing.T) {
		for _, mode := range []string{"all", "favorite", "recent"} {
			if err := r.SetFavorite("collection-01-03", true); err != nil {
				t.Fatal(err)
			}
			if err := r.RecordUse("collection-01-03"); err != nil {
				t.Fatal(err)
			}
			a.popup.view, a.popup.mode, a.popup.selected, a.popup.listScroll = viewSpecial, mode, 0, 0
			if mode == "all" {
				a.popup.selected = 6
				a.popup.listScroll = 2
			}
			before := popupLocation{a.popup.view, a.popup.mode, a.popup.taskID, a.popup.searchText, a.popup.selected, a.popup.listScroll}
			id := a.selectedPromptID()
			a.showSelectedDetail()
			if a.popup.view != viewDetail || !strings.HasPrefix(getWindowText(a.popup.detailText), windowsText(r.PromptByID(id).Body)) {
				t.Fatal("detail lost body")
			}
			// This would enter clipboard/paste logic in the old implementation.
			a.searchWndProc(a.popup.search, WM_KEYDOWN, VK_RETURN, 0)
			if a.popup.view != viewDetail {
				t.Fatal("Enter changed detail state")
			}
			a.backOrHide()
			after := popupLocation{a.popup.view, a.popup.mode, a.popup.taskID, a.popup.searchText, a.popup.selected, a.popup.listScroll}
			if before != after {
				t.Fatalf("detail return: got %+v want %+v", after, before)
			}
		}
		setWindowText(a.popup.search, "访谈")
		a.showSelectedDetail()
		a.backOrHide()
		if a.popup.searchText != "访谈" || a.selectedPromptID() != "collection-01-08" {
			t.Fatal("search detail return lost query")
		}

		// Category browsing reaches a real entry, while a search from that
		// category remains global and Esc restores the original category list.
		a.selectPopupEntry(popupEntryCategories, false)
		categoryIndex := -1
		for i, filter := range a.taskFilters() {
			if filter.ID == "ai-dialogue" {
				categoryIndex = i
				break
			}
		}
		if categoryIndex < 0 {
			t.Fatal("expected an AI-dialogue category with bundled content")
		}
		a.popup.selected = categoryIndex
		a.enterSelectedTask()
		categoryID := a.popup.taskID
		if a.popup.view != viewTask || categoryID == "" || len(a.popupResults()) == 0 {
			t.Fatal("category did not open a browsable prompt list")
		}
		a.showSelectedDetail()
		a.backOrHide()
		if a.popup.view != viewTask || a.popup.taskID != categoryID {
			t.Fatal("detail did not return to its category list")
		}
		setWindowText(a.popup.search, "访谈")
		if a.popup.view != viewTask || a.popup.taskID != categoryID || a.selectedPromptID() != "collection-01-08" {
			t.Fatal("category search did not use the full library")
		}
		a.backOrHide()
		if a.popup.searchText != "" || a.popup.view != viewTask || a.popup.taskID != categoryID {
			t.Fatal("clearing category search did not restore its prior location")
		}
		a.backOrHide()
		if !a.showsTaskNavigation() {
			t.Fatal("Esc from a category should return to the category root")
		}
	})

	run("edit tags and long body then restart", func(t *testing.T) {
		id := m.loadedID
		original := r.PromptByID(id)
		long := strings.Repeat("中文段落 📝\n", 10000) + "结束"
		setWindowText(m.title, "论文润色（个人）")
		setWindowText(m.description, "我的学术写作场景")
		setWindowText(m.tags, "论文，学术润色;个人用途")
		setMultilineText(m.body, long)
		if !m.dirty {
			t.Fatal("edit did not mark form dirty")
		}
		a.ensureManager()
		managerRefresh(a) // A refresh must never silently discard a dirty form.
		if getWindowText(m.body) != windowsText(long) || !m.dirty {
			t.Fatal("reopening discarded draft")
		}
		for i, id := range m.taskIDs {
			if id == "writing-expression" {
				sendMessage(m.task, 0x014E, uintptr(i), 0)
			}
		}
		if !a.managerSave(false) {
			t.Fatal("save failed")
		}
		saved := r.PromptByID(id)
		if saved.Body != long || len(saved.Tags) != 3 || saved.PrimaryTaskID != "writing-expression" || !saved.LocalModified || saved.Source != original.Source {
			t.Fatal("editing lost body/tags/category/provenance")
		}
		restarted, err := NewRepository()
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(saved, restarted.PromptByID(id)) {
			t.Fatal("restart lost personal edits")
		}
	})

	run("new categories refresh the dropdown", func(t *testing.T) {
		data := encodeLibrary(t, Library{SchemaVersion: 1, Tasks: []Task{{ID: "import-scene", Title: "导入新场景"}}, Prompts: []Prompt{{ID: "scene", Title: "场景条目", Body: "正文", PrimaryTaskID: "import-scene"}}})
		if _, _, _, err := r.Import(data); err != nil {
			t.Fatal(err)
		}
		m.loadedID = "scene"
		managerRefresh(a)
		if int(sendMessage(m.task, 0x0146, 0, 0)) != len(r.TaskFilters()) {
			t.Fatal("new category absent from dropdown")
		}
		idx := int(sendMessage(m.task, 0x0147, 0, 0))
		if idx < 0 || m.taskIDs[idx] != "import-scene" {
			t.Fatal("category selection does not match stable task ID")
		}
	})

	run("popup entry defaults to categories and persists reusable entries", func(t *testing.T) {
		a.selectPopupEntry(popupEntryCategories, false)
		if !a.showsTaskNavigation() || a.activePopupEntry() != popupEntryCategories || len(a.taskFilters()) == 0 {
			t.Fatal("popup should default to a browsable category root")
		}
		if r.PopupEntry() != popupEntryCategories {
			t.Fatal("unset entry preference should resolve to categories")
		}

		// The permanently visible entry strip selects and saves both reuse paths.
		a.handlePopupClick(false, 30, popupEntryTop+4)
		if a.popup.view != viewSpecial || a.popup.mode != popupEntryFavorite || r.PopupEntry() != popupEntryFavorite {
			t.Fatal("favorite entry did not activate or persist")
		}
		restarted, err := NewRepository()
		if err != nil || restarted.PopupEntry() != popupEntryFavorite {
			t.Fatalf("favorite entry was not retained across restart: %v", err)
		}
		a.handlePopupClick(false, 150, popupEntryTop+4)
		if a.popup.view != viewSpecial || a.popup.mode != popupEntryRecent || r.PopupEntry() != popupEntryRecent {
			t.Fatal("recent entry did not activate or persist")
		}
		restarted, err = NewRepository()
		if err != nil || restarted.PopupEntry() != popupEntryRecent {
			t.Fatalf("recent entry was not retained across restart: %v", err)
		}

		// Empty reuse roots keep navigation available and the in-window category
		// link returns to discovery instead of leaving a blank list.
		for i := range r.Library.Prompts {
			r.Library.Prompts[i].Favorite = false
			r.Library.Prompts[i].LastUsedAt = time.Time{}
		}
		a.selectPopupEntry(popupEntryFavorite, false)
		if !a.showsEmptyReuseState() {
			t.Fatal("empty favorite root should expose the category recovery action")
		}
		a.handlePopupClick(false, 150, popupContentTop+58)
		if !a.showsTaskNavigation() || a.activePopupEntry() != popupEntryCategories {
			t.Fatal("empty favorite recovery action did not open categories")
		}

		searchRect, popupRect := getWindowRect(a.popup.search), getWindowRect(a.popup.hwnd)
		if searchRect.Top <= popupRect.Top+popupHeaderHeight || searchRect.Bottom >= popupRect.Bottom-popupEntryHeight || popupContentTop <= popupEntryTop+popupEntryHeight {
			t.Fatal("search and entry controls are not fixed above the scrollable list")
		}
	})

	run("new prompt needs only title and body", func(t *testing.T) {
		a.managerNewDraft()
		if !m.isNew || m.loadedID == "" || getWindowText(m.description) != "" || getWindowText(m.tags) != "" {
			t.Fatal("new draft should start with optional description and tags")
		}
		idx := int(sendMessage(m.task, 0x0147, 0, 0))
		if idx < 0 || m.taskIDs[idx] != unclassifiedTaskID {
			t.Fatal("new draft did not default to unclassified")
		}
		setWindowText(m.title, "我的最小提示词")
		setMultilineText(m.body, "只需要名称和正文。")
		if !a.managerSave(false) {
			t.Fatal("new prompt with title and body should save")
		}
		p := r.PromptByID(m.loadedID)
		if p == nil || p.PrimaryTaskID != unclassifiedTaskID || p.Description != "" || len(p.Tags) != 0 {
			t.Fatalf("new prompt saved unexpected required fields: %#v", p)
		}
		a.handleManagerCommand(ID_MANAGER_SOURCE_TOGGLE, BN_CLICKED)
		if !m.sourceExpanded {
			t.Fatal("source information did not expand")
		}
		a.handleManagerCommand(ID_MANAGER_SOURCE_TOGGLE, BN_CLICKED)
		if m.sourceExpanded {
			t.Fatal("source information did not collapse")
		}
	})

	run("clipboard failure keeps manager insertion context for retry", func(t *testing.T) {
		wantTarget := TargetSession{HWND: 101, PID: 202, ThreadID: 303, FocusHWND: 404}
		m.target = wantTarget
		a.target = TargetSession{}
		attempts := 0
		a.clipboardWriter = func(HWND, string) error {
			attempts++
			return errors.New("clipboard busy")
		}
		defer func() { a.clipboardWriter = nil }()
		for retry := 0; retry < 2; retry++ {
			if err := a.managerInsert(); err == nil {
				t.Fatal("clipboard failure reported insertion success")
			}
			if m.target != wantTarget || a.target.Valid() || m.loadedID == "" {
				t.Fatal("clipboard failure discarded retry context")
			}
		}
		if attempts != 2 {
			t.Fatalf("retry did not attempt clipboard again: %d", attempts)
		}
	})

	run("timeout keeps popup visible and focused", func(t *testing.T) {
		a.target = TargetSession{HWND: 101, PID: 202}
		a.popup.visible = true
		setWindowPos(a.popup.hwnd, 0, -4000, -4000, 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
		showWindow(a.popup.hwnd, SW_SHOW)
		setFocus(a.popup.search)
		focusBefore := focusedWindow()
		if !isWindowVisible(a.popup.hwnd) || focusBefore != a.popup.search {
			t.Fatal("popup test window was not visible and focused")
		}
		a.clipboardWriter = func(HWND, string) error { return nil }
		a.releaseWaiter = func() bool { return false }
		pasteCalls := 0
		a.pasteSender = func() bool { pasteCalls++; return true }
		message := ""
		a.insertionMessage = func(_ HWND, _, text string) { message = text }
		defer func() {
			a.clipboardWriter = nil
			a.releaseWaiter = nil
			a.pasteSender = nil
			a.insertionMessage = nil
			a.hidePopup()
		}()
		a.insertPrompt(m.loadedID)
		if pasteCalls != 0 {
			t.Fatal("timed-out key release still sent paste")
		}
		if !isWindowVisible(a.popup.hwnd) || !a.popup.visible || focusedWindow() != focusBefore || !a.target.Valid() {
			t.Fatal("timeout hid popup or transferred focus")
		}
		if !strings.Contains(message, "仍未释放") || !strings.Contains(message, "正文已复制") {
			t.Fatalf("timeout did not provide manual-paste fallback: %q", message)
		}
	})

	run("timeout keeps manager visible and insertion target", func(t *testing.T) {
		wantTarget := TargetSession{HWND: 101, PID: 202, ThreadID: 303, FocusHWND: 404}
		m.target = wantTarget
		setWindowPos(m.hwnd, 0, -4000, -4000, 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
		showWindow(m.hwnd, SW_SHOW)
		setFocus(m.title)
		focusBefore := focusedWindow()
		if !isWindowVisible(m.hwnd) || focusBefore != m.title {
			t.Fatal("manager test window was not visible and focused")
		}
		a.clipboardWriter = func(HWND, string) error { return nil }
		a.releaseWaiter = func() bool { return false }
		pasteCalls := 0
		a.pasteSender = func() bool { pasteCalls++; return true }
		message := ""
		a.insertionMessage = func(_ HWND, _, text string) { message = text }
		defer func() {
			a.clipboardWriter = nil
			a.releaseWaiter = nil
			a.pasteSender = nil
			a.insertionMessage = nil
			showWindow(m.hwnd, SW_HIDE)
		}()
		if err := a.managerInsert(); err != nil {
			t.Fatal(err)
		}
		if pasteCalls != 0 || !isWindowVisible(m.hwnd) || focusedWindow() != focusBefore || m.target != wantTarget {
			t.Fatal("timeout hid manager or transferred insertion context")
		}
		if !strings.Contains(message, "仍未释放") || !strings.Contains(message, "正文已复制") {
			t.Fatalf("timeout did not provide manager fallback: %q", message)
		}
	})

	run("empty search clears the old editor", func(t *testing.T) {
		setWindowText(m.search, "不可能匹配的场景词")
		if len(m.ids) != 0 || m.loadedID != "" || getWindowText(m.body) != "" {
			t.Fatal("empty result left stale editor values")
		}
		if a.managerSave(false) {
			t.Fatal("empty result should not save an old record")
		}
	})

	run("new draft after empty search re-enables native controls", func(t *testing.T) {
		a.managerNewDraft()
		for _, control := range []HWND{m.title, m.description, m.body, m.task, m.tags,
			m.buttons[ID_MANAGER_SAVE], m.buttons[ID_MANAGER_COPY], m.buttons[ID_MANAGER_INSERT]} {
			if !isWindowEnabled(control) {
				t.Fatal("new draft left an editable control disabled")
			}
		}
		for _, control := range []HWND{m.buttons[ID_MANAGER_DELETE], m.buttons[ID_MANAGER_FAV]} {
			if isWindowEnabled(control) {
				t.Fatal("new draft enabled an action requiring a saved prompt")
			}
		}
		id := m.loadedID
		setWindowText(m.title, "空搜索后的新建")
		setMultilineText(m.body, "可以保存的正文。")
		if !a.managerSave(false) {
			t.Fatal("new draft after empty search did not save")
		}
		if p := r.PromptByID(id); p == nil || p.Body != "可以保存的正文。" {
			t.Fatalf("new draft after empty search was not persisted: %#v", p)
		}
	})

	run("leaving a new draft refreshes or preserves it as selected", func(t *testing.T) {
		defer func() { a.managerLeaveChoice = nil }()

		// A blank draft has no confirmation, but leaving it must still end the
		// transient state so a search can rebuild the list.
		a.managerNewDraft()
		blankID := m.loadedID
		setWindowText(m.search, "访谈")
		if m.isNew || m.loadedID == blankID || m.query != "访谈" || len(m.ids) == 0 {
			t.Fatal("blank new draft blocked search refresh")
		}

		// Discard is chosen for a dirty draft and must remove its temporary ID
		// before continuing to the requested search.
		a.managerNewDraft()
		discardedID := m.loadedID
		setWindowText(m.title, "应放弃的新建草稿")
		a.managerLeaveChoice = func(string) int { return IDNO }
		setWindowText(m.search, "论文")
		if m.isNew || m.loadedID == discardedID || m.query != "论文" || len(m.ids) == 0 || r.PromptByID(discardedID) != nil {
			t.Fatal("discarding a new draft did not restore a refreshed list")
		}

		// Cancel keeps the unfinished draft intact and returns the search box to
		// the pre-draft query instead of silently switching records.
		a.managerNewDraft()
		keptID := m.loadedID
		setWindowText(m.title, "取消时保留的新建草稿")
		a.managerLeaveChoice = func(string) int { return IDCANCEL }
		setWindowText(m.search, "不会应用的搜索词")
		if !m.isNew || !m.dirty || m.loadedID != keptID || m.query != "" || getWindowText(m.title) != "取消时保留的新建草稿" {
			t.Fatal("cancel should keep the new draft and its prior search")
		}

		// Closing with discard follows the same cleanup, and opening the manager
		// again shows a normal refreshable list.
		a.managerLeaveChoice = func(string) int { return IDNO }
		showWindow(m.hwnd, SW_SHOW)
		a.managerWndProc(m.hwnd, WM_CLOSE, 0, 0)
		if isWindowVisible(m.hwnd) || m.isNew || m.dirty {
			t.Fatal("closing with discard left the manager in draft state")
		}
		showWindow(m.hwnd, SW_SHOW)
		managerRefresh(a)
		if len(m.ids) == 0 || m.loadedID == "" {
			t.Fatal("reopening after discarding a draft did not restore the list")
		}
		showWindow(m.hwnd, SW_HIDE)
	})

	run("new draft after the last visible item is removed re-enables controls", func(t *testing.T) {
		for i := range r.Library.Prompts {
			r.Library.Prompts[i].Deleted = true
		}
		m.query = ""
		m.loadingForm = true
		setWindowText(m.search, "")
		m.loadingForm = false
		managerRefresh(a)
		if len(m.ids) != 0 || isWindowEnabled(m.title) || isWindowEnabled(m.buttons[ID_MANAGER_SAVE]) {
			t.Fatal("empty library did not disable the existing-item editor")
		}
		a.managerNewDraft()
		if !isWindowEnabled(m.title) || !isWindowEnabled(m.body) || !isWindowEnabled(m.task) || !isWindowEnabled(m.tags) || !isWindowEnabled(m.buttons[ID_MANAGER_SAVE]) {
			t.Fatal("new draft after empty library left required controls disabled")
		}
		id := m.loadedID
		setWindowText(m.title, "空库后的新建")
		setMultilineText(m.body, "仍然可以保存。")
		if !a.managerSave(false) || r.PromptByID(id) == nil {
			t.Fatal("new draft after empty library was not saved")
		}
	})
}

func TestPasteSequenceNeverAddsSendKey(t *testing.T) {
	inputs := pasteInputs()
	want := []KeybdInput{{Vk: VK_CONTROL}, {Vk: VK_V}, {Vk: VK_V, Flags: KEYEVENTF_KEYUP}, {Vk: VK_CONTROL, Flags: KEYEVENTF_KEYUP}}
	if len(inputs) != len(want) {
		t.Fatal("paste should contain only one Ctrl+V chord")
	}
	for i, input := range inputs {
		if input.Type != INPUT_KEYBOARD || *(*KeybdInput)(unsafe.Pointer(&input.Union[0])) != want[i] {
			t.Fatal("unexpected key in paste sequence")
		}
	}
}

func TestInsertKeyReleaseWait(t *testing.T) {
	seen := make(map[uint32]bool)
	for _, key := range insertReleaseKeys {
		seen[key] = true
	}
	for _, key := range []uint32{VK_RETURN, VK_CONTROL, VK_MENU, VK_SHIFT, VK_LWIN, VK_RWIN, 'P'} {
		if !seen[key] {
			t.Fatalf("insert release guard omitted key %#x", key)
		}
	}

	pauses := 0
	if !waitForKeysReleased(insertReleaseKeys, 5, func(key uint32) bool {
		return key == VK_RETURN && pauses < 3
	}, func() { pauses++ }) {
		t.Fatal("released selection key should allow insertion")
	}
	if pauses != 3 {
		t.Fatalf("release was not polled until keyup: %d pauses", pauses)
	}

	pauses = 0
	if waitForKeysReleased([]uint32{VK_RETURN}, 3, func(uint32) bool { return true }, func() { pauses++ }) {
		t.Fatal("held selection key should time out and block insertion")
	}
	if pauses != 3 {
		t.Fatalf("timeout wait count = %d, want 3", pauses)
	}
}

func TestCompactRowTextDoesNotOverlapActions(t *testing.T) {
	rect := Rect{Right: 328, Bottom: 57}
	for _, favorite := range []bool{false, true} {
		title, description := popupRowTextRects(rect, true, false, favorite)
		if description.Right >= rect.Right-43 || description.Left >= description.Right {
			t.Fatal("scenario text overlaps insertion label")
		}
		if favorite && title.Right >= rect.Right-56 {
			t.Fatal("title overlaps favorite star")
		}
	}
}

func TestWindowsClipboardLineEndingsKeepParagraphs(t *testing.T) {
	for _, input := range []string{"第一行\n\n第二段", "第一行\r\n\r\n第二段", "第一行\r\n\n第二段"} {
		if got := windowsText(input); got != "第一行\r\n\r\n第二段" {
			t.Fatalf("paragraphs changed: %q", got)
		}
	}
}
