//go:build windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Exclusive sharing mode is enforced by Windows across processes/sessions.
// Normal startup acquires this BEFORE reading or creating the personal library.
type fileLock struct{ handle syscall.Handle }

func acquireFileLock(name string) (*fileLock, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("文件正被另一实例使用，或目录无写入权限：%w", err)
	}
	return &fileLock{handle: h}, nil
}
func (l *fileLock) Close() {
	if l != nil && l.handle != 0 {
		_ = syscall.CloseHandle(l.handle)
		l.handle = 0
	}
}

func hiddenCommand(exe string, args ...string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

type updateJob struct {
	InstallDir string `json:"installDir"`
	DataDir    string `json:"dataDir"`
	Stage      string `json:"stage"`
	ParentPID  int    `json:"parentPID"`
	OldVersion string `json:"oldVersion"`
	Version    string `json:"version"`
	OldHash    string `json:"oldHash"`
	NewHash    string `json:"newHash"`
	Backup     string `json:"backup"`
	DataHash   string `json:"dataHash"`
	Nonce      string `json:"nonce"`
}
type updateJournal struct {
	State   string    `json:"state"`
	Message string    `json:"message"`
	Job     updateJob `json:"job"`
}

func canonicalDirectory(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Clean(abs), filepath.Clean(real)) {
		return "", errors.New("更新目录含符号链接或目录联接，请手动下载解压升级")
	}
	return abs, nil
}
func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

func prepareUpdateStage(exe string) (string, error) {
	dir, err := canonicalDirectory(filepath.Dir(exe))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Base(exe), "PromptDock.exe") {
		return "", errors.New("请运行发布包中的 PromptDock.exe 后重试，或手动下载升级")
	}
	return os.MkdirTemp(dir, ".promptdock-update-")
}

func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".promptdock-write-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceFile(tmp, name)
}
func writeJournal(job updateJob, state, message string) error {
	data, err := json.MarshalIndent(updateJournal{State: state, Message: message, Job: job}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(job.InstallDir, ".promptdock-update.json"), data)
}
func copySynced(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeSynced(target, data, 0700)
}

func (job updateJob) validate(jobPath string) error {
	for _, dir := range []string{job.InstallDir, job.DataDir, job.Stage} {
		if _, err := canonicalDirectory(dir); err != nil {
			return err
		}
	}
	if !samePath(filepath.Dir(job.Stage), job.InstallDir) || !strings.HasPrefix(filepath.Base(job.Stage), ".promptdock-update-") || !samePath(jobPath, filepath.Join(job.Stage, "job.json")) {
		return errors.New("更新作业路径无效")
	}
	rel, err := filepath.Rel(filepath.Join(job.DataDir, "backups"), job.Backup)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.Base(job.Backup) != "library.json" {
		return errors.New("备份路径无效")
	}
	if len(job.Nonce) != 32 || job.ParentPID <= 0 {
		return errors.New("更新作业参数无效")
	}
	for _, value := range []string{job.OldHash, job.NewHash, job.DataHash} {
		data, err := hex.DecodeString(value)
		if err != nil || len(data) != 32 {
			return errors.New("更新作业摘要无效")
		}
	}
	cmp, err := compareVersions(job.Version, job.OldVersion)
	if err != nil || cmp <= 0 {
		return errors.New("更新版本必须高于当前版本")
	}
	actual, err := fileHash(job.Backup)
	if err != nil || actual != job.DataHash {
		return errors.New("升级前个人数据备份不完整")
	}
	return nil
}
func loadUpdateJob(name string) (updateJob, error) {
	var job updateJob
	data, err := os.ReadFile(name)
	if err != nil {
		return job, err
	}
	if len(data) > 16<<10 {
		return job, errors.New("更新作业过大")
	}
	if err := json.Unmarshal(data, &job); err != nil {
		return job, err
	}
	return job, job.validate(name)
}

func prepareUpdateJob(exe, library, stage, version string) (updateJob, error) {
	job := updateJob{InstallDir: filepath.Dir(exe), DataDir: filepath.Dir(library), Stage: stage, ParentPID: os.Getpid(), OldVersion: appVersion, Version: version}
	backup, hash, err := backupPersonalData(library)
	if err != nil {
		return job, fmt.Errorf("备份失败，已停止安装：%w", err)
	}
	job.Backup, job.DataHash = backup, hash
	job.OldHash, err = fileHash(exe)
	if err != nil {
		return job, err
	}
	job.NewHash, err = fileHash(filepath.Join(stage, "PromptDock.next.exe"))
	if err != nil {
		return job, err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return job, err
	}
	job.Nonce = hex.EncodeToString(buf)
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return job, err
	}
	if err := writeSynced(filepath.Join(stage, "job.json"), data, 0600); err != nil {
		return job, err
	}
	if err := copySynced(exe, filepath.Join(stage, "updater.exe")); err != nil {
		return job, err
	}
	return job, writeJournal(job, "prepared", "备份完成，等待应用退出")
}

