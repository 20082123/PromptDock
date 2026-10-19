package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const librarySchemaVersion = 1

const (
	popupEntryFavorite   = "favorite"
	popupEntryRecent     = "recent"
	popupEntryCategories = "categories"
)

type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// TaskFilter is a display category. It can cover legacy task IDs without
// rewriting the category saved on any existing prompt.
type TaskFilter struct {
	ID      string
	Title   string
	TaskIDs []string
}

type Source struct {
	Kind       string `json:"kind"`
	Author     string `json:"author"`
	License    string `json:"license"`
	URL        string `json:"url"`
	Adaptation string `json:"adaptation"`
	CheckedAt  string `json:"checkedAt"`
	LicenseURL string `json:"licenseUrl,omitempty"`
	Notice     string `json:"notice,omitempty"`
}

type TestNote struct {
	Date     string `json:"date"`
	Model    string `json:"model"`
	Scenario string `json:"scenario"`
	Result   string `json:"result"`
}

type Prompt struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Body           string     `json:"body"`
	PrimaryTaskID  string     `json:"primaryTaskId"`
	RelatedTaskIDs []string   `json:"relatedTaskIds,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	Origin         string     `json:"origin"`
	Source         Source     `json:"source"`
	ContentVersion int        `json:"contentVersion"`
	Status         string     `json:"status"`
	TestNotes      []TestNote `json:"testNotes,omitempty"`
	Favorite       bool       `json:"favorite"`
	LastUsedAt     time.Time  `json:"lastUsedAt,omitempty"`
	UseCount       int        `json:"useCount"`
	LocalModified  bool       `json:"localModified"`
	Deleted        bool       `json:"deleted"`
}

// Settings deliberately live alongside the library so that lightweight UI
// preferences survive an application restart without introducing a second
// persistence file.
type Settings struct {
	AlwaysOnTop            bool   `json:"alwaysOnTop"`
	PopupEntry             string `json:"popupEntry,omitempty"`
	DisableAutoUpdateCheck bool   `json:"disableAutoUpdateCheck,omitempty"`
}

type Library struct {
	SchemaVersion int       `json:"schemaVersion"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Settings      Settings  `json:"settings"`
	Tasks         []Task    `json:"tasks"`
	Prompts       []Prompt  `json:"prompts"`
}

type SearchResult struct {
	Prompt Prompt
	Score  int
	Order  int
}

type Repository struct {
	LibraryPath string
	Library     Library
}

func defaultTasks() []Task {
	return []Task{
		{ID: "idea", Title: "讨论一个想法"},
		{ID: "writing", Title: "写或修改文字"},
		{ID: "learn", Title: "学懂一个知识"},
		{ID: "organize", Title: "整理一份资料"},
		{ID: "plan", Title: "制订一个计划"},
		{ID: "code", Title: "解决代码问题"},
	}
}

const unclassifiedTaskID = "unclassified"

var legacyTaskAliases = map[string]string{
	"idea":     "analysis-decisions",
	"plan":     "analysis-decisions",
	"writing":  "writing-expression",
	"learn":    "learning-understanding",
	"organize": "reading-organizing",
}

var canonicalTaskTitles = map[string]string{
	"ai-dialogue":             "与 AI 沟通",
	"writing-expression":      "写作与修改",
	"learning-understanding":  "学习与理解",
	"analysis-decisions":      "分析与决策",
	"reading-organizing":      "阅读与整理",
	"work-life-communication": "工作与生活沟通",
	"code":                    "编程与开发",
	"unclassified":            "未分类",
}

var canonicalTaskOrder = []string{
	"ai-dialogue", "writing-expression", "learning-understanding",
	"analysis-decisions", "reading-organizing", "work-life-communication",
	"code", unclassifiedTaskID,
}

func canonicalTaskID(id string) string {
	if replacement, ok := legacyTaskAliases[id]; ok {
		return replacement
	}
	return id
}

func ensureUnclassifiedTask(lib *Library) bool {
	if taskExists(lib.Tasks, unclassifiedTaskID) {
		return false
	}
	lib.Tasks = append(lib.Tasks, Task{ID: unclassifiedTaskID, Title: "未分类"})
	return true
}

