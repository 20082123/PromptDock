//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"
)

const (
	WM_SOFTWARE_UPDATE = 0x8050
	ID_SOFTWARE_UPDATE = 1121
	ID_UPDATE_CHECK    = 1201
	ID_UPDATE_INSTALL  = 1202
	ID_UPDATE_LATER    = 1203
	ID_UPDATE_AUTO     = 1204
	ID_UPDATE_WEBSITE  = 1205
)

type updateEvent struct {
	kind    string
	offer   *UpdateOffer
	stage   string
	err     error
	manual  bool
	version string
}
type softwareUpdater struct {
	hwnd, status, notes, auto, check, install, later, website HWND
	events                                                    chan updateEvent
	client                                                    *updateClient
	offer                                                     *UpdateOffer
	busy                                                      bool
	download                                                  bool
	cancelRequested                                           bool
	cancel                                                    context.CancelFunc
	statusText                                                string
}

func (a *App) initSoftwareUpdater() error {
	a.updater = &softwareUpdater{events: make(chan updateEvent, 8), client: newUpdateClient(updateRepository)}
	return registerClass("PromptDockUpdate", makeCallback(a.updateWndProc))
}
func (a *App) scheduleSoftwareCheck() {
	if a.repo.Library.Settings.DisableAutoUpdateCheck || updateRepository == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.backgroundCancel = cancel
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(6 * time.Second):
		}
		checkCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		offer, err := a.updater.client.check(checkCtx, appVersion)
		if ctx.Err() != nil {
			return
		}
		a.queueUpdateEvent(updateEvent{kind: "check", offer: offer, err: err})
	}()
}
func (a *App) queueUpdateEvent(event updateEvent) {
	select {
	case a.updater.events <- event:
		postMessage(a.popup.hwnd, WM_SOFTWARE_UPDATE, 0, 0)
	default:
	}
}
func (a *App) openSoftwareUpdate() {
	u := a.updater
	if u == nil {
		return
	}
	if u.hwnd == 0 {
		u.hwnd = createWindow(0, "PromptDockUpdate", "PromptDock · 软件更新", WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_CLIPCHILDREN, 0, 0, 660, 420, 0, 0, 0)
	}
	centerWindow(u.hwnd, 660, 420)
	showWindow(u.hwnd, SW_SHOW)
	setForeground(u.hwnd)
	if !u.busy {
		a.checkSoftwareUpdate(true)
	}
}
func (a *App) checkSoftwareUpdate(manual bool) {
	u := a.updater
	if u == nil || u.busy {
		return
	}
	u.busy = true
	u.statusText = "正在检查软件更新…"
	a.syncUpdateControls()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	u.cancel = cancel
	go func() {
		defer cancel()
		offer, err := u.client.check(ctx, appVersion)
		a.queueUpdateEvent(updateEvent{kind: "check", offer: offer, err: err, manual: manual})
	}()
}
func (a *App) syncUpdateControls() {
	u := a.updater
	if u == nil || u.hwnd == 0 {
		return
	}
	setWindowText(u.status, u.statusText)
	if u.offer != nil {
		setMultilineText(u.notes, fmt.Sprintf("当前版本：%s    新版本：%s\n\n%s\n\n仅点击“更新并重启”后才下载。更新只替换主程序，个人数据先备份；内置提示词需另行预览更新。", appVersion, u.offer.Version, u.offer.Summary))
	} else {
		setMultilineText(u.notes, "当前版本："+appVersion+"\n\n软件更新会检查明确配置的官方 GitHub 稳定 Release。\n\n当前不需要联网使用提示词；关闭自动检查后仍可手动检查。\n\nSHA-256 用于验证下载完整性，不等同于代码签名。")
	}
	enableWindow(u.check, !u.busy)
	enableWindow(u.install, !u.busy && u.offer != nil)
	enableWindow(u.website, u.offer != nil)
	if u.download {
		setWindowText(u.later, "取消下载")
	} else {
		setWindowText(u.later, "稍后")
	}
	checked := uintptr(1)
	if a.repo.Library.Settings.DisableAutoUpdateCheck {
		checked = 0
	}
	sendMessage(u.auto, 0x00F1, checked, 0)
}
func (a *App) startSoftwareInstall() {
	u := a.updater
	if u == nil || u.busy || u.offer == nil {
		return
	}
	if !a.confirmManagerLeave("安装软件更新并重启") {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		messageBox(u.hwnd, "无法准备更新", err.Error())
		return
	}
	stage, err := prepareUpdateStage(exe)
	if err != nil {
		messageBox(u.hwnd, "无法准备更新", err.Error()+"\n可用“打开下载页”下载后解压到可写目录。")
		return
	}
	u.busy, u.download = true, true
	u.cancelRequested = false
	u.statusText = "正在下载并校验…当前版本继续可用。"
	a.syncUpdateControls()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	u.cancel = cancel
	offer := *u.offer
	go func() {
		defer cancel()
		err := u.client.download(ctx, &offer, stage)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		a.queueUpdateEvent(updateEvent{kind: "download", stage: stage, version: offer.Version, err: err})
	}()
}
func (a *App) handleSoftwareEvents() {
	u := a.updater
	for {
		select {
		case event := <-u.events:
			if event.kind == "check" {
				if !event.manual && a.repo.Library.Settings.DisableAutoUpdateCheck {
					continue
				}
				if !event.manual && u.busy {
					continue
				}
				if event.manual {
					u.busy = false
				}
				if event.err != nil {
					if event.manual {
						if errors.Is(event.err, errUpdateSourceDisabled) {
							u.statusText = "更新源尚未启用。官方仓库发布后才会启用检查。"
						} else {
							u.statusText = "检查失败，请点击“检查/重试”再次尝试。"
							messageBox(u.hwnd, "检查软件更新失败", event.err.Error())
						}
					}
				} else {
					u.offer = event.offer
					if event.offer == nil {
						u.statusText = "当前已是最新稳定版本（" + appVersion + "）。"
					} else {
						u.statusText = "发现新版本 " + event.offer.Version + "。您可以更新并重启，或稍后处理。"
						if !event.manual {
							trayNotify(a.popup.hwnd, "PromptDock 有新版本", "版本 "+event.offer.Version+" 已可用，请从托盘打开“检查软件更新”。")
						}
					}
				}
			} else if event.kind == "download" {
				u.busy, u.download = false, false
				if u.cancelRequested {
					event.err = context.Canceled
				}
				if event.err != nil {
					u.statusText = "更新已停止，当前版本未改动。可以重试或打开下载页。"
					if !errors.Is(event.err, context.Canceled) {
						messageBox(u.hwnd, "下载或校验失败", event.err.Error())
					}
				} else {
					// Users may keep editing while a download runs. Confirm AGAIN
					// immediately before backing up and handing off the executable.
					if !a.confirmManagerLeave("安装软件更新并重启") {
						u.statusText = "安装已取消，当前版本与未保存编辑保留。"
						a.syncUpdateControls()
						continue
					}
					exe, err := os.Executable()
					if err == nil {
						var job updateJob
						job, err = prepareUpdateJob(exe, a.repo.LibraryPath, event.stage, event.version)
						if err == nil {
							err = startUpdateHelper(job)
						}
					}
					if err != nil {
						u.statusText = "安装未开始；旧版本与个人库保留。"
						messageBox(u.hwnd, "无法安装更新", err.Error()+"\n可重新尝试或打开下载页手动解压升级。")
					} else {
						a.shutdown()
						return
					}
				}
			}
			a.syncUpdateControls()
		default:
			return
		}
	}
}