func startUpdateHelper(job updateJob) error {
	cmd := hiddenCommand(filepath.Join(job.Stage, "updater.exe"), "--apply-update", filepath.Join(job.Stage, "job.json"))
	if err := cmd.Start(); err != nil {
		return err
	}
	// The app remains open unless a validated helper signals that it is ready.
	defer cmd.Process.Release()
	for start := time.Now(); time.Since(start) < 4*time.Second; {
		data, err := os.ReadFile(filepath.Join(job.Stage, "ready"))
		if err == nil && string(data) == job.Nonce {
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return errors.New("更新助手未就绪；当前程序与个人库未被替换，请重试或手动下载")
}

func parentProcess(pid int, expected string) (syscall.Handle, error) {
	h, err := syscall.OpenProcess(0x00100000|0x1000, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	var buf [32768]uint16
	size := uint32(len(buf))
	v, _, callErr := kernel32.NewProc("QueryFullProcessImageNameW").Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if v == 0 || !samePath(syscall.UTF16ToString(buf[:size]), expected) {
		syscall.CloseHandle(h)
		return 0, fmt.Errorf("原进程验证失败：%v", callErr)
	}
	return h, nil
}

func validateHealthLaunch(jobPath, exe, dataDir string) (updateJob, error) {
	job, err := loadUpdateJob(jobPath)
	if err != nil {
		return job, err
	}
	hash, err := fileHash(exe)
	if err != nil || !samePath(exe, filepath.Join(job.InstallDir, "PromptDock.exe")) || !samePath(dataDir, job.DataDir) || appVersion != job.Version || hash != job.NewHash {
		return job, errors.New("更新启动参数不匹配")
	}
	return job, nil
}

// All file replacement is in one directory on one volume. MoveFileEx performs
// a single-file atomic replacement; the old file is never removed first.
func swapExecutable(job updateJob, replace func(string, string) error) error {
	exe := filepath.Join(job.InstallDir, "PromptDock.exe")
	actual, err := fileHash(exe)
	if err != nil || actual != job.OldHash {
		return errors.New("当前程序已变化，升级已停止")
	}
	actual, err = fileHash(filepath.Join(job.Stage, "PromptDock.next.exe"))
	if err != nil || actual != job.NewHash {
		return errors.New("待安装主程序已变化，升级已停止")
	}
	actual, err = fileHash(filepath.Join(job.DataDir, "library.json"))
	if err != nil || actual != job.DataHash {
		return errors.New("个人库在备份后发生变化，升级已停止，请重新备份")
	}
	if err := copySynced(exe, filepath.Join(job.Stage, "PromptDock.previous.exe")); err != nil {
		return err
	}
	if err := writeJournal(job, "swapping", "准备原子替换主程序"); err != nil {
		return err
	}
	if err := replace(filepath.Join(job.Stage, "PromptDock.next.exe"), exe); err != nil {
		_ = writeJournal(job, "failed", "替换失败，旧版保持可用："+err.Error())
		return err
	}
	return writeJournal(job, "swapped", "等待新版本启动和数据兼容性确认")
}

func rollbackUpdate(job updateJob, replace func(string, string) error) error {
	// No other instance may write during rollback. Preserve any failed-version
	// bytes before restoring the exact compatible pre-update backup.
	library := filepath.Join(job.DataDir, "library.json")
	current, err := os.ReadFile(library)
	if err != nil {
		return err
	}
	if hashBytes(current) != job.DataHash {
		if err := writeSynced(filepath.Join(filepath.Dir(job.Backup), "failed-version-library.json"), current, 0600); err != nil {
			return err
		}
		backup, err := os.ReadFile(job.Backup)
		if err != nil || hashBytes(backup) != job.DataHash {
			return errors.New("原始数据备份校验失败，停止回退")
		}
		if err := atomicWrite(library, backup); err != nil {
			return err
		}
	}
	previous := filepath.Join(job.Stage, "PromptDock.previous.exe")
	actual, err := fileHash(previous)
	if err != nil || actual != job.OldHash {
		return errors.New("旧主程序备份不可用")
	}
	if err := replace(previous, filepath.Join(job.InstallDir, "PromptDock.exe")); err != nil {
		return err
	}
	return writeJournal(job, "rolled-back", "新版未通过启动确认，已恢复旧程序及兼容的个人数据；升级前备份仍保留")
}

func applyUpdate(jobPath string) error {
	job, err := loadUpdateJob(jobPath)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil || !samePath(self, filepath.Join(job.Stage, "updater.exe")) {
		return errors.New("更新助手位置不正确")
	}
	parent, err := parentProcess(job.ParentPID, filepath.Join(job.InstallDir, "PromptDock.exe"))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(parent)
	if err := writeSynced(filepath.Join(job.Stage, "ready"), []byte(job.Nonce), 0600); err != nil {
		return err
	}
	result, err := syscall.WaitForSingleObject(parent, 120000)
	if err != nil || result != syscall.WAIT_OBJECT_0 {
		return errors.New("等待原程序退出超时，旧程序未被替换")
	}
	gate, err := acquireFileLock(filepath.Join(job.InstallDir, ".promptdock-install.lock"))
	if err != nil {
		return err
	}
	defer gate.Close()
	dataLock, err := acquireFileLock(filepath.Join(job.DataDir, "library.lock"))
	if err != nil {
		return err
	}
	defer dataLock.Close()
	if err := swapExecutable(job, replaceFile); err != nil {
		actual, _ := fileHash(filepath.Join(job.InstallDir, "PromptDock.exe"))
		if actual == job.NewHash {
			if rollbackErr := rollbackUpdate(job, replaceFile); rollbackErr != nil {
				return fmt.Errorf("更新日志写入失败，自动回退也未完成：%v；%w", err, rollbackErr)
			}
		}
		return fmt.Errorf("替换失败，旧版与备份保留。请关闭占用文件的程序，或把新版解压到有写入权限的新目录：%w", err)
	}
	dataLock.Close()
	cmd := hiddenCommand(filepath.Join(job.InstallDir, "PromptDock.exe"), "--update-health", jobPath)
	cmd.Env = append(os.Environ(), "PROMPTDOCK_DATA_DIR="+filepath.Dir(job.DataDir))
	if err := cmd.Start(); err == nil {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		for start := time.Now(); time.Since(start) < 20*time.Second; {
			select {
			case <-done:
				goto rollback
			default:
			}
			health, err := os.ReadFile(filepath.Join(job.Stage, "healthy"))
			if err == nil && string(health) == job.Nonce {
				actual, err := fileHash(filepath.Join(job.DataDir, "library.json"))
				if err == nil && actual == job.DataHash {
					if err := writeJournal(job, "complete", "更新完成；个人数据备份："+job.Backup); err == nil {
						if err := writeSynced(filepath.Join(job.Stage, "committed"), []byte(job.Nonce), 0600); err == nil {
							return nil
						}
					}
					break
				}
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = cmd.Process.Kill()
		<-done
	}
rollback:
	dataLock, err = acquireFileLock(filepath.Join(job.DataDir, "library.lock"))
	if err != nil {
		return fmt.Errorf("无法取得数据锁，备份保留，停止回退：%w", err)
	}
	if err := rollbackUpdate(job, replaceFile); err != nil {
		return fmt.Errorf("回退未完成，请保留程序及数据备份并手动恢复：%w", err)
	}
	dataLock.Close()
	gate.Close()
	restart := hiddenCommand(filepath.Join(job.InstallDir, "PromptDock.exe"), "--update-recovery")
	restart.Env = append(os.Environ(), "PROMPTDOCK_DATA_DIR="+filepath.Dir(job.DataDir))
	if err := restart.Start(); err != nil {
		return err
	}
	return restart.Process.Release()
}

func pendingUpdateMessage(exe string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), ".promptdock-update.json"))
	if err != nil {
		return ""
	}
	var journal updateJournal
	if json.Unmarshal(data, &journal) != nil {
		return ""
	}
	switch journal.State {
	case "swapping", "swapped":
		return "上次软件更新未完成确认。程序文件采用原子替换；当前数据格式仍为 1。若当前版本无法使用，请关闭程序，从以下目录恢复旧程序及 library.json（两者需配套）：\n" + journal.Job.Stage + "\n" + journal.Job.Backup
	case "rolled-back", "failed":
		return journal.Message + "\n备份：" + journal.Job.Backup
	}
	return ""
}
