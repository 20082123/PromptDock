package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableVersionComparison(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{{"v1.10.0", "1.9.9", 1}, {"0.2.0", "0.2.0", 0}, {"0.2.0+build.5", "v0.2.0", 0}, {"1.0.0", "2.0.0", -1}} {
		got, err := compareVersions(tt.a, tt.b)
		if err != nil || got != tt.want {
			t.Fatalf("%s vs %s = %d, %v", tt.a, tt.b, got, err)
		}
	}
	for _, v := range []string{"1.0", "v1.0.0-rc.1", "01.2.0", "999999999999999999999.0.0", "1.2.3/evil", "v1.2.3\n"} {
		if _, err := versionParts(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}

type routeTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (r routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *copy.URL
	u.Scheme = r.base.Scheme
	u.Host = r.base.Host
	copy.URL = &u
	return r.transport.RoundTrip(copy)
}
func mockUpdateClient(t *testing.T, handler http.HandlerFunc) *updateClient {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	c := newUpdateClient("example/PromptDock")
	c.client.Transport = routeTransport{base: base, transport: server.Client().Transport}
	c.arch = "amd64"
	return c
}
func mockRelease(version, arch string) githubRelease {
	tag := "v" + version
	root := "https://github.com/example/PromptDock/releases/"
	name := "PromptDock-" + version + "-windows-" + arch + ".zip"
	return githubRelease{Tag: tag, URL: root + "tag/" + tag, Body: "版本摘要", Assets: []releaseAsset{
		{Name: name, URL: root + "download/" + tag + "/" + name, Size: 100, State: "uploaded"},
		{Name: "SHA256SUMS.txt", URL: root + "download/" + tag + "/SHA256SUMS.txt", Size: 100, State: "uploaded"},
	}}
}
func TestCheckUpdateFiltersAndErrors(t *testing.T) {
	stable := mockRelease("0.3.0", "amd64")
	draft := mockRelease("9.0.0", "amd64")
	draft.Draft = true
	pre := mockRelease("8.0.0", "amd64")
	pre.Prerelease = true
	wrong := mockRelease("7.0.0", "arm64")
	badURL := mockRelease("6.0.0", "amd64")
	badURL.Assets[0].URL = "https://evil.example/package.zip"
	invalid := mockRelease("x", "amd64")
	preTag := mockRelease("0.4.0-rc.1", "amd64")
	c := mockUpdateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || r.Method != "GET" {
			t.Error("missing anonymous read-only request headers")
		}
		json.NewEncoder(w).Encode([]githubRelease{draft, pre, wrong, badURL, invalid, preTag, stable, mockRelease("0.2.0", "amd64")})
	})
	offer, err := c.check(context.Background(), "0.2.0")
	if err != nil || offer == nil || offer.Version != "0.3.0" {
		t.Fatalf("%+v %v", offer, err)
	}
	offer, err = c.check(context.Background(), "10.0.0")
	if err != nil || offer != nil {
		t.Fatalf("no update: %+v %v", offer, err)
	}
	c.repository = ""
	if _, err = c.check(context.Background(), "0.2.0"); !errors.Is(err, errUpdateSourceDisabled) {
		t.Fatal(err)
	}
	c.repository = "../evil"
	if _, err = c.check(context.Background(), "0.2.0"); err == nil {
		t.Fatal("invalid source accepted")
	}
	c = mockUpdateClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	if _, err = c.check(context.Background(), "0.2.0"); err == nil {
		t.Fatal("network error accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.check(ctx, "0.2.0"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func fixturePE() []byte {
	// Minimal parseable PE for package-validation tests only, never executed.
	b := make([]byte, 512)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x80)
	copy(b[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x84:], 0x8664)
	binary.LittleEndian.PutUint16(b[0x94:], 240)
	binary.LittleEndian.PutUint16(b[0x98:], 0x20b)
	binary.LittleEndian.PutUint16(b[0x98+68:], 2)
	binary.LittleEndian.PutUint32(b[0x98+108:], 16)
	return b
}
func testUpdateZIP(t *testing.T, version string, mutate func(map[string][]byte)) []byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range windowsPackageFiles {
		files[name] = []byte("test notice")
	}
	files["PromptDock.exe"] = fixturePE()
	m := updateManifest{Version: version, OS: "windows", Arch: "amd64", Schema: 1, ExecutableSHA256: hashBytes(files["PromptDock.exe"])}
	files["update-manifest.json"], _ = json.Marshal(m)
	if mutate != nil {
		mutate(files)
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		f, err := zw.Create("PromptDock-" + version + "-windows-amd64/" + name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestDownloadAndPackageProtection(t *testing.T) {
	data := testUpdateZIP(t, "0.3.0", nil)
	release := mockRelease("0.3.0", "amd64")
	release.Assets[0].Size = int64(len(data))
	checksum := []byte(hashBytes(data) + "  " + release.Assets[0].Name + "\n")
	release.Assets[1].Size = int64(len(checksum))
	mode := "ok"
	c := mockUpdateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS.txt") {
			w.Write(checksum)
			return
		}
		switch mode {
		case "interrupted":
			w.Header().Set("Content-Length", "999999")
			w.Write(data[:len(data)/2])
		case "corrupt":
			w.Write([]byte("corrupt"))
		default:
			w.Write(data)
		}
	})
	offer := &UpdateOffer{Version: "0.3.0", ReleaseURL: release.URL, Package: release.Assets[0], Checksums: release.Assets[1]}
	stage := t.TempDir()
	if err := c.download(context.Background(), offer, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stage, "PromptDock.next.exe")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"interrupted", "corrupt"} {
		mode = bad
		dir := t.TempDir()
		if err := c.download(context.Background(), offer, dir); err == nil {
			t.Fatal("accepted " + bad)
		}
		if _, err := os.Stat(filepath.Join(dir, "PromptDock.next.exe")); !os.IsNotExist(err) {
			t.Fatal("bad package extracted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.download(ctx, offer, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, entry := range []string{"library.json", "../escape.exe", "..\\escape.exe", "OTHER.EXE"} {
		bad := testUpdateZIP(t, "0.3.0", func(files map[string][]byte) { files[entry] = []byte("forbidden") })
		dir := t.TempDir()
		name := filepath.Join(dir, "test.zip")
		os.WriteFile(name, bad, 0600)
		if err := extractUpdate(name, dir, "0.3.0", "amd64"); err == nil {
			t.Fatalf("accepted %s", entry)
		}
	}
	bad := testUpdateZIP(t, "0.3.0", func(files map[string][]byte) { files["PromptDock.exe"] = []byte("tampered") })
	dir := t.TempDir()
	name := filepath.Join(dir, "test.zip")
	os.WriteFile(name, bad, 0600)
	if err := extractUpdate(name, dir, "0.3.0", "amd64"); err == nil {
		t.Fatal("accepted tampered exe")
	}
}

func TestUpdateRedirectPolicy(t *testing.T) {
	c := newUpdateClient("example/PromptDock")
	for _, address := range []string{"http://github.com/file", "https://evil.example/file", "https://github.com:8443/file", "https://user:pass@github.com/file"} {
		req, _ := http.NewRequest("GET", address, nil)
		if c.client.CheckRedirect(req, nil) == nil {
			t.Fatal("unsafe redirect accepted")
		}
	}
	req, _ := http.NewRequest("GET", "https://release-assets.githubusercontent.com/file?token=example", nil)
	if err := c.client.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
}
func TestBackupAndUnknownSchemaPreservePersonalBytes(t *testing.T) {
	base := t.TempDir()
	t.Setenv("PROMPTDOCK_DATA_DIR", base)
	r, err := NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	p := r.Library.Prompts[0]
	p.Title = "虚构个人标题"
	p.Body = "个人正文\n第二行"
	p.Tags = []string{"自选标签"}
	p.Source.Author = "虚构作者"
	if err := r.UpsertPrompt(p); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordUse(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFavorite(p.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.DeletePrompt(r.Library.Prompts[1].ID); err != nil {
		t.Fatal(err)
	}
	r.SetAutoUpdateCheck(false)
	r.SetAlwaysOnTop(true)
	r.SetPopupEntry(popupEntryFavorite)
	before, _ := os.ReadFile(r.LibraryPath)
	backup, hash, err := backupPersonalData(r.LibraryPath)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(backup)
	if !bytes.Equal(before, data) || hash != hashBytes(before) {
		t.Fatal("backup changed data/settings/tombstones")
	}
	invalid := []byte(`{"schemaVersion":99,"prompts":[{"id":"do-not-delete"}]}`)
	os.WriteFile(r.LibraryPath, invalid, 0600)
	if repo, err := NewRepository(); err == nil || repo != nil {
		t.Fatal("incompatible library was loaded")
	}
	after, _ := os.ReadFile(r.LibraryPath)
	if !bytes.Equal(invalid, after) {
		t.Fatal("incompatible library overwritten")
	}
	if _, _, err := backupPersonalData(r.LibraryPath); err == nil {
		t.Fatal("backed up incompatible library for upgrade")
	}
}
