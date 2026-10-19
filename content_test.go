package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func contentRepository(t *testing.T) *Repository {
	t.Helper()
	lib, err := bundledLibrary()
	if err != nil {
		t.Fatal(err)
	}
	return &Repository{LibraryPath: filepath.Join(t.TempDir(), "library.json"), Library: lib}
}

func encodeLibrary(t *testing.T, lib Library) []byte {
	t.Helper()
	data, err := json.Marshal(lib)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sameJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

type publicPromptExpectation struct {
	task, kind, bodySHA256 string
}

// These hashes are a release-boundary check: public source and tests need no
// candidate documents, while an unintended prompt-body rewrite still fails CI.
var publicPromptExpectations = map[string]publicPromptExpectation{
	"collection-01-03":         {"ai-dialogue", "adaptation-zh", "656dcb711b673105582a857bcd9e4601f73afa1a0528cf8173f95d10efb168ce"},
	"collection-01-04":         {"learning-understanding", "adaptation-zh", "84d90a624bc021fbf3460b09de30fd4117c63685dff1f14aeb76e4443f525ba5"},
	"collection-01-05":         {"learning-understanding", "adaptation-zh", "200dad764a17019150dba815aa03242d16e5c8ec808671b27318298dd43f0347"},
	"collection-01-06":         {"writing-expression", "adaptation-zh", "ec231dba3599271efa471b8756552dd3dd0da99fc3553ad5f8cd855986dda694"},
	"collection-01-07":         {"work-life-communication", "adaptation-zh", "76e8b3a3345f5a9ffd24f89cfc3680b2d1fe6fa374aaa154f0b574ccb6ca124a"},
	"collection-01-08":         {"reading-organizing", "adaptation-zh", "69ec28fd84dc0944032a04a855e9fac2641d7a47812da82175416644327c4972"},
	"collection-01-09":         {"analysis-decisions", "adaptation-zh", "d8886974792bd042be35454f18045156051309a0674f2b8f1345dedd6f527134"},
	"collection-01-10":         {"analysis-decisions", "adaptation-zh", "1d780e51fac6a6d5846de4a2286b2397988fb2a5ea013eb1d290f908596d9c33"},
	"writing-03-02":            {"writing-expression", "original", "5391577b2bd6d3266aa4f861027a55440bea2647d7abde12df0085d4088360ac"},
	"reading-planning-04-01":   {"reading-organizing", "adaptation-zh", "953e4fb82930144c55a4faaec7901c385a22c282bfea915738dee77e70a5b118"},
	"reading-planning-04-02":   {"learning-understanding", "adaptation-zh", "2257c5e5dd41918e7d3733f43c97f47e780bdff3334f6fa120cb6dd1fca7dc37"},
	"work-communication-05-03": {"work-life-communication", "translation-zh", "62e795c10418d8b1d3adbc094bde26c341c97fa95767f28fcaa6aea99de9fa06"},
	"remaining-06-03":          {"writing-expression", "adaptation-zh", "23bebd2358d608fb8b9fcdd98ea540144265ae4c82a9f5865d1bcba7678d8e73"},
	"remaining-06-04":          {"writing-expression", "adaptation-zh", "be0ab592cbbee113548ea688846b9e5caf18640b4a6dddc77dd436d918ebedb0"},
	"remaining-06-05":          {"writing-expression", "adaptation-zh", "03b9195bb5fd8791b35dbbc8f53642818599e2b76de76a89ff7a715bed21a4b0"},
	"remaining-06-06":          {"analysis-decisions", "translation-zh", "68d915b533c2087bd07e491f12c8bdd131efdab4cf7a0646265e26f9a59e32a0"},
	"remaining-06-07":          {"reading-organizing", "adaptation-zh", "b00bf0931a371a6072c279194de23fa97e93935c7ca66ae775c1f5345777b8af"},
	"remaining-06-08":          {"code", "original", "2254644d1addb2dd9bf3436c1d4ce76564217de7b2725cc8a8e0d6772f3a59e1"},
}

func promptBodySHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func TestBundledCollectionUsesApprovedBodiesAndAttribution(t *testing.T) {
	r := contentRepository(t)
	if len(r.Library.Prompts) != 18 || len(r.Library.Tasks) != 7 {
		t.Fatal("expected Collection 01 plus ten publicly distributable batch entries and seven task groups")
	}
	if len(publicPromptExpectations) != len(r.Library.Prompts) {
		t.Fatal("public prompt manifest is incomplete")
	}
	for _, p := range r.Library.Prompts {
		want, ok := publicPromptExpectations[p.ID]
		if !ok || p.PrimaryTaskID != want.task || p.Source.Kind != want.kind || promptBodySHA256(p.Body) != want.bodySHA256 {
			t.Fatalf("unexpected public content: %s", p.ID)
		}
		if p.Origin != "bundled" || p.Status != "collected" || p.Source.Author == "" || p.Source.URL == "" || p.Source.License == "" || p.Source.Notice == "" || p.Source.LicenseURL == "" {
			t.Fatalf("missing public attribution: %s", p.ID)
		}
		if p.Source.Kind == "translation-zh" && !strings.Contains(r.promptInfo(p), "版本说明：中文翻译版") {
			t.Fatalf("translation is not shown as a readable version: %s", p.ID)
		}
	}
	for query, want := range map[string]string{"交接": "collection-01-03", "访谈": "collection-01-08", "知识盲点": "collection-01-05", "讲解练习 理解检查": "collection-01-05", "情况专报": "remaining-06-04", "代码阅读": "remaining-06-08"} {
		results := r.VisiblePrompts("all", "", query)
		if len(results) == 0 || results[0].Prompt.ID != want {
			t.Fatalf("search %q: %+v", query, results)
		}
	}
}

func TestExistingLibraryGetsOnlyMissingContentAcrossRestarts(t *testing.T) {
	base := t.TempDir()
	t.Setenv("PROMPTDOCK_DATA_DIR", base)
	dir := filepath.Join(base, "PromptDock")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := seedLibrary()
	legacy.Prompts[0].Title = "我的旧稿"
	legacy.Prompts[0].LocalModified = true
	legacy.Prompts[1].Deleted = true
	legacy.Settings.AlwaysOnTop = true
	if err := os.WriteFile(filepath.Join(dir, "library.json"), encodeLibrary(t, legacy), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Library.Prompts) != 48 || !reflect.DeepEqual(r.Library.Prompts[:30], legacy.Prompts) || !reflect.DeepEqual(r.Library.Tasks[:6], legacy.Tasks) || !taskExists(r.Library.Tasks, unclassifiedTaskID) || !r.Library.Settings.AlwaysOnTop {
		t.Fatal("startup replaced existing data")
	}
	p := r.PromptByID("collection-01-03")
	p.Title, p.Description, p.Body = "个人交接", "个人用途", "我的第一行\n我的第二行 📝"
	p.Tags = []string{"个人标签", "项目交接"}
	p.PrimaryTaskID = "code"
	if err := r.UpsertPrompt(*p); err != nil {
		t.Fatal(err)
	}
	if err := r.DeletePrompt("collection-01-04"); err != nil {
		t.Fatal(err)
	}
	before := cloneLibrary(r.Library)
	r, err = NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(r.Library, before) {
		t.Fatal("restart changed saved personal content or tombstones")
	}
}

func TestPersonalScenesAndExportRoundTrip(t *testing.T) {
	r := contentRepository(t)
	for _, p := range []Prompt{
		{ID: "my-academic", Title: "论文润色", Description: "整理学术表述", Body: "只处理我指定的段落。\n保留事实与引用。", PrimaryTaskID: "writing-expression", Tags: parseTags("论文、学术润色，论文;AI 表达"), Source: Source{Kind: "personal"}},
		{ID: "my-daily", Title: "文本润色", Body: "处理日常文章。", PrimaryTaskID: "writing-expression", Tags: []string{"日常文章"}},
	} {
		if err := r.UpsertPrompt(p); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.VisiblePrompts("all", "", "论文 学术润色")) != 1 || len(r.Library.Prompts) != 20 {
		t.Fatal("personal scenes were lost or blocked")
	}
	p := r.PromptByID("collection-01-05")
	p.Body = strings.Repeat("长正文，不改变来源。\n", 10000)
	p.Tags = []string{"个人场景", "知识盲点"}
	p.RelatedTaskIDs = []string{"ai-dialogue"}
	if err := r.UpsertPrompt(*p); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFavorite(p.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordUse(p.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.json")
	if err := r.Export(path, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dest := &Repository{LibraryPath: filepath.Join(t.TempDir(), "library.json")}
	added, _, _, err := dest.Import(data)
	if err != nil || added != 20 {
		t.Fatalf("round trip import: %d, %v", added, err)
	}
	if !sameJSON(r.Library.Prompts, dest.Library.Prompts) || !reflect.DeepEqual(r.Library.Tasks, dest.Library.Tasks) {
		t.Fatal("export/import lost content, sources or edits")
	}
	if added, _, conflicts, err := dest.Import(data); err != nil || added != 0 || conflicts != 0 {
		t.Fatal("same export should not create copies")
	}
}

func TestMetadataUpdatesBecomeStableConflictCopies(t *testing.T) {
	changes := map[string]func(*Prompt){
		"tags":     func(p *Prompt) { p.Tags = []string{"新的场景"} },
		"category": func(p *Prompt) { p.PrimaryTaskID = "writing-expression" },
		"related":  func(p *Prompt) { p.RelatedTaskIDs = []string{"reading-organizing"} },
		"license":  func(p *Prompt) { p.Source.License = "updated license" },
		"notice":   func(p *Prompt) { p.Source.Notice += "\nUpdated notice" },
		"url":      func(p *Prompt) { p.Source.URL += "?version=2" },
		"version":  func(p *Prompt) { p.ContentVersion++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			r := contentRepository(t)
			before := cloneLibrary(r.Library)
			incoming := clonePrompt(r.Library.Prompts[0])
			change(&incoming)
			data := encodeLibrary(t, Library{SchemaVersion: 1, Prompts: []Prompt{incoming}})
			report, err := r.PreviewImport(data)
			if err != nil || report.Added != 1 || len(report.Conflicts) != 1 || !reflect.DeepEqual(before, r.Library) {
				t.Fatalf("bad preview: %+v %v", report, err)
			}
			added, skipped, conflicts, err := r.Import(data)
			if err != nil || added != 1 || skipped != 0 || conflicts != 1 {
				t.Fatalf("bad import: %d %d %d %v", added, skipped, conflicts, err)
			}
			if !reflect.DeepEqual(before.Prompts[0], r.Library.Prompts[0]) {
				t.Fatal("original was overwritten")
			}
			copy := r.Library.Prompts[len(r.Library.Prompts)-1]
			copy.Body += "\nMy change to the imported copy"
			if err := r.UpsertPrompt(copy); err != nil {
				t.Fatal(err)
			}
			if added, _, _, err := r.Import(data); err != nil || added != 0 {
				t.Fatal("reimport duplicated edited conflict copy")
			}
			if err := r.DeletePrompt(copy.ID); err != nil {
				t.Fatal(err)
			}
			if added, _, _, err := r.Import(data); err != nil || added != 0 {
				t.Fatal("reimport resurrected deleted conflict copy")
			}
		})
	}
}

func TestImportUsageOnlyChangesDoNotOverwriteLocalUse(t *testing.T) {
	r := contentRepository(t)
	if err := r.RecordUse(r.Library.Prompts[0].ID); err != nil {
		t.Fatal(err)
	}
	before := cloneLibrary(r.Library)
	added, skipped, conflicts, err := r.Import(bundledContent)
	if err != nil || added != 0 || skipped != 18 || conflicts != 0 || !reflect.DeepEqual(before, r.Library) {
		t.Fatal("usage changes caused duplicate or overwrite")
	}
}

func TestBundledUpdateAddsOnlyTheTenPublicBatchEntries(t *testing.T) {
	var collection01 Library
	if err := json.Unmarshal(collection01Content, &collection01); err != nil {
		t.Fatal(err)
	}
	collection01.Prompts[0].Deleted = true
	r := &Repository{LibraryPath: filepath.Join(t.TempDir(), "library.json"), Library: collection01}
	report, err := r.importContent(bundledContent, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 10 || report.Skipped != 8 || report.TasksAdded != 1 || len(report.Conflicts) != 0 {
		t.Fatalf("unexpected public update report: %+v", report)
	}
	if r.PromptByID("collection-01-03") != nil {
		t.Fatal("bundled update revived a deleted item")
	}
	if len(r.Library.Prompts) != 18 || r.PromptByID("remaining-06-08") == nil {
		t.Fatal("public batch was not added once")
	}
	if report, err := r.importContent(bundledContent, true); err != nil || report.Added != 0 || report.Skipped != 18 {
		t.Fatalf("repeat bundled update was not idempotent: %+v, %v", report, err)
	}
}

func TestImportValidationAndFailedCommitAreAtomic(t *testing.T) {
	r := contentRepository(t)
	before := cloneLibrary(r.Library)
	data := encodeLibrary(t, Library{SchemaVersion: 1, Tasks: []Task{{ID: "new", Title: "新分类"}}, Prompts: []Prompt{
		{ID: "valid", Title: "正常", Body: "完整正文", PrimaryTaskID: "new"},
		{ID: "invalid", Title: "无效", Body: "正文", PrimaryTaskID: "missing"},
	}})
	if _, err := r.PreviewImport(data); err == nil {
		t.Fatal("preview accepted unknown category")
	}
	if _, _, _, err := r.Import(data); err == nil || !reflect.DeepEqual(before, r.Library) {
		t.Fatal("failed import changed library")
	}
	r.LibraryPath = filepath.Join(t.TempDir(), "missing", "library.json")
	data = encodeLibrary(t, Library{SchemaVersion: 1, Prompts: []Prompt{{ID: "new", Title: "正常", Body: "正文", PrimaryTaskID: "ai-dialogue"}}})
	if a, s, c, err := r.Import(data); err == nil || a+s+c != 0 || !reflect.DeepEqual(before, r.Library) {
		t.Fatal("failed disk write changed library or reported success")
	}
}
