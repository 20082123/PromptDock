//go:build windows

package main

import "unsafe"

var applicationFont uintptr

func uiFont() uintptr {
	if applicationFont == 0 {
		v, _, _ := gdi32.NewProc("CreateFontW").Call(^uintptr(13), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(utf16Ptr("Microsoft YaHei UI"))))
		applicationFont = v
	}
	if applicationFont == 0 {
		return stockObject(DEFAULT_GUI_FONT)
	}
	return applicationFont
}
func appIcon() uintptr {
	v, _, _ := procLoadIconW.Call(getModuleHandle(), 101)
	if v == 0 {
		v, _, _ = procLoadIconW.Call(0, IDI_APPLICATION)
	}
	return v
}

type drawItem struct {
	Type, ControlID, ItemID, Action, State uint32
	Window                                 HWND
	DC                                     HDC
	Rect                                   Rect
	Data                                   uintptr
}

func popupRowTextRects(rect Rect, selected, task, favorite bool) (Rect, Rect) {
	titleRight, descriptionRight := rect.Right-38, rect.Right-15
	if favorite && !task {
		titleRight = rect.Right - 64 // Leave space for the star, not just the arrow.
	}
	if selected && !task {
		descriptionRight = rect.Right - 52 // Keep the insertion label separate.
	}
	return Rect{Left: rect.Left + 14, Top: rect.Top + 7, Right: titleRight, Bottom: rect.Top + 28},
		Rect{Left: rect.Left + 14, Top: rect.Top + 28, Right: descriptionRight, Bottom: rect.Bottom - 6}
}

func (a *App) drawManagerItem(param uintptr) bool {
	var item drawItem
	kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&item)), param, unsafe.Sizeof(item))
	m := a.manager
	if m == nil || item.ControlID != ID_MANAGER_LIST {
		return false
	}
	background, foreground, secondary := rgb(255, 255, 255), rgb(35, 40, 48), rgb(100, 108, 120)
	if item.State&1 != 0 {
		background, foreground, secondary = rgb(228, 238, 255), rgb(24, 65, 130), rgb(50, 85, 140)
	}
	brush := createBrush(background)
	fillRect(item.DC, &item.Rect, brush)
	deleteObject(uintptr(brush))
	if int(item.ItemID) >= len(m.ids) {
		return true
	}
	p := a.repo.PromptByID(m.ids[item.ItemID])
	if p == nil {
		return true
	}
	selectObject(item.DC, uiFont())
	setBkMode(item.DC, TRANSPARENT)
	setTextColor(item.DC, foreground)
	r := item.Rect
	r.Left += 8
	r.Right -= 8
	r.Top += 3
	r.Bottom = r.Top + 21
	drawText(item.DC, p.Title, &r, DT_SINGLELINE|DT_END_ELLIPSIS|DT_VCENTER|DT_NOPREFIX)
	setTextColor(item.DC, secondary)
	r.Top += 22
	r.Bottom = r.Top + 18
	drawText(item.DC, a.repo.TaskTitle(p.PrimaryTaskID), &r, DT_SINGLELINE|DT_END_ELLIPSIS|DT_VCENTER|DT_NOPREFIX)
	return true
}
