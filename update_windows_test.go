//go:build windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExclusiveDataLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.lock")
	first, err := acquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := acquireFileLock(path); err == nil {
		second.Close()
		t.Fatal("second writer acquired lock")
	}
	first.Close()
	third, err := acquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
}

func TestDataLockAcrossProcesses(t *testing.T) {
	if path := os.Getenv("PROMPTDOCK_LOCK_TEST_PATH"); path != "" {
		lock, err := acquireFileLock(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := os.WriteFile(path+".ready", []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		for start := time.Now(); time.Since(start) < 5*time.Second; {
			if _, err := os.Stat(path + ".release"); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("parent did not release lock helper")
	}
	path := filepath.Join(t.TempDir(), "library.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDataLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "PROMPTDOCK_LOCK_TEST_PATH="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := false
	for start := time.Now(); time.Since(start) < 4*time.Second; {
		if _, err := os.Stat(path + ".ready"); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("lock helper did not become ready")
	}
	if lock, err := acquireFileLock(path); err == nil {
		lock.Close()
		t.Fatal("another process acquired the data lock")
	}
	if err := os.WriteFile(path+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireFileLock(path)
	if err != nil {
		t.Fatal("lock not released when other process exited:", err)
	}
	lock.Close()
}
func fixtureJob(t *testing.T) updateJob {
	t.Helper()
	install := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "PromptDock")
	os.MkdirAll(dataDir, 0700)
	r := &Repository{LibraryPath: filepath.Join(dataDir, "library.json"), Library: seedLibrary()}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(install, ".promptdock-update-")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(install, "PromptDock.exe"), []byte("old executable"), 0700)
	os.WriteFile(filepath.Join(stage, "PromptDock.next.exe"), []byte("new executable"), 0700)
	job, err := prepareUpdateJob(filepath.Join(install, "PromptDock.exe"), r.LibraryPath, stage, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func TestReplacementFailureAndRollbackPreserveCompatibleData(t *testing.T) {
	job := fixtureJob(t)
	original, _ := os.ReadFile(filepath.Join(job.DataDir, "library.json"))
	if err := swapExecutable(job, func(string, string) error { return errors.New("file occupied") }); err == nil {
		t.Fatal("replacement failure ignored")
	}
	old, _ := os.ReadFile(filepath.Join(job.InstallDir, "PromptDock.exe"))
	if string(old) != "old executable" {
		t.Fatal("old version lost after replacement failure")
	}
	job = fixtureJob(t)
	original, _ = os.ReadFile(filepath.Join(job.DataDir, "library.json"))
	if err := swapExecutable(job, replaceFile); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(job.DataDir, "library.json"), []byte(`{"schemaVersion":999}`), 0600)
	if err := rollbackUpdate(job, replaceFile); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(job.DataDir, "library.json"))
	if !bytes.Equal(original, after) {
		t.Fatal("data rollback lost pre-update data")
	}
	old, _ = os.ReadFile(filepath.Join(job.InstallDir, "PromptDock.exe"))
	if string(old) != "old executable" {
		t.Fatal("old executable not restored")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(job.Backup), "failed-version-library.json")); err != nil {
		t.Fatal("failed-version data was not preserved")
	}
}

func TestOccupiedExecutableKeepsOldProgramAndData(t *testing.T) {
	job := fixtureJob(t)
	exe := filepath.Join(job.InstallDir, "PromptDock.exe")
	name, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		t.Fatal(err)
	}
	// Allow reading/checksums but not deletion/replacement while this handle lives.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	if err := swapExecutable(job, replaceFile); err == nil {
		t.Fatal("replaced an executable held without delete sharing")
	}
	if hash, _ := fileHash(exe); hash != job.OldHash {
		t.Fatal("occupied old executable changed")
	}
	if hash, _ := fileHash(filepath.Join(job.DataDir, "library.json")); hash != job.DataHash {
		t.Fatal("replacement failure changed personal data")
	}
}
func TestChangedDataOrBackupFailureStopsUpgrade(t *testing.T) {
	job := fixtureJob(t)
	os.WriteFile(filepath.Join(job.DataDir, "library.json"), []byte(`{"schemaVersion":1,"changed":true}`), 0600)
	if err := swapExecutable(job, replaceFile); err == nil {
		t.Fatal("changed data upgraded without fresh backup")
	}
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "library.json"), []byte(`{"schemaVersion":1}`), 0600)
	os.WriteFile(filepath.Join(base, "backups"), []byte("blocked"), 0600)
	if _, _, err := backupPersonalData(filepath.Join(base, "library.json")); err == nil {
		t.Fatal("backup failure ignored")
	}
}
func TestUninstallOnlyRemovesKnownProgramFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range append(append([]string{}, windowsPackageFiles...), "library.json", "my-notes.txt") {
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0600)
	}
	hash, _ := fileHash(filepath.Join(dir, "PromptDock.exe"))
	if err := removeInstalledFiles(dir, hash); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"library.json", "my-notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("uninstaller removed personal file")
		}
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("source"), 0600)
	if err := validateUninstallDirectory(dir); err == nil {
		t.Fatal("source directory accepted")
	}
}
