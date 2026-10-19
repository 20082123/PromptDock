//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type uninstallJob struct {
	Directory string
	ParentPID int
	Hash      string
	DataDir   string
}

func validateUninstallDirectory(dir string) error {
	dir, err := canonicalDirectory(dir)
	if err != nil {
		return err
	}
	if filepath.Dir(dir) == dir {
		return errors.New("不能从磁盘根目录运行卸载，请把发布文件放在独立目录")
	}
	for _, name := range []string{"go.mod", ".git", "app_windows.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return errors.New("这是开发工作区，卸载程序不会删除源码；请在解压的 Windows 发布目录中使用")
		}
	}
	return nil
}

func uninstallMain() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if err := validateUninstallDirectory(dir); err != nil {
		return err
	}
	if !confirmBox(0, "卸载 PromptDock", "将删除此目录中的 PromptDock 程序及随包说明文件。\n\n个人提示词、设置和备份全部保留在原数据目录，不会删除。\n\n请先退出正在运行的 PromptDock。是否继续？") {
		return nil
	}
	gate, err := acquireFileLock(filepath.Join(dir, ".promptdock-install.lock"))
	if err != nil {
		return err
	}
	gate.Close()
	dataDir, err := dataDirectory()
	if err != nil {
		return err
	}
	// Do not read the personal library. An existing application prevents removal.
	if _, err := os.Stat(dataDir); err == nil {
		lock, err := acquireFileLock(filepath.Join(dataDir, "library.lock"))
		if err != nil {
			return errors.New("PromptDock 仍在运行，请从托盘退出后重新卸载")
		}
		lock.Close()
	}
	tmp, err := os.MkdirTemp("", "PromptDock-uninstall-")
	if err != nil {
		return err
	}
	helper := filepath.Join(tmp, "uninstall-helper.exe")
	if err := copySynced(exe, helper); err != nil {
		return err
	}
	hash, err := fileHash(exe)
	if err != nil {
		return err
	}
	job := uninstallJob{Directory: dir, ParentPID: os.Getpid(), Hash: hash, DataDir: dataDir}
	data, _ := json.Marshal(job)
	name := filepath.Join(tmp, "uninstall.json")
	if err := writeSynced(name, data, 0600); err != nil {
		return err
	}
	cmd := hiddenCommand(helper, "--finish-uninstall", name)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func removeInstalledFiles(dir, hash string) error {
	if err := validateUninstallDirectory(dir); err != nil {
		return err
	}
	actual, err := fileHash(filepath.Join(dir, "PromptDock.exe"))
	if err != nil || actual != hash {
		return errors.New("程序文件已改变，停止卸载")
	}
	var failures []string
	for _, name := range windowsPackageFiles {
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			failures = append(failures, name)
			continue
		}
		if err := os.Remove(p); err != nil {
			failures = append(failures, name)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("部分文件正被占用，关闭相关程序后可手动删除：%s", strings.Join(failures, "、"))
	}
	return nil
}
func finishUninstall(name string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(name), filepath.Dir(self)) || filepath.Base(name) != "uninstall.json" {
		return errors.New("卸载作业路径无效")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	var job uninstallJob
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	if err := validateUninstallDirectory(job.Directory); err != nil {
		return err
	}
	parent, err := parentProcess(job.ParentPID, filepath.Join(job.Directory, "PromptDock.exe"))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(parent)
	result, err := syscall.WaitForSingleObject(parent, 30000)
	if err != nil || result != syscall.WAIT_OBJECT_0 {
		return errors.New("等待卸载入口退出超时")
	}
	gate, err := acquireFileLock(filepath.Join(job.Directory, ".promptdock-install.lock"))
	if err != nil {
		return err
	}
	defer gate.Close()
	if _, err := os.Stat(job.DataDir); err == nil {
		lock, err := acquireFileLock(filepath.Join(job.DataDir, "library.lock"))
		if err != nil {
			return errors.New("PromptDock 仍在运行，请退出后重新卸载")
		}
		defer lock.Close()
	}
	if err := removeInstalledFiles(job.Directory, job.Hash); err != nil {
		return err
	}
	messageBox(0, "PromptDock 已卸载", "程序文件已移除。个人提示词、设置和备份均已保留；您可以手动删除解压目录中剩余的文件。")
	return nil
}
