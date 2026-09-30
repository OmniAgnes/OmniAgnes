package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"omni/internal/docparse"
)

func builtinTools() []*Tool {
	return []*Tool{
		{
			Name:        "read",
			Description: "读取文件内容。可指定 offset(起始行，0 基) 与 limit(读取行数)。路径限制在工作目录内。.docx/.pdf/.pptx/.xlsx 文档会自动提取文本（pdf 可传 page 页码，xlsx 可传 sheet 工作表名）。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "文件路径"},
					"offset": map[string]any{"type": "integer", "description": "起始行号（从 0 开始）"},
					"limit":  map[string]any{"type": "integer", "description": "读取的最大行数"},
					"page":   map[string]any{"type": "integer", "description": "PDF：只读这一页（从 1 起）"},
					"sheet":  map[string]any{"type": "string", "description": "xlsx：工作表名（省略时列出全部工作表）"},
				},
			},
			ReadOnly: true,
			Execute:  execRead,
		},
		{
			Name:        "write",
			Description: "将 content 完整写入 path（覆盖已有内容）。新建中间目录。仅供 build 模式。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path", "content"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "目标文件路径"},
					"content": map[string]any{"type": "string", "description": "写入内容"},
				},
			},
			ReadOnly: false,
			Execute:  execWrite,
		},
		{
			Name:        "edit",
			Description: "在文件中用 new_string 精确替换 old_string。old_string 必须唯一匹配，否则报错。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path", "old_string", "new_string"},
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "文件路径"},
					"old_string":  map[string]any{"type": "string", "description": "需被替换的原始文本"},
					"new_string":  map[string]any{"type": "string", "description": "替换后的文本"},
					"replace_all": map[string]any{"type": "boolean", "description": "是否替换所有匹配"},
				},
			},
			ReadOnly: false,
			Execute:  execEdit,
		},
		{
			Name:        "bash",
			Description: "在工作目录执行 shell 命令，返回 stdout+stderr。默认超时 10 秒，输出会被截断。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"command"},
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "要执行的 shell 命令"},
					"timeout": map[string]any{"type": "integer", "description": "超时秒数（可覆盖默认）"},
				},
			},
			ReadOnly: false,
			Execute:  execBash,
		},
		{
			Name:        "grep",
			Description: "在目录中按正则表达式搜索文本，输出 path:line: content。可指定 include 模式过滤文件（如 *.go）。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"pattern"},
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "正则表达式"},
					"path":    map[string]any{"type": "string", "description": "搜索目录（默认工作目录）"},
					"include": map[string]any{"type": "string", "description": "文件通配模式，逗号分隔，如 *.go,*.md"},
				},
			},
			ReadOnly: true,
			Execute:  execGrep,
		},
		{
			Name:        "glob",
			Description: "根据通配模式列出匹配的文件路径，支持 ** 递归。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"pattern"},
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "通配模式，如 **/*.go"},
				},
			},
			ReadOnly: true,
			Execute:  execGlob,
		},
		{
			Name: "json",
			Description: "解析与查询 JSON。用于处理 curl/API 返回的 JSON——不必再写 Python 脚本。" +
				"op=query 时按 path 提取子树（点号路径，如 data.items 或 data.items.0.id，支持 [i] 下标）；" +
				"op=keys 列出对象的键；op=shape 返回类型结构概览；op=pretty 格式化输出。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"op"},
				"properties": map[string]any{
					"op": map[string]any{
						"type":        "string",
						"enum":        []string{"query", "keys", "shape", "pretty"},
						"description": "query=按 path 提取；keys=列键；shape=结构概览；pretty=格式化",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "JSON 文件路径（与 content 二选一）",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "直接传入 JSON 文本（与 path 二选一，适合处理命令输出）",
					},
					"pointer": map[string]any{
						"type":        "string",
						"description": "query 用的点号路径，如 data.items 或 items[0].id；为空则返回根",
					},
				},
			},
			ReadOnly: true,
			Execute:  execJSON,
		},
		{
			Name: "todo",
			Description: "维护一份多步骤任务清单，让用户实时了解你处理到哪一步。" +
				"接到较复杂任务时先建立清单；每开始或完成一步都再次调用它提交**整份**清单（全量覆盖，非增量）。" +
				"同一时刻最多一项 in_progress，其余 pending 或 completed。简单一步到位的任务无需使用。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"items"},
				"properties": map[string]any{
					"items": map[string]any{
						"type":        "array",
						"description": "任务项数组（整份清单，全量覆盖）",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"content", "status"},
							"properties": map[string]any{
								"content":     map[string]any{"type": "string", "description": "任务描述（祈使句，如“修复登录接口”）"},
								"active_form": map[string]any{"type": "string", "description": "进行中的描述（如“正在修复登录接口”）"},
								"status": map[string]any{
									"type":        "string",
									"enum":        []string{"pending", "in_progress", "completed"},
									"description": "pending=待办，in_progress=进行中，completed=已完成",
								},
							},
						},
					},
				},
			},
			ReadOnly: true,
			Execute:  execTodo,
		},
		{
			Name: "ask",
			Description: "向用户提一个澄清问题并等待其回答。仅在歧义会实质影响结果、无法合理假设时使用；" +
				"一次最多问一个问题，可提供 2-4 个候选项。拿到回答后再继续。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"question"},
				"properties": map[string]any{
					"question": map[string]any{"type": "string", "description": "要问用户的问题（简洁明了）"},
					"options": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "候选项（可选，2-4 个）；用户也可自由输入",
					},
				},
			},
			ReadOnly: true,
			// 由 Agent 在执行循环中拦截并向 UI 要答案；此兜底仅在界面不支持提问时返回提示。
			Execute: func(_ context.Context, _ map[string]any, _ *Env) (string, error) {
				return "（当前界面不支持向用户提问，请按最合理的假设继续，并在回复中说明所做的假设）", nil
			},
		},
	}
}

