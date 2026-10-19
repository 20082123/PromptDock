//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestReviewInputStructureMatchesWindowsABI(t *testing.T) {
	want := uintptr(28)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 40
	}
	if got := unsafe.Sizeof(Input{}); got != want {
		t.Fatalf("INPUT size: got %d bytes, Windows ABI requires %d", got, want)
	}
	inputs := []Input{newKeyboardInput(VK_CONTROL, 0), newKeyboardInput(VK_V, 0)}
	if stride := uintptr(unsafe.Pointer(&inputs[1])) - uintptr(unsafe.Pointer(&inputs[0])); stride != want {
		t.Fatalf("INPUT array stride: got %d bytes, Windows ABI requires %d", stride, want)
	}
}

func TestGuiThreadInfoMatchesWindowsABI(t *testing.T) {
	want := uintptr(48)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 72
	}
	if got := unsafe.Sizeof(GuiThreadInfo{}); got != want {
		t.Fatalf("GUITHREADINFO size: got %d bytes, Windows ABI requires %d", got, want)
	}
}

func TestReviewInvalidLaterImportDoesNotMutate(t *testing.T) {
	r := &Repository{LibraryPath: filepath.Join(t.TempDir(), "library.json"), Library: seedLibrary()}
	before := len(r.Library.Prompts)
	data, _ := json.Marshal(Library{SchemaVersion: librarySchemaVersion, Prompts: []Prompt{
		{ID: "review-valid", Title: "Valid", Body: "Test only", PrimaryTaskID: "idea"},
		{ID: "review-invalid", Title: "", Body: "Invalid"},
	}})
	_, _, _, err := r.Import(data)
	if err == nil {
		t.Fatal("expected import failure")
	}
	if len(r.Library.Prompts) != before {
		t.Errorf("rejected file changed in-memory prompt count from %d to %d", before, len(r.Library.Prompts))
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(r.LibraryPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk Library
	if err := json.Unmarshal(saved, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Prompts) != before {
		t.Errorf("a later save persisted %d prompts from a rejected import", len(disk.Prompts)-before)
	}
}

func TestReviewFailedSaveDoesNotMutate(t *testing.T) {
	r := &Repository{LibraryPath: filepath.Join(t.TempDir(), "absent", "library.json"), Library: seedLibrary()}
	before := len(r.Library.Prompts)
	err := r.UpsertPrompt(Prompt{ID: "review-write-failure", Title: "Title", Body: "Test only", PrimaryTaskID: "idea"})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if len(r.Library.Prompts) != before {
		t.Fatalf("failed save still changed prompt count from %d to %d", before, len(r.Library.Prompts))
	}
}

func TestReviewRelatedTaskAppears(t *testing.T) {
	p := Prompt{ID: "review-related", Title: "Test", Body: "Test only", PrimaryTaskID: "idea", RelatedTaskIDs: []string{"plan"}}
	r := &Repository{Library: Library{Tasks: defaultTasks(), Prompts: []Prompt{p}}}
	if len(r.VisiblePrompts("task", "plan", "")) != 1 {
		t.Fatal("related-task prompt is absent from the task view")
	}
}

func TestReviewPromptLookupCannotMutateRepository(t *testing.T) {
	r := &Repository{Library: seedLibrary()}
	p := r.PromptByID("idea-clarify")
	p.Body = "changed only in copy"
	if actual := r.PromptByID("idea-clarify"); actual == nil || actual.Body == p.Body {
		t.Fatal("callers must not be able to mutate active library data through PromptByID")
	}
}

func TestReviewTrayNotificationUsesVersion4EventField(t *testing.T) {
	lParam := uintptr(uint32(WM_RBUTTONUP) | uint32(1)<<16)
	if got := trayNotificationEvent(lParam); got != WM_RBUTTONUP {
		t.Fatalf("tray event: got %#x, want %#x", got, WM_RBUTTONUP)
	}
}