type seedSpec struct {
	id, task, title, description, body string
	tags                               []string
}

func seedLibrary() Library {
	specs := []seedSpec{
		{"idea-clarify", "idea", "先帮我想清楚", "先发现遗漏和默认假设，再问一个关键问题", "请先不要直接回答。先指出我没有说清楚的目标、限制和默认假设，再告诉我最影响判断的一项缺失信息。最后只向我提出一个最关键、我可以立即回答的问题。\n\n我的问题是：[粘贴你的问题]", []string{"想法", "澄清", "提问"}},
		{"idea-risks", "idea", "帮我找漏洞", "检查方案中的假设、风险和失败可能", "请审查下面这个想法：列出关键假设、可能失败的环节、容易被忽略的反例，并按影响和发生可能性排序。最后给出三个可以低成本验证的动作。\n\n想法：\n[粘贴想法]", []string{"风险", "反例", "验证"}},
		{"idea-tradeoffs", "idea", "帮我比较取舍", "按目标、限制和代价比较多个选项", "请把下面几个选项放在同一套标准下比较：目标收益、成本、风险、可逆性和适用条件。先说明标准，再给出表格，最后给出在不同优先级下的选择建议。\n\n选项：\n[列出选项]", []string{"比较", "决策", "取舍"}},
		{"idea-testable", "idea", "把想法变成可验证的问题", "把模糊设想拆成假设和验证方式", "请把我的设想改写成一组可验证的假设。每条包含：假设内容、需要观察的信号、最小验证方法、可能的误判。不要直接假设资源和结论。\n\n设想：\n[粘贴设想]", []string{"假设", "实验", "验证"}},
		{"idea-angle", "idea", "从另一种角度审视", "用不同立场重新检查当前判断", "请分别从使用者、执行者、反对者和旁观者的角度审视下面的判断。指出每个角度会关注什么，以及这会如何改变我的下一步。\n\n当前判断：\n[粘贴内容]", []string{"视角", "反思"}},
		{"writing-clear", "writing", "把这段话写清楚", "保留原意，改善结构、逻辑和表达", "请在不改变事实和立场的前提下，把下面的文字改得更清楚。先列出原文中的歧义或跳跃，再给出修改稿；没有依据的地方不要擅自补充。\n\n原文：\n[粘贴文字]", []string{"改写", "清晰", "表达"}},
		{"writing-outline", "writing", "先搭一个大纲", "把目标、受众和材料组织成可执行结构", "请根据目标和已有材料搭一个分层大纲。每一节说明要回答的问题、需要的证据和与前后部分的关系。信息不足时用占位符标出。\n\n目标：\n[填写目标]\n材料：\n[粘贴材料]", []string{"大纲", "结构", "写作"}},
		{"writing-rephrase", "writing", "换一种说法", "保留含义，改变语气或表达方式", "请保留原文事实和核心含义，提供三种不同表达：简洁直接、自然口语、正式稳妥。说明每种表达适合的场景，不要添加原文没有的承诺。\n\n原文：\n[粘贴文字]", []string{"语气", "改写"}},
		{"writing-shorten", "writing", "缩短但保留重点", "压缩文字，不牺牲必要背景和结论", "请把下面的文字压缩到原长度的约一半。保留目标、关键事实、限制和行动请求；删除重复、铺垫和空泛形容。先给出压缩稿，再列出删掉的信息。\n\n原文：\n[粘贴文字]", []string{"摘要", "精简"}},
		{"writing-fact", "writing", "检查事实与表达是否混淆", "区分可验证事实、推断、意见和措辞", "请逐句检查下面的文字，并标注每句属于事实、推断、意见还是表达性措辞。指出哪些句子需要来源或改成更谨慎的说法，并给出修改建议。\n\n文字：\n[粘贴文字]", []string{"事实", "审校", "表达"}},
		{"learn-example", "learn", "用例子讲明白", "从直观例子和反例入手解释知识", "请用一个日常类比、一个具体例子和一个反例解释下面的概念。先说它解决什么问题，再说明类比在哪些地方会失效。最后用一句话总结。\n\n概念：\n[填写概念]", []string{"解释", "例子", "入门"}},
		{"learn-check", "learn", "检查我的理解", "找出理解中的正确部分和缺口", "下面是我对一个概念的理解。请先复述你认为我说对的部分，再指出不准确、遗漏或容易误解的地方。最后给我一个最小修正版，不要超出这个概念的范围。\n\n我的理解：\n[粘贴理解]", []string{"学习", "反馈", "理解"}},
		{"learn-start", "learn", "找到学习起点", "根据目标和已有基础安排最小学习路径", "请根据我的目标、已有基础和可投入时间，给出一个从今天开始的最小学习路径。每一步写明前置知识、练习方式和完成判据；不要堆砌资源名称。\n\n目标：\n[填写目标]\n基础：\n[填写基础]\n时间：\n[填写时间]", []string{"学习路径", "基础"}},
		{"learn-distinguish", "learn", "区分相似概念", "用边界、例外和反例辨析相近术语", "请比较下面几个容易混淆的概念：分别给出定义、边界、典型例子、反例和选择判断。最后给出一个能检验理解的小问题。\n\n概念：\n[列出概念]", []string{"概念", "辨析"}},
		{"learn-exercise", "learn", "出一道练习题", "针对刚学内容设计可反馈的练习", "请根据下面的学习内容出一道难度适中的练习题。不要马上给答案，先说明考察点和作答要求；等我回答后再按步骤批改。\n\n学习内容：\n[粘贴内容]", []string{"练习", "巩固"}},
		{"organize-keypoints", "organize", "提炼重点与依据", "把资料压缩为结论、证据和不确定性", "请从下面的资料中提炼：核心结论、支持结论的依据、关键数字或原句、存在的限制和无法确认的部分。不要把资料中的推测改写成事实。\n\n资料：\n[粘贴资料]", []string{"资料", "重点", "证据"}},
		{"organize-todos", "organize", "把记录变成待办", "从混乱记录中提取下一步行动", "请把下面的记录整理成待办清单。每项包含动作、负责人（未知就标未知）、截止时间（没有就留空）、依赖和完成判据。不要凭空补日期。\n\n记录：\n[粘贴记录]", []string{"待办", "整理", "行动"}},
		{"organize-compare", "organize", "比较几份资料", "对齐多份材料的共同点、差异和冲突", "请按同一组维度比较下面几份资料。先给出维度，再列共同点、差异、相互矛盾之处和可能的原因；标出每个判断来自哪份资料。\n\n资料 A：\n[粘贴]\n资料 B：\n[粘贴]", []string{"比较", "资料", "冲突"}},
		{"organize-fields", "organize", "提取结构化信息", "从文本中提取固定字段并保留缺失信息", "请把下面的文本提取成 JSON。只使用文本中明确出现的信息，缺失字段填 null；无法确定的对应关系放进 notes，不要猜测。字段：\n[列出字段]\n\n文本：\n[粘贴文本]", []string{"结构化", "JSON", "提取"}},
		{"organize-questions", "organize", "找出尚未回答的问题", "盘点资料留下的空白和待确认事项", "请阅读下面的资料，列出仍未回答但会影响决策的问题。按影响程度排序，每个问题说明为什么重要、需要谁确认以及可以用什么材料回答。\n\n资料：\n[粘贴资料]", []string{"问题", "空白", "审查"}},
		{"plan-next", "plan", "把目标拆成下一步", "把大目标变成可执行的近期动作", "请把下面的目标拆成接下来一到两周可以执行的步骤。每步包含结果、前置条件、预计耗时和完成判据；优先安排能减少不确定性的动作。\n\n目标：\n[填写目标]", []string{"计划", "拆解", "行动"}},
		{"plan-priority", "plan", "决定先做哪件事", "在资源有限时安排任务优先级", "请根据目标、截止时间、依赖、风险和预计收益，帮我给下面的任务排序。说明排序依据，指出如果某个假设变化，顺序会如何调整。\n\n任务：\n[列出任务]\n限制：\n[填写限制]", []string{"优先级", "安排"}},
		{"plan-realistic", "plan", "检查计划是否现实", "发现计划中的估时、依赖和资源问题", "请审查这份计划的可行性。重点检查工作量、依赖、并行假设、缓冲时间、技能和外部不确定性。先列风险，再给出保守版调整，不要只说可行或不可行。\n\n计划：\n[粘贴计划]", []string{"可行性", "估时", "风险"}},
		{"plan-failure", "plan", "为失败做准备", "为关键计划设计预案和触发条件", "请为下面的计划设计预案。列出可能失败的原因、尽早可观察的信号、触发条件、应急动作和需要提前准备的资源。把低概率高损失情况单独标出。\n\n计划：\n[粘贴计划]", []string{"预案", "风险", "应急"}},
		{"plan-retro", "plan", "做一次复盘", "从结果中提炼可复用的改进动作", "请按目标、实际结果、偏差、原因、保留项和改进项复盘下面的经历。把可控因素与不可控因素分开，最后只给出三项下一次可以直接尝试的改变。\n\n经历：\n[粘贴记录]", []string{"复盘", "改进"}},
		{"code-debug", "code", "一步步定位报错", "从现象到最小复现逐层缩小问题", "请帮助我定位这个报错。先复述现象和环境，再列出按优先级排列的假设；每一步都给出要执行的检查、预期结果和分支。不要在缺少日志时直接编造根因。\n\n报错：\n[粘贴报错]\n相关代码：\n[粘贴代码]", []string{"调试", "报错", "日志"}},
		{"code-understand", "code", "看懂代码", "按职责、数据流和边界解释代码", "请带我读懂下面的代码。先说明整体职责，再按执行顺序解释关键数据、分支和副作用；指出我需要特别注意的边界条件。不要只逐行翻译语法。\n\n代码：\n[粘贴代码]", []string{"阅读代码", "解释"}},
		{"code-review", "code", "检查实现中的问题", "从正确性、可维护性和风险审查代码", "请审查下面的实现，优先找会导致错误结果、数据丢失、安全问题或难以维护的问题。每条指出位置、影响、原因和最小修复建议；没有问题的地方不必凑数。\n\n代码：\n[粘贴代码]", []string{"代码审查", "质量", "风险"}},
		{"code-requirements", "code", "先补齐需求", "把模糊需求整理成行为和验收条件", "请把下面的开发想法整理成明确需求：用户目标、输入输出、状态变化、异常情况、非目标和验收标准。把需要我确认的问题集中列出，不要直接假设技术方案。\n\n想法：\n[粘贴想法]", []string{"需求", "验收", "分析"}},
		{"code-tests", "code", "为关键行为设计验证步骤", "覆盖正常路径、边界和失败路径", "请为下面的功能设计测试矩阵。至少覆盖正常、空输入、边界值、重复操作、失败恢复和并发/顺序问题。每条包含前置条件、步骤、预期结果，不要把实现细节当成测试目标。\n\n功能：\n[描述功能]", []string{"测试", "验证", "边界"}},
	}

	prompts := make([]Prompt, 0, len(specs))
	for _, s := range specs {
		prompts = append(prompts, Prompt{
			ID: s.id, Title: s.title, Description: s.description, Body: s.body,
			PrimaryTaskID: s.task, Tags: s.tags, Origin: "bundled", ContentVersion: 1,
			Status: "draft", Source: Source{Kind: "original-draft", Author: "PromptDock", License: "project-draft", Adaptation: "自行设计的首版示例，发布前仍需内容测试与许可确认"},
		})
	}
	return Library{SchemaVersion: librarySchemaVersion, UpdatedAt: time.Now(), Tasks: defaultTasks(), Prompts: prompts}
}

