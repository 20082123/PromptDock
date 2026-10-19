package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSeedLibraryHasTasksAndPrompts(t *testing.T) {
	lib := seedLibrary()
	if len(lib.Tasks) != 6 || len(lib.Prompts) < 24 {
		t.Fatalf("unexpected seed size: tasks=%d prompts=%d", len(lib.Tasks), len(lib.Prompts))
	}
}

func TestDefaultBrowsePrioritizesFavoritesThenRecentAndHidesUntouchedLegacy(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	legacy := func(id string) Prompt {
		return Prompt{ID: id, Title: id, Body: "正文", Origin: "bundled", Status: "draft", Source: Source{Kind: "original-draft"}}
	}
	r := &Repository{Library: Library{Prompts: []Prompt{
		legacy("legacy-hidden"),
		{ID: "never-a", Title: "never-a", Body: "正文", Origin: "personal"},
		func() Prompt { p := legacy("legacy-edited"); p.LocalModified = true; return p }(),
		{ID: "recent-old", Title: "recent-old", Body: "正文", Origin: "personal", LastUsedAt: now.Add(-time.Hour)},
		{ID: "recent-new", Title: "recent-new", Body: "正文", Origin: "personal", LastUsedAt: now},
		{ID: "favorite-old", Title: "favorite-old", Body: "正文", Origin: "personal", Favorite: true, LastUsedAt: now.Add(-time.Hour)},
		{ID: "never-b", Title: "never-b", Body: "正文", Origin: "personal"},
		{ID: "favorite-new", Title: "favorite-new", Body: "正文", Origin: "personal", Favorite: true, LastUsedAt: now},
	}}}

	results := r.VisiblePrompts("default", "", "")
	want := []string{"favorite-new", "favorite-old", "recent-new", "recent-old", "never-a", "legacy-edited", "never-b"}
	if len(results) != len(want) {
		t.Fatalf("default count = %d, want %d", len(results), len(want))
	}
	for i, id := range want {
		if results[i].Prompt.ID != id {
			t.Fatalf("default result %d = %s, want %s", i, results[i].Prompt.ID, id)
		}
	}
	legacyResults := r.VisiblePrompts("legacy", "", "")
	if len(legacyResults) != 2 || legacyResults[0].Prompt.ID != "legacy-hidden" {
		t.Fatalf("legacy entry missing: %#v", legacyResults)
	}
}

func TestTaskFiltersMergeLegacyDuplicatesWithoutChangingPromptIDs(t *testing.T) {
	r := &Repository{Library: Library{Tasks: []Task{
		{ID: "writing", Title: "写或修改文字"},
		{ID: "writing-expression", Title: "写作与表达"},
		{ID: "learn", Title: "学懂一个知识"},
		{ID: "learning-understanding", Title: "学习与理解"},
		{ID: unclassifiedTaskID, Title: "未分类"},
	}, Prompts: []Prompt{
		{ID: "old-writing", Title: "旧写作", Body: "正文", PrimaryTaskID: "writing"},
		{ID: "new-writing", Title: "新写作", Body: "正文", PrimaryTaskID: "writing-expression"},
	}}}
	filters := r.TaskFilters()
	var writing *TaskFilter
	for i := range filters {
		if filters[i].ID == "writing-expression" {
			writing = &filters[i]
		}
		if filters[i].ID == "writing" {
			t.Fatal("legacy writing category remained as a duplicate filter")
		}
	}
	if writing == nil || writing.Title != "写作与修改" || len(writing.TaskIDs) != 2 {
		t.Fatalf("writing filter = %#v", writing)
	}
	results := r.VisiblePromptsForTasks(writing.TaskIDs, "")
	if len(results) != 2 || results[0].Prompt.PrimaryTaskID != "writing" || results[1].Prompt.PrimaryTaskID != "writing-expression" {
		t.Fatalf("merged filter changed or lost legacy prompt: %#v", results)
	}
}

func TestSearchUsesAndSemanticsAndPriority(t *testing.T) {
	r := &Repository{Library: seedLibrary()}
	results := r.VisiblePrompts("all", "", "漏洞 方案")
	if len(results) == 0 || results[0].Prompt.ID != "idea-risks" {
		t.Fatalf("unexpected search results: %#v", results)
	}
	if len(r.VisiblePrompts("all", "", "不存在的词")) != 0 {
		t.Fatal("unmatched search should be empty")
	}
}

func TestImportRejectsWholeInvalidFile(t *testing.T) {
	r := &Repository{Library: seedLibrary()}
	before := len(r.Library.Prompts)
	b, _ := json.Marshal(Library{SchemaVersion: librarySchemaVersion, Prompts: []Prompt{{ID: "x", Title: "", Body: "bad"}}})
	_, _, _, err := r.Import(b)
	if err == nil || len(r.Library.Prompts) != before {
		t.Fatal("invalid import must not mutate the library")
	}
}

func TestSeedBodiesKeepMultilineText(t *testing.T) {
	lib := seedLibrary()
	if !strings.Contains(lib.Prompts[0].Body, "\n") {
		t.Fatal("seed prompt should contain multiline body")
	}
}

func TestAlwaysOnTopSettingPersistsThroughRepositoryCommit(t *testing.T) {
	r := &Repository{LibraryPath: t.TempDir() + "/library.json", Library: seedLibrary()}
	if err := r.SetAlwaysOnTop(true); err != nil {
		t.Fatal(err)
	}
	if !r.Library.Settings.AlwaysOnTop {
		t.Fatal("always-on-top setting was not committed")
	}
}

func TestPopupEntryDefaultsAndPersistsThroughRepositoryCommit(t *testing.T) {
	r := &Repository{LibraryPath: t.TempDir() + "/library.json", Library: seedLibrary()}
	if r.PopupEntry() != popupEntryCategories {
		t.Fatal("missing popup entry should default to categories")
	}
	if err := r.SetPopupEntry(popupEntryFavorite); err != nil {
		t.Fatal(err)
	}
	if r.PopupEntry() != popupEntryFavorite || r.Library.Settings.PopupEntry != popupEntryFavorite {
		t.Fatal("favorite popup entry was not committed")
	}
	if err := r.SetPopupEntry("obsolete-entry"); err != nil {
		t.Fatal(err)
	}
	if r.PopupEntry() != popupEntryCategories {
		t.Fatal("invalid popup entry should recover to categories")
	}
}

func TestTaskBrowseHidesUntouchedLegacyButSearchKeepsItAvailable(t *testing.T) {
	legacy := Prompt{ID: "legacy", Title: "旧示例", Body: "正文", PrimaryTaskID: "writing", Origin: "bundled", Status: "draft", Source: Source{Kind: "original-draft"}}
	r := &Repository{Library: Library{Tasks: []Task{{ID: "writing", Title: "写或修改文字"}}, Prompts: []Prompt{
		legacy,
		{ID: "personal", Title: "个人条目", Body: "正文", PrimaryTaskID: "writing"},
	}}}
	if results := r.VisiblePromptsForTasks([]string{"writing"}, ""); len(results) != 1 || results[0].Prompt.ID != "personal" {
		t.Fatalf("category browse should keep untouched legacy examples collapsed: %#v", results)
	}
	if results := r.VisiblePrompts("all", "", "旧示例"); len(results) != 1 || results[0].Prompt.ID != "legacy" {
		t.Fatalf("full-library search should keep legacy example available: %#v", results)
	}
}
