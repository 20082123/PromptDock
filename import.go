package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ImportReport struct {
	Added         int
	Skipped       int
	TasksAdded    int
	Conflicts     []string
	TaskConflicts []string
}

// Local usage and personal-edit flags never decide whether two distributed
// versions are equal. All content, provenance and version fields do.
func sharedPrompt(p Prompt) Prompt {
	p = clonePrompt(p)
	p.ID = ""
	p.Favorite, p.LocalModified, p.Deleted = false, false, false
	p.LastUsedAt, p.UseCount = time.Time{}, 0
	sort.Strings(p.Tags)
	sort.Strings(p.RelatedTaskIDs)
	return p
}

func samePromptContent(a, b Prompt) bool {
	return reflect.DeepEqual(sharedPrompt(a), sharedPrompt(b))
}

func contentID(base string, p Prompt) string {
	data, _ := json.Marshal(sharedPrompt(p))
	return fmt.Sprintf("%s-import-%x", base, sha256.Sum256(data))
}

func validatePromptText(p Prompt) error {
	fields := []string{p.ID, p.Title, p.Description, p.Body, p.PrimaryTaskID,
		p.Source.Kind, p.Source.Author, p.Source.License, p.Source.URL, p.Source.LicenseURL, p.Source.Notice, p.Source.Adaptation, p.Source.CheckedAt}
	fields = append(fields, p.Tags...)
	fields = append(fields, p.RelatedTaskIDs...)
	for _, field := range fields {
		if strings.ContainsRune(field, '\x00') {
			return fmt.Errorf("提示词 %q 含有无法显示的空字符", p.Title)
		}
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" {
		return errors.New("名称和正文不能为空")
	}
	return nil
}

func (r *Repository) PreviewImport(data []byte) (ImportReport, error) {
	_, report, err := r.planImport(data, false)
	return report, err
}

func (r *Repository) importContent(data []byte, onlyMissing bool) (ImportReport, error) {
	candidate, report, err := r.planImport(data, onlyMissing)
	if err != nil {
		return ImportReport{}, err
	}
	if report.Added > 0 || report.TasksAdded > 0 {
		if err := r.commit(candidate); err != nil {
			return ImportReport{}, err
		}
	}
	return report, nil
}

// Preview and commit share this pure planner. Existing IDs are never replaced:
// differing versions get a stable copy ID, so repeated imports remain idempotent.
func (r *Repository) planImport(data []byte, onlyMissing bool) (Library, ImportReport, error) {
	var incoming Library
	var report ImportReport
	if err := json.Unmarshal(data, &incoming); err != nil {
		return Library{}, report, err
	}
	if incoming.SchemaVersion != librarySchemaVersion || len(incoming.Prompts) == 0 {
		return Library{}, report, errors.New("文件不是受支持的 PromptDock JSON，或不包含提示词")
	}
	candidate := cloneLibrary(r.Library)
	tasks := make(map[string]Task)
	for _, task := range candidate.Tasks {
		tasks[task.ID] = task
	}
	seenTasks := make(map[string]bool)
	for _, task := range incoming.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Title) == "" ||
			strings.ContainsRune(task.ID+task.Title, '\x00') || seenTasks[task.ID] {
			return Library{}, ImportReport{}, errors.New("导入文件包含无效或重复的任务分类")
		}
		seenTasks[task.ID] = true
		if old, ok := tasks[task.ID]; ok {
			if old.Title != task.Title {
				report.TaskConflicts = append(report.TaskConflicts, old.Title+" ← "+task.Title)
			}
		} else {
			candidate.Tasks = append(candidate.Tasks, task)
			tasks[task.ID] = task
			report.TasksAdded++
		}
	}
	if len(candidate.Tasks) == 0 {
		return Library{}, ImportReport{}, errors.New("导入文件没有可用的任务分类")
	}
	existing := make(map[string]Prompt)
	for _, p := range candidate.Prompts {
		existing[p.ID] = p
	}
	seenIDs := make(map[string]bool)
	for _, p := range incoming.Prompts {
		if err := validatePromptText(p); err != nil {
			return Library{}, ImportReport{}, err
		}
		if p.ID != "" && seenIDs[p.ID] {
			return Library{}, ImportReport{}, fmt.Errorf("导入文件包含重复 ID: %s", p.ID)
		}
		seenIDs[p.ID] = true
		if p.PrimaryTaskID == "" {
			p.PrimaryTaskID = candidate.Tasks[0].ID
		}
		for _, id := range append([]string{p.PrimaryTaskID}, p.RelatedTaskIDs...) {
			if _, ok := tasks[id]; !ok {
				return Library{}, ImportReport{}, fmt.Errorf("导入提示词引用了未知分类: %s", id)
			}
		}
		if p.ID == "" {
			p.ID = contentID("personal", p)
		}
		if old, ok := existing[p.ID]; ok {
			if onlyMissing || old.Deleted || p.Deleted || samePromptContent(old, p) {
				report.Skipped++
				continue
			}
			copyID := contentID(p.ID, p)
			if _, exists := existing[copyID]; exists {
				// A previously imported copy may itself have personal edits or a
				// tombstone. Keep it rather than resurrecting another duplicate.
				report.Skipped++
				continue
			}
			report.Conflicts = append(report.Conflicts, p.Title)
			p.ID = copyID
		}
		candidate.Prompts = append(candidate.Prompts, clonePrompt(p))
		existing[p.ID] = p
		report.Added++
	}
	return candidate, report, nil
}
