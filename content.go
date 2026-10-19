package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed content/collection-01.json
var collection01Content []byte

//go:embed content/collection-02-public.json
var collection02PublicContent []byte

// bundledContent stays as one importable library because the existing update
// command and startup migration deliberately share the same non-overwriting
// import path. Collection files remain separate so their attribution and
// release decisions stay reviewable.
var bundledContent []byte

func init() {
	library, err := bundledLibrary()
	if err != nil {
		panic(err)
	}
	bundledContent, err = json.Marshal(library)
	if err != nil {
		panic(fmt.Errorf("编码内置内容包失败: %w", err))
	}
}

func bundledLibrary() (Library, error) {
	var combined Library
	seenTasks := make(map[string]bool)
	seenPrompts := make(map[string]bool)
	for _, data := range [][]byte{collection01Content, collection02PublicContent} {
		var collection Library
		if err := json.Unmarshal(data, &collection); err != nil {
			return Library{}, fmt.Errorf("读取内置内容包失败: %w", err)
		}
		if collection.SchemaVersion != librarySchemaVersion {
			return Library{}, fmt.Errorf("内置内容包版本不受支持: %d", collection.SchemaVersion)
		}
		combined.SchemaVersion = collection.SchemaVersion
		if collection.UpdatedAt.After(combined.UpdatedAt) {
			combined.UpdatedAt = collection.UpdatedAt
		}
		for _, task := range collection.Tasks {
			if seenTasks[task.ID] {
				continue
			}
			seenTasks[task.ID] = true
			combined.Tasks = append(combined.Tasks, task)
		}
		for _, prompt := range collection.Prompts {
			if seenPrompts[prompt.ID] {
				return Library{}, fmt.Errorf("内置内容包包含重复 ID: %s", prompt.ID)
			}
			seenPrompts[prompt.ID] = true
			combined.Prompts = append(combined.Prompts, prompt)
		}
	}
	return combined, nil
}

// Tags can contain spaces (for example "AI 沟通"). Only explicit separators
// split a tag; Chinese and English punctuation are both accepted.
func parseTags(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune(",，、;；\n\r", r)
	})
	var tags []string
	seen := make(map[string]bool)
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		key := strings.ToLower(tag)
		if tag != "" && !seen[key] {
			seen[key] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

func (r *Repository) promptInfo(p Prompt) string {
	version := p.Source.Kind
	switch version {
	case "adaptation-zh":
		version = "中文整理版（非逐字翻译）"
	case "translation-zh":
		version = "中文翻译版"
	case "original":
		version = "作者原文"
	case "original-draft":
		version = "早期开发示例"
	case "":
		version = "个人添加 / 未标注原文版本"
	}
	lines := []string{
		"用途：" + p.Description,
		"分类：" + r.TaskTitle(p.PrimaryTaskID),
		"场景标签：" + strings.Join(p.Tags, "、"),
		"版本说明：" + version,
	}
	if p.LocalModified {
		lines = append(lines, "个人修改：已保存；保留以下原始来源信息")
	}
	if len(p.RelatedTaskIDs) > 0 {
		var titles []string
		for _, id := range p.RelatedTaskIDs {
			titles = append(titles, r.TaskTitle(id))
		}
		lines = append(lines, "关联分类："+strings.Join(titles, "、"))
	}
	for _, field := range [][2]string{
		{"作者 / 来源项目", p.Source.Author}, {"原始链接", p.Source.URL},
		{"许可", p.Source.License}, {"许可链接", p.Source.LicenseURL},
		{"整理 / 改编说明", p.Source.Adaptation}, {"来源读取日期", p.Source.CheckedAt},
		{"完整许可声明", p.Source.Notice},
	} {
		if field[1] != "" {
			lines = append(lines, field[0]+"："+field[1])
		}
	}
	return strings.Join(lines, "\n\n")
}