func (a *App) updateWndProc(hwnd HWND, msg uint32, w, l uintptr) uintptr {
	u := a.updater
	switch msg {
	case WM_CREATE:
		u.hwnd = hwnd
		u.status = createStatic(hwnd, "软件更新", 18, 16, 610, 44)
		u.notes = createWindow(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_VSCROLL|0x0004|0x0040|0x0800, 18, 68, 610, 212, hwnd, 0, 0)
		sendMessage(u.notes, WM_SETFONT, uiFont(), 1)
		u.auto = createWindow(0, "BUTTON", "启动后自动检查软件更新（不自动下载）", WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0003, 18, 290, 610, 26, hwnd, ID_UPDATE_AUTO, 0)
		sendMessage(u.auto, WM_SETFONT, uiFont(), 1)
		u.install = createButton(hwnd, "更新并重启", ID_UPDATE_INSTALL, 18, 330, 132, 30)
		u.check = createButton(hwnd, "检查/重试", ID_UPDATE_CHECK, 162, 330, 110, 30)
		u.website = createButton(hwnd, "打开下载页", ID_UPDATE_WEBSITE, 284, 330, 128, 30)
		u.later = createButton(hwnd, "稍后", ID_UPDATE_LATER, 500, 330, 128, 30)
		a.syncUpdateControls()
		return 0
	case WM_COMMAND:
		switch int(loword(w)) {
		case ID_UPDATE_CHECK:
			a.checkSoftwareUpdate(true)
		case ID_UPDATE_INSTALL:
			a.startSoftwareInstall()
		case ID_UPDATE_LATER:
			if u.download && u.cancel != nil {
				u.cancelRequested = true
				u.cancel()
			}
			showWindow(hwnd, SW_HIDE)
		case ID_UPDATE_AUTO:
			enabled := sendMessage(u.auto, 0x00F0, 0, 0) != 0
			if err := a.repo.SetAutoUpdateCheck(enabled); err != nil {
				messageBox(hwnd, "设置未保存", err.Error())
			}
			if !enabled && a.backgroundCancel != nil {
				a.backgroundCancel()
			}
			a.syncUpdateControls()
		case ID_UPDATE_WEBSITE:
			if u.offer != nil {
				openExternalURL(hwnd, u.offer.ReleaseURL)
			}
		}
		return 0
	case WM_CLOSE:
		if u.download && u.cancel != nil {
			u.cancelRequested = true
			u.cancel()
		}
		showWindow(hwnd, SW_HIDE)
		return 0
	case WM_DESTROY:
		u.hwnd = 0
		return 0
	}
	v, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return v
}