func clonePrompt(p Prompt) Prompt {
	p.RelatedTaskIDs = append([]string(nil), p.RelatedTaskIDs...)
	p.Tags = append([]string(nil), p.Tags...)
	p.TestNotes = append([]TestNote(nil), p.TestNotes...)
	return p
}

func cloneLibrary(l Library) Library {
	copyLibrary := l
	copyLibrary.Tasks = append([]Task(nil), l.Tasks...)
	copyLibrary.Prompts = make([]Prompt, len(l.Prompts))
	for i, prompt := range l.Prompts {
		copyLibrary.Prompts[i] = clonePrompt(prompt)
	}
	return copyLibrary
}

func taskExists(tasks []Task, id string) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

func dataDirectory() (string, error) {
	base := os.Getenv("PROMPTDOCK_DATA_DIR")
	if base == "" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	if base == "" {
		return "", errors.New("无法确定本地数据目录")
	}
	return filepath.Abs(filepath.Join(base, "PromptDock"))
}

func NewRepository() (*Repository, error) {
	dir, err := dataDirectory()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	r := &Repository{LibraryPath: filepath.Join(dir, "library.json")}
	data, err := os.ReadFile(r.LibraryPath)
	if os.IsNotExist(err) {
		r.Library, err = bundledLibrary()
		if err != nil {
			return nil, err
		}
		ensureUnclassifiedTask(&r.Library)
		if err = r.Save(); err != nil {
			return nil, err
		}
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取本地库失败: %w", err)
	}
	if err := json.Unmarshal(data, &r.Library); err != nil || r.Library.SchemaVersion != librarySchemaVersion {
		return nil, fmt.Errorf("本地库格式损坏或不兼容（当前支持格式 %d）。原文件已保留，不会用空库或默认库覆盖：%s。请使用兼容版本或退出后从备份恢复", librarySchemaVersion, r.LibraryPath)
	}
	tasksChanged := false
	if len(r.Library.Tasks) == 0 {
		r.Library.Tasks = defaultTasks()
		tasksChanged = true
	}
	if ensureUnclassifiedTask(&r.Library) {
		tasksChanged = true
	}
	if tasksChanged {
		if err := r.Save(); err != nil {
			return nil, fmt.Errorf("补充默认分类失败: %w", err)
		}
	}
	// Add only missing bundled IDs. Existing edits and deletion markers win;
	// changed upstream versions are offered only through explicit import.
	_, err = r.importContent(bundledContent, true)
	return r, err
}

