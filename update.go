package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var errUpdateSourceDisabled = errors.New("更新源尚未启用")
var stableVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func versionParts(value string) ([3]uint64, error) {
	var parts [3]uint64
	m := stableVersionPattern.FindStringSubmatch(value)
	if m == nil {
		return parts, fmt.Errorf("不是稳定版本号：%q", value)
	}
	for i := range parts {
		v, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return parts, err
		}
		parts[i] = v
	}
	return parts, nil
}

func compareVersions(a, b string) (int, error) {
	x, err := versionParts(a)
	if err != nil {
		return 0, err
	}
	y, err := versionParts(b)
	if err != nil {
		return 0, err
	}
	for i := range x {
		if x[i] > y[i] {
			return 1, nil
		}
		if x[i] < y[i] {
			return -1, nil
		}
	}
	return 0, nil
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	State  string `json:"state"`
}
type githubRelease struct {
	Tag        string         `json:"tag_name"`
	Body       string         `json:"body"`
	URL        string         `json:"html_url"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}
type UpdateOffer struct {
	Version    string
	Summary    string
	ReleaseURL string
	Package    releaseAsset
	Checksums  releaseAsset
}
type updateClient struct {
	repository string
	arch       string
	client     *http.Client
}

func newUpdateClient(repository string) *updateClient {
	return &updateClient{repository: repository, arch: runtime.GOARCH, client: &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
				return errors.New("拒绝不安全的更新重定向")
			}
			switch req.URL.Hostname() {
			case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
				return nil
			}
			return errors.New("更新重定向不属于 GitHub 资产服务")
		},
	}}
}

func (c *updateClient) enabled() error {
	if c.repository == "" {
		return errUpdateSourceDisabled
	}
	if !repositoryPattern.MatchString(c.repository) || strings.Contains(c.repository, "..") {
		return errors.New("官方更新仓库配置无效")
	}
	return nil
}

func (c *updateClient) validReleaseURL(value, tag string) bool {
	return value == "https://github.com/"+c.repository+"/releases/tag/"+url.PathEscape(tag)
}
func (c *updateClient) validAssetURL(value, tag, name string) bool {
	return value == "https://github.com/"+c.repository+"/releases/download/"+url.PathEscape(tag)+"/"+url.PathEscape(name)
}

func (c *updateClient) get(ctx context.Context, address string, maxSize int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PromptDock/"+appVersion)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新服务返回 HTTP %d；请稍后重试", resp.StatusCode)
	}
	if resp.ContentLength > maxSize {
		return nil, errors.New("更新响应过大")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("更新响应过大")
	}
	return data, nil
}

func (c *updateClient) check(ctx context.Context, current string) (*UpdateOffer, error) {
	if err := c.enabled(); err != nil {
		return nil, err
	}
	if _, err := versionParts(current); err != nil {
		return nil, err
	}
	var best *UpdateOffer
	for page := 1; page <= 10; page++ {
		data, err := c.get(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", c.repository, page), 4<<20)
		if err != nil {
			return nil, err
		}
		var releases []githubRelease
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, errors.New("无法解析更新服务响应")
		}
		for _, rel := range releases {
			cmp, err := compareVersions(rel.Tag, current)
			if err != nil || rel.Draft || rel.Prerelease || cmp <= 0 || !c.validReleaseURL(rel.URL, rel.Tag) {
				continue
			}
			version := strings.TrimPrefix(rel.Tag, "v")
			name := "PromptDock-" + version + "-windows-" + c.arch + ".zip"
			offer := &UpdateOffer{Version: version, Summary: rel.Body, ReleaseURL: rel.URL}
			if len(offer.Summary) > 8000 {
				offer.Summary = string([]rune(offer.Summary)[:min(2000, len([]rune(offer.Summary)))])
			}
			seen := map[string]bool{}
			for _, a := range rel.Assets {
				if a.Name != name && a.Name != "SHA256SUMS.txt" {
					continue
				}
				if seen[a.Name] {
					offer.Package.URL = ""
					break
				}
				seen[a.Name] = true
				if a.State != "uploaded" || a.Size <= 0 || !c.validAssetURL(a.URL, rel.Tag, a.Name) {
					continue
				}
				if a.Name == name {
					offer.Package = a
				} else {
					offer.Checksums = a
				}
			}
			if offer.Package.URL == "" || offer.Checksums.URL == "" || offer.Package.Size > 40<<20 || offer.Checksums.Size > 64<<10 {
				continue
			}
			if best == nil {
				best = offer
			} else if cmp, _ := compareVersions(offer.Version, best.Version); cmp > 0 {
				best = offer
			}
		}
		if len(releases) < 100 {
			return best, nil
		}
	}
	return nil, errors.New("更新 Release 列表超出检查范围，请打开官方 Release 页面查看")
}

func checksumFor(data []byte, name string) (string, error) {
	var found string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		hash := strings.ToLower(fields[0])
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size || found != "" {
			return "", errors.New("发布校验信息无效或重复")
		}
		found = hash
	}
	if found == "" {
		return "", errors.New("发布包缺少 SHA-256 校验信息")
	}
	return found, nil
}
func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func fileHash(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *updateClient) download(ctx context.Context, offer *UpdateOffer, stage string) error {
	if err := c.enabled(); err != nil {
		return err
	}
	tag := path.Base(offer.ReleaseURL)
	if !c.validReleaseURL(offer.ReleaseURL, tag) || !c.validAssetURL(offer.Package.URL, tag, offer.Package.Name) || !c.validAssetURL(offer.Checksums.URL, tag, "SHA256SUMS.txt") {
		return errors.New("更新资产来源无效")
	}
	checksums, err := c.get(ctx, offer.Checksums.URL, 64<<10)
	if err != nil {
		return err
	}
	expected, err := checksumFor(checksums, offer.Package.Name)
	if err != nil {
		return err
	}
	if offer.Package.Digest != "" && offer.Package.Digest != "sha256:"+expected {
		return errors.New("GitHub 资产摘要与校验文件不一致")
	}
	data, err := c.get(ctx, offer.Package.URL, 40<<20)
	if err != nil {
		return err
	}
	if int64(len(data)) != offer.Package.Size || hashBytes(data) != expected {
		return errors.New("下载不完整或 SHA-256 校验失败；旧版本未改动")
	}
	archive := filepath.Join(stage, "download.zip")
	if err := writeSynced(archive, data, 0600); err != nil {
		return err
	}
	return extractUpdate(archive, stage, offer.Version, c.arch)
}

type updateManifest struct {
	Version          string `json:"version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Schema           int    `json:"librarySchema"`
	ExecutableSHA256 string `json:"executableSHA256"`
}

