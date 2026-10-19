//go:build windows && updatefixture

package main

// Local integration fixture only. Not compiled into published executables.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type fixtureTransport struct{ releases, checksums, archive []byte }

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var data []byte
	switch {
	case req.URL.Host == "api.github.com":
		data = f.releases
	case strings.HasSuffix(req.URL.Path, "SHA256SUMS.txt"):
		data = f.checksums
	case strings.HasSuffix(req.URL.Path, ".zip"):
		data = f.archive
	default:
		return nil, fmt.Errorf("unexpected fixture request %s", req.URL)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: req}, nil
}
func init() {
	if len(os.Args) > 1 && os.Args[1] == "--update-health" && os.Getenv("PROMPTDOCK_FIXTURE_FAIL_HEALTH") == "1" {
		os.Exit(9)
	}
	if len(os.Args) != 3 || os.Args[1] != "--fixture-upgrade" {
		return
	}
	runtime.LockOSThread()
	if err := fixtureUpgrade(os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func fixtureUpgrade(archive string) error {
	dir, err := dataDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := acquireFileLock(filepath.Join(dir, "library.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	r, err := NewRepository()
	if err != nil {
		return err
	}
	p := *r.PromptByID("collection-01-03")
	p.Title = "隔离测试：个人标题"
	p.Body = "个人修改第一行\n保留我自己的问题"
	p.PrimaryTaskID = "code"
	p.Tags = []string{"虚构标签"}
	p.Source.Author = "虚构个人来源"
	if err := r.UpsertPrompt(p); err != nil {
		return err
	}
	for _, err := range []error{r.UpsertPrompt(Prompt{ID: "fictional-personal", Title: "虚构自建条目", Body: "自建正文", PrimaryTaskID: "code", Origin: "personal"}), r.SetFavorite(p.ID, true), r.RecordUse(p.ID), r.DeletePrompt("collection-01-04"), r.SetAlwaysOnTop(true), r.SetPopupEntry(popupEntryRecent), r.SetAutoUpdateCheck(false)} {
		if err != nil {
			return err
		}
	}
	before, err := os.ReadFile(r.LibraryPath)
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(filepath.Dir(dir), "before.json"), before, 0600); err != nil {
		return err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	version := "0.2.0"
	tag := "v" + version
	name := "PromptDock-" + version + "-windows-amd64.zip"
	base := "https://github.com/example/PromptDock/releases/"
	checksums := []byte(hashBytes(data) + "  " + name + "\n")
	releases, _ := json.Marshal([]githubRelease{{Tag: tag, URL: base + "tag/" + tag, Body: "隔离本地升级夹具", Assets: []releaseAsset{{Name: name, URL: base + "download/" + tag + "/" + name, Size: int64(len(data)), State: "uploaded"}, {Name: "SHA256SUMS.txt", URL: base + "download/" + tag + "/SHA256SUMS.txt", Size: int64(len(checksums)), State: "uploaded"}}}})
	client := newUpdateClient("example/PromptDock")
	client.client.Transport = fixtureTransport{releases: releases, checksums: checksums, archive: data}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	offer, err := client.check(ctx, appVersion)
	if err != nil {
		return err
	}
	if offer == nil {
		return fmt.Errorf("no fixture update")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	stage, err := prepareUpdateStage(exe)
	if err != nil {
		return err
	}
	if err := client.download(ctx, offer, stage); err != nil {
		return err
	}
	job, err := prepareUpdateJob(exe, r.LibraryPath, stage, offer.Version)
	if err != nil {
		return err
	}
	return startUpdateHelper(job)
}