func (r *Repository) Save() error {
	return r.commit(cloneLibrary(r.Library))
}

func (r *Repository) commit(candidate Library) error {
	candidate.SchemaVersion = librarySchemaVersion
	candidate.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.LibraryPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := replaceFile(tmp, r.LibraryPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	r.Library = candidate
	return nil
}

func (r *Repository) PromptByID(id string) *Prompt {
	for i := range r.Library.Prompts {
		if r.Library.Prompts[i].ID == id && !r.Library.Prompts[i].Deleted {
			copyPrompt := clonePrompt(r.Library.Prompts[i])
			return &copyPrompt
		}
	}
	return nil
}

func (r *Repository) TaskTitle(id string) string {
	if title, ok := canonicalTaskTitles[canonicalTaskID(id)]; ok {
		return title
	}
	for _, task := range r.Library.Tasks {
		if task.ID == id {
			return task.Title
		}
	}
	return "未分类"
}

// TaskFilters merges known legacy category names only for navigation. Prompt
// records retain their original PrimaryTaskID until a user explicitly changes
// it in the manager.
func (r *Repository) TaskFilters() []TaskFilter {
	groups := make(map[string]*TaskFilter)
	encountered := make([]string, 0, len(r.Library.Tasks))
	for _, task := range r.Library.Tasks {
		id := canonicalTaskID(task.ID)
		group := groups[id]
		if group == nil {
			title := task.Title
			if known, ok := canonicalTaskTitles[id]; ok {
				title = known
			}
			group = &TaskFilter{ID: id, Title: title}
			groups[id] = group
			encountered = append(encountered, id)
		}
		group.TaskIDs = append(group.TaskIDs, task.ID)
	}
	filters := make([]TaskFilter, 0, len(groups))
	seen := make(map[string]bool, len(groups))
	for _, id := range canonicalTaskOrder {
		if group := groups[id]; group != nil {
			filters = append(filters, *group)
			seen[id] = true
		}
	}
	for _, id := range encountered {
		if !seen[id] {
			filters = append(filters, *groups[id])
		}
	}
	return filters
}

func (r *Repository) VisiblePrompts(mode, taskID, query string) []SearchResult {
	taskIDs := []string(nil)
	if taskID != "" {
		taskIDs = []string{taskID}
	}
	return r.visiblePrompts(mode, taskIDs, query)
}

func (r *Repository) VisiblePromptsForTasks(taskIDs []string, query string) []SearchResult {
	return r.visiblePrompts("task", taskIDs, query)
}

func (r *Repository) visiblePrompts(mode string, taskIDs []string, query string) []SearchResult {
	query = strings.TrimSpace(query)
	var out []SearchResult
	for i, p := range r.Library.Prompts {
		if p.Deleted || p.Body == "" {
			continue
		}
		if mode == "task" && !matchesAnyTask(p, taskIDs) {
			continue
		}
		if (mode == "default" || mode == "task") && isHiddenLegacyExample(p) {
			continue
		}
		if mode == "legacy" && !isLegacyDevelopmentExample(p) {
			continue
		}
		if mode == "favorite" && !p.Favorite {
			continue
		}
		if mode == "recent" && p.LastUsedAt.IsZero() {
			continue
		}
		if query != "" {
			if score := promptScore(p, query); score > 0 {
				out = append(out, SearchResult{Prompt: p, Score: score, Order: i})
			}
		} else {
			out = append(out, SearchResult{Prompt: p, Order: i})
		}
	}
	if mode == "default" && query == "" {
		sort.SliceStable(out, func(i, j int) bool {
			left, right := out[i].Prompt, out[j].Prompt
			if left.Favorite != right.Favorite {
				return left.Favorite
			}
			leftUsed, rightUsed := !left.LastUsedAt.IsZero(), !right.LastUsedAt.IsZero()
			if leftUsed != rightUsed {
				return leftUsed
			}
			if leftUsed && !left.LastUsedAt.Equal(right.LastUsedAt) {
				return left.LastUsedAt.After(right.LastUsedAt)
			}
			return false
		})
	} else if mode == "recent" {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Prompt.LastUsedAt.After(out[j].Prompt.LastUsedAt) })
	} else if query != "" {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	}
	return out
}