var windowsPackageFiles = []string{"PromptDock.exe", "Uninstall.exe", "README.txt", "LICENSE", "THIRD_PARTY_NOTICES.md", "RELEASE_NOTES.md", "update-manifest.json"}

// Inspect every ZIP entry before extraction. The updater writes only the main
// program; it never extracts library.json, settings, paths or executable extras.
func extractUpdate(archive, stage, version, arch string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	prefix := "PromptDock-" + version + "-windows-" + arch + "/"
	allowed := map[string]bool{}
	for _, f := range windowsPackageFiles {
		allowed[f] = true
	}
	seen := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.Name == prefix && f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if name == f.Name || !allowed[name] || seen[name] != nil || f.Mode()&os.ModeSymlink != 0 || f.FileInfo().IsDir() || f.UncompressedSize64 > 50<<20 {
			return fmt.Errorf("更新包含有不允许的路径或文件：%q", f.Name)
		}
		seen[name] = f
	}
	if len(seen) != len(allowed) {
		return errors.New("更新包文件不完整")
	}
	read := func(name string, limit int64) ([]byte, error) {
		f, err := seen[name].Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			return nil, errors.New("更新文件过大")
		}
		return data, nil
	}
	manifestData, err := read("update-manifest.json", 4096)
	if err != nil {
		return err
	}
	var manifest updateManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}
	if manifest.Version != version || manifest.OS != "windows" || manifest.Arch != arch || manifest.Schema != librarySchemaVersion {
		return errors.New("更新包的版本、架构或数据格式不兼容，请手动下载兼容版本")
	}
	exe, err := read("PromptDock.exe", 50<<20)
	if err != nil {
		return err
	}
	if hashBytes(exe) != manifest.ExecutableSHA256 {
		return errors.New("主程序校验失败")
	}
	name := filepath.Join(stage, "PromptDock.next.exe")
	if err := writeSynced(name, exe, 0700); err != nil {
		return err
	}
	pf, err := pe.Open(name)
	if err != nil {
		return errors.New("更新主程序不是有效的 Windows EXE")
	}
	defer pf.Close()
	if arch != "amd64" || pf.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("更新主程序架构不匹配")
	}
	opt, ok := pf.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || opt.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		return errors.New("更新包不是 Windows GUI 主程序")
	}
	return nil
}

func writeSynced(name string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(name)
		return writeErr
	}
	return closeErr
}

// Caller holds the data lock; the exact committed bytes (including settings and
// tombstones) are backed up and reread before any executable replacement.
func backupPersonalData(libraryPath string) (string, string, error) {
	data, err := os.ReadFile(libraryPath)
	if err != nil {
		return "", "", err
	}
	var header struct {
		Schema int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil || header.Schema != librarySchemaVersion {
		return "", "", errors.New("个人库损坏或格式不兼容，升级已停止，原文件保留")
	}
	base := filepath.Join(filepath.Dir(libraryPath), "backups")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", "", err
	}
	dir, err := os.MkdirTemp(base, "pre-update-"+time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return "", "", err
	}
	backup := filepath.Join(dir, "library.json")
	if err := writeSynced(backup, data, 0600); err != nil {
		return "", "", err
	}
	hash := hashBytes(data)
	actual, err := fileHash(backup)
	if err != nil || actual != hash {
		return "", "", errors.New("升级前备份验证失败")
	}
	return backup, hash, nil
}