func completeHealthStartup(job updateJob) error {
	// This release keeps schema 1 unchanged. No implicit schema migration is
	// allowed during update health checks; both executable and data can revert.
	actual, err := fileHash(filepath.Join(job.DataDir, "library.json"))
	if err != nil || actual != job.DataHash {
		return fmt.Errorf("更新启动改变了个人数据，拒绝确认升级")
	}
	if err := writeSynced(filepath.Join(job.Stage, "healthy"), []byte(job.Nonce), 0600); err != nil {
		return err
	}
	// Do not start processing edits/hotkeys until the helper has committed the
	// update; this removes a race between first use and rollback verification.
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		data, err := os.ReadFile(filepath.Join(job.Stage, "committed"))
		if err == nil && string(data) == job.Nonce {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("更新助手未确认完成；停止本次启动以便回退")
}

func postMessage(hwnd HWND, msg uint32, w, l uintptr) {
	user32.NewProc("PostMessageW").Call(uintptr(hwnd), uintptr(msg), w, l)
}
func trayNotify(hwnd HWND, title, text string) {
	n := NotifyIconData{CbSize: uint32(unsafe.Sizeof(NotifyIconData{})), Wnd: hwnd, ID: 1, Flags: 0x10, InfoFlags: 1}
	copy(n.Info[:], mustUTF16(text))
	copy(n.InfoTitle[:], mustUTF16(title))
	procShellNotifyIconW.Call(1, uintptr(unsafe.Pointer(&n)))
}