func matchesAnyTask(p Prompt, taskIDs []string) bool {
	for _, id := range taskIDs {
		if p.PrimaryTaskID == id || containsTaskID(p.RelatedTaskIDs, id) {
			return true
		}
	}
	return false
}

func isLegacyDevelopmentExample(p Prompt) bool {
	return p.Origin == "bundled" && p.Status == "draft" && p.Source.Kind == "original-draft"
}

func isHiddenLegacyExample(p Prompt) bool {
	return isLegacyDevelopmentExample(p) && !p.LocalModified && !p.Favorite && p.LastUsedAt.IsZero()
}

func containsTaskID(taskIDs []string, id string) bool {
	for _, taskID := range taskIDs {
		if taskID == id {
			return true
		}
	}
	return false
}

func promptScore(p Prompt, query string) int {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return 1
	}
	title := strings.ToLower(p.Title)
	desc := strings.ToLower(p.Description)
	tags := strings.ToLower(strings.Join(p.Tags, " "))
	body := strings.ToLower(p.Body)
	score := 0
	for _, word := range words {
		switch {
		case strings.Contains(title, word):
			score += 40
		case strings.Contains(desc, word):
			score += 25
		case strings.Contains(tags, word):
			score += 18
		case strings.Contains(body, word):
			score += 8
		default:
			return 0
		}
	}
	return score
}