// execJSON 解析/查询 JSON，避免模型为简单解析任务去写 Python 脚本。
func execJSON(_ context.Context, args map[string]any, env *Env) (string, error) {
	op, _ := args["op"].(string)
	if op == "" {
		op = "query"
	}

	// 取 JSON 文本：content 直接给，或从文件读
	var raw string
	if s, ok := args["content"].(string); ok && strings.TrimSpace(s) != "" {
		raw = s
	} else {
		p, err := argString(args, "path")
		if err != nil {
			return "", fmt.Errorf("json: 需要 content 或 path 其中之一")
		}
		abs, err := resolvePath(env.Cwd, p, env.Unsafe)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("json: %w", err)
		}
		raw = string(data)
	}

	// 容忍 ```json 包裹与前后缀垃圾文本
	raw = stripCodeFence(raw)

	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return "", fmt.Errorf("json: 解析失败: %w", err)
	}

	switch op {
	case "query":
		ptr, _ := args["pointer"].(string)
		ptr = strings.TrimSpace(ptr)
		if ptr == "" || ptr == "$" {
			return marshalOut(doc, env.MaxOutput)
		}
		v, err := jsonQuery(doc, ptr)
		if err != nil {
			return "", err
		}
		return marshalOut(v, env.MaxOutput)

	case "keys":
		m, ok := doc.(map[string]any)
		if !ok {
			if a, isArr := doc.([]any); isArr {
				// 数组取所有对象的键并集
				set := map[string]bool{}
				for _, it := range a {
					if im, ok := it.(map[string]any); ok {
						for k := range im {
							set[k] = true
						}
					}
				}
				keys := make([]string, 0, len(set))
				for k := range set {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				if len(keys) == 0 {
					return "数组元素均非对象，无键可列", nil
				}
				return "数组元素的键（" + fmt.Sprintf("%d 个", len(keys)) + "）：\n" + strings.Join(keys, "\n"), nil
			}
			return "根节点不是对象，没有键", nil
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Sprintf("根对象的键（%d 个）：\n%s", len(keys), strings.Join(keys, "\n")), nil

	case "shape":
		return jsonShape(doc, "$", 0), nil

	case "pretty":
		return marshalOut(doc, env.MaxOutput)

	default:
		return "", fmt.Errorf("json: 未知 op %q（可选 query / keys / shape / pretty）", op)
	}
}

// stripCodeFence 去掉 markdown 代码围栏与首尾空白，让模型可以直接传工具输出。
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	// 第一行可能是语言标记（json / JSON）
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		first := strings.TrimSpace(s[:i])
		if first == "" || !strings.ContainsAny(first, "{[\"") {
			s = s[i+1:]
		}
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// jsonQuery 按点号路径取值，支持 a.b.0.c 与 a.b[0].c 两种下标写法。
func jsonQuery(doc any, ptr string) (any, error) {
	cur := doc
	// 归一化：a.b[0].c → a.b.0.c
	p := strings.NewReplacer("[", ".", "]", "").Replace(ptr)
	for _, seg := range strings.Split(p, ".") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				keys := make([]string, 0, len(node))
				for k := range node {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				return nil, fmt.Errorf("json: 路径 %q 在此层不存在（可用键：%s）", seg, strings.Join(keys, ", "))
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("json: %q 是数组，需用数字下标", seg)
			}
			if i < 0 || i >= len(node) {
				return nil, fmt.Errorf("json: 下标 %d 越界（长度 %d）", i, len(node))
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("json: 路径 %q 处已是标量，无法继续下钻", seg)
		}
	}
	return cur, nil
}

// jsonShape 输出类型结构概览，帮助模型在不取值的情况下先摸清数据形状。
func jsonShape(v any, path string, depth int) string {
	if depth > 3 {
		return ""
	}
	pad := strings.Repeat("  ", depth)
	switch node := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(node))
		for k := range node {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s: object{%d}\n", pad, path, len(keys))
		for _, k := range keys {
			if s := jsonShape(node[k], k, depth+1); s != "" {
				b.WriteString(s)
			}
		}
		return b.String()
	case []any:
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s: array[%d]\n", pad, path, len(node))
		// 只看首个元素，避免同构数组刷屏
		if len(node) > 0 {
			if s := jsonShape(node[0], "[0]", depth+1); s != "" {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return fmt.Sprintf("%s%s: %s\n", pad, path, jsonTypeName(v))
	}
}

func jsonTypeName(v any) string {
	switch n := v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	default:
		_ = n
		return "unknown"
	}
}

// marshalOut 缩进序列化并按需截断。
func marshalOut(v any, maxOutput int) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("json: 序列化失败: %w", err)
	}
	s := string(b)
	if maxOutput > 0 && len(s) > maxOutput {
		return s[:maxOutput] + fmt.Sprintf("\n…（已截断，共 %d 字节）", len(s)), nil
	}
	return s, nil
}

// execTodo 无副作用：仅确认清单已更新（真实渲染由前端从工具参数读取）。
func execTodo(_ context.Context, args map[string]any, _ *Env) (string, error) {
	raw, ok := args["items"]
	if !ok {
		return "", fmt.Errorf("todo: 缺少 items")
	}
	list, ok := raw.([]any)
	if !ok {
		return "", fmt.Errorf("todo: items 需为数组")
	}
	var done, doing int
	for _, it := range list {
		m, _ := it.(map[string]any)
		st, _ := m["status"].(string)
		switch st {
		case "completed":
			done++
		case "in_progress":
			doing++
		}
	}
	return fmt.Sprintf("任务清单已更新（共 %d 项，已完成 %d，进行中 %d）", len(list), done, doing), nil
}

func execRead(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}

	// 办作文档：按扩展名自动提取文本（docx/pdf/pptx/xlsx 内置解析，无需 MCP）
	ext := strings.ToLower(filepath.Ext(abs))
	if docparse.Supported(ext) {
		sheet, _ := args["sheet"].(string)
		body, err := docparse.Extract(abs, docparse.Options{
			Page:  argInt(args, "page", 0),
			Sheet: sheet,
		})
		if err != nil {
			return "", fmt.Errorf("read: %w", err)
		}
		if body == "" {
			return "", fmt.Errorf("read: 文档没有可提取的文本")
		}
		lines := strings.Split(body, "\n")
		offset := argInt(args, "offset", 0)
		limit := argInt(args, "limit", 0)
		if offset < 0 {
			offset = 0
		}
		if offset > len(lines) {
			offset = len(lines)
		}
		end := len(lines)
		if limit > 0 && offset+limit < end {
			end = offset + limit
		}
		out := strings.Join(lines[offset:end], "\n")
		if env.MaxOutput > 0 && len(out) > env.MaxOutput {
			out = out[:env.MaxOutput] + "\n...（输出截断）"
		}
		return out, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	offset := argInt(args, "offset", 0)
	limit := argInt(args, "limit", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	sel := lines[offset:end]
	body := strings.Join(sel, "\n")
	if env.MaxOutput > 0 && len(body) > env.MaxOutput {
		body = body[:env.MaxOutput] + "\n...（输出截断）"
	}
	return body, nil
}

func execWrite(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	content, _ := args["content"].(string)
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	// 区分新建 / 覆盖（供摘要文案；差异渲染由 webui 在写入前抓取旧内容完成）
	_, statErr := os.Stat(abs)
	existed := statErr == nil
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	verb := "已新建"
	if existed {
		verb = "已覆盖写入"
	}
	return fmt.Sprintf("%s %s（%d 字节）", verb, abs, len(content)), nil
}

func execEdit(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	oldStr, err := argString(args, "old_string")
	if err != nil {
		return "", err
	}
	newStr, _ := args["new_string"].(string)
	replaceAll := argBool(args, "replace_all", false)
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	content := string(data)
	if oldStr == "" {
		return "", fmt.Errorf("edit: old_string 不能为空")
	}
	n := strings.Count(content, oldStr)
	if n == 0 {
		return "", fmt.Errorf("edit: 未找到 old_string。请确保与文件内容完全一致（含缩进/换行），可先用 read 查看")
	}
	if n > 1 && !replaceAll {
		return "", fmt.Errorf("edit: old_string 匹配了 %d 处，请给出更多上下文使匹配唯一，或设置 replace_all=true", n)
	}
	var out string
	if replaceAll {
		out = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		out = strings.Replace(content, oldStr, newStr, 1)
	}
	if err := os.WriteFile(abs, []byte(out), 0o644); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	return fmt.Sprintf("edit: 已替换 %d 处", n), nil
}

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("缺少参数 %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("参数 %s 必须为非空字符串", key)
	}
	return s, nil
}

func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