func (r *Repository) RecordUse(id string) error {
	candidate := cloneLibrary(r.Library)
	for i := range candidate.Prompts {
		if candidate.Prompts[i].ID != id || candidate.Prompts[i].Deleted {
			continue
		}
		candidate.Prompts[i].LastUsedAt = time.Now()
		candidate.Prompts[i].UseCount++
		return r.commit(candidate)
	}
	return errors.New("提示词不存在")
}

func (r *Repository) SetFavorite(id string, favorite bool) error {
	candidate := cloneLibrary(r.Library)
	for i := range candidate.Prompts {
		if candidate.Prompts[i].ID != id || candidate.Prompts[i].Deleted {
			continue
		}
		candidate.Prompts[i].Favorite = favorite
		return r.commit(candidate)
	}
	return errors.New("提示词不存在")
}

func (r *Repository) ToggleFavorite(id string) error {
	p := r.PromptByID(id)
	if p == nil {
		return errors.New("提示词不存在")
	}
	return r.SetFavorite(id, !p.Favorite)
}

func (r *Repository) SetAlwaysOnTop(alwaysOnTop bool) error {
	candidate := cloneLibrary(r.Library)
	candidate.Settings.AlwaysOnTop = alwaysOnTop
	return r.commit(candidate)
}

func (r *Repository) SetAutoUpdateCheck(enabled bool) error {
	candidate := cloneLibrary(r.Library)
	candidate.Settings.DisableAutoUpdateCheck = !enabled
	return r.commit(candidate)
}

func normalizePopupEntry(entry string) string {
	switch entry {
	case popupEntryFavorite, popupEntryRecent, popupEntryCategories:
		return entry
	default:
		return popupEntryCategories
	}
}

func (r *Repository) PopupEntry() string {
	return normalizePopupEntry(r.Library.Settings.PopupEntry)
}

func (r *Repository) SetPopupEntry(entry string) error {
	entry = normalizePopupEntry(entry)
	if r.PopupEntry() == entry && r.Library.Settings.PopupEntry == entry {
		return nil
	}
	candidate := cloneLibrary(r.Library)
	candidate.Settings.PopupEntry = entry
	return r.commit(candidate)
}

func (r *Repository) UpsertPrompt(p Prompt) error {
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" {
		return errors.New("名称和正文不能为空")
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("personal-%d", time.Now().UnixNano())
	}
	if p.ContentVersion == 0 {
		p.ContentVersion = 1
	}
	if p.Origin == "" {
		p.Origin = "personal"
	}
	if p.Status == "" {
		p.Status = "personal"
	}
	if err := validatePromptText(p); err != nil {
		return err
	}
	if !taskExists(r.Library.Tasks, p.PrimaryTaskID) {
		return errors.New("主任务不存在")
	}
	for _, relatedTaskID := range p.RelatedTaskIDs {
		if !taskExists(r.Library.Tasks, relatedTaskID) {
			return fmt.Errorf("关联任务不存在: %s", relatedTaskID)
		}
	}
	candidate := cloneLibrary(r.Library)
	p = clonePrompt(p)
	for i := range candidate.Prompts {
		if candidate.Prompts[i].ID == p.ID {
			if candidate.Prompts[i].LocalModified || !samePromptContent(candidate.Prompts[i], p) {
				p.LocalModified = true
			}
			candidate.Prompts[i] = p
			return r.commit(candidate)
		}
	}
	candidate.Prompts = append(candidate.Prompts, p)
	return r.commit(candidate)
}

func (r *Repository) DeletePrompt(id string) error {
	candidate := cloneLibrary(r.Library)
	for i := range candidate.Prompts {
		p := candidate.Prompts[i]
		if p.ID != id || p.Deleted {
			continue
		}
		// Tombstones also keep imported/personal items from reappearing on reimport.
		candidate.Prompts[i].Deleted = true
		candidate.Prompts[i].LocalModified = true
		return r.commit(candidate)
	}
	return errors.New("提示词不存在")
}

func (r *Repository) Export(path string, includeUsage bool) error {
	copyLib := r.Library
	if !includeUsage {
		copyLib = Library{SchemaVersion: r.Library.SchemaVersion, UpdatedAt: time.Now(), Tasks: r.Library.Tasks, Prompts: make([]Prompt, len(r.Library.Prompts))}
		copy(copyLib.Prompts, r.Library.Prompts)
		for i := range copyLib.Prompts {
			copyLib.Prompts[i].Favorite = false
			copyLib.Prompts[i].LastUsedAt = time.Time{}
			copyLib.Prompts[i].UseCount = 0
		}
	}
	b, err := json.MarshalIndent(copyLib, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (r *Repository) Import(data []byte) (added, skipped, conflicts int, err error) {
	report, err := r.importContent(data, false)
	if err != nil {
		return 0, 0, 0, err
	}
	return report.Added, report.Skipped, len(report.Conflicts), nil
}
