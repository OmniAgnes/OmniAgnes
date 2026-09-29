// Package mcp 实现 MCP（Model Context Protocol）客户端管理：
// 按配置连接 stdio / streamable HTTP 服务器，把远端工具动态注入 tools.Registry。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"omni/internal/config"
	"omni/internal/tools"
)

// Prefix 是 MCP 工具名的命名空间前缀。
const Prefix = "mcp__"

// Status 是单个 MCP 服务器的连接状态（供 /mcp 命令与 webui 展示）。
type Status struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`   // stdio | http
	Target    string `json:"target"` // 启动命令或 URL
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
	Err       string `json:"error,omitempty"`
}

// Manager 持有全部 MCP 会话与其连接状态。
type Manager struct {
	mu       sync.Mutex
	client   *mcp.Client
	sessions map[string]*mcp.ClientSession
	statuses map[string]*Status
	onChange func()
}

// NewManager 创建管理器。version 用于握手标识。
func NewManager(version string) *Manager {
	m := &Manager{
		sessions: map[string]*mcp.ClientSession{},
		statuses: map[string]*Status{},
	}
	m.client = mcp.NewClient(
		&mcp.Implementation{Name: "omni", Version: version},
		&mcp.ClientOptions{
			ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
				m.mu.Lock()
				fn := m.onChange
				m.mu.Unlock()
				if fn != nil {
					go fn()
				}
			},
		},
	)
	return m
}

// SetOnChange 注册工具列表变化回调（服务器发出 tools/list_changed 时触发，外层据此刷新注册表）。
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// Start 逐个连接配置的 MCP 服务器（10s 握手超时）。
// 连接失败只记录到状态里，不中断启动；返回带错误信息的状态。
func (m *Manager) Start(ctx context.Context, servers map[string]config.MCPServerConfig) []Status {
	var failed []Status
	for name, cfg := range servers {
		if cfg.Disabled {
			continue
		}
		if _, err := m.Connect(ctx, name, cfg); err != nil {
			failed = append(failed, *m.status(name))
		}
	}
	return failed
}

// Connect 连接单个服务器并拉取一次工具列表。
func (m *Manager) Connect(ctx context.Context, name string, cfg config.MCPServerConfig) (*Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kind, target := "stdio", strings.TrimSpace(cfg.Command+" "+strings.Join(cfg.Args, " "))
	if cfg.URL != "" {
		kind, target = "http", cfg.URL
	}
	st := &Status{Name: name, Kind: kind, Target: target}
	m.statuses[name] = st

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var transport mcp.Transport
	if kind == "http" {
		transport = &mcp.StreamableClientTransport{Endpoint: cfg.URL}
	} else {
		if cfg.Command == "" {
			st.Err = "缺少 command 或 url"
			return nil, fmt.Errorf("mcp %s: %s", name, st.Err)
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if len(cfg.Env) > 0 {
			cmd.Env = append(cmd.Environ(), envPairs(cfg.Env)...)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	}

	session, err := m.client.Connect(connectCtx, transport, nil)
	if err != nil {
		st.Err = err.Error()
		return nil, fmt.Errorf("mcp %s 连接失败: %w", name, err)
	}
	m.sessions[name] = session
	st.Connected = true
	st.Err = ""

	tools, err := listTools(ctx, session)
	if err != nil {
		st.Err = "工具列表获取失败: " + err.Error()
	} else {
		st.ToolCount = len(tools)
	}
	return st, nil
}

// RegisterInto 把当前全部 MCP 工具重新注册进注册表（先清掉旧前缀再注册）。
// 服务器工具列表可能已变化，这里实时重新拉取。
func (m *Manager) RegisterInto(reg *tools.Registry) {
	if reg == nil {
		return
	}
	m.mu.Lock()
	sessions := make(map[string]*mcp.ClientSession, len(m.sessions))
	for name, s := range m.sessions {
		if s != nil && m.statuses[name] != nil && m.statuses[name].Connected {
			sessions[name] = s
		}
	}
	m.mu.Unlock()

	reg.UnregisterByPrefix(Prefix)
	for name, session := range sessions {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		toolsForServer, err := listTools(ctx, session)
		cancel()
		if err != nil {
			continue
		}
		for _, tl := range toolsForServer {
			reg.Register(convertTool(name, session, tl))
		}
		if st := m.status(name); st != nil {
			m.mu.Lock()
			st.ToolCount = len(toolsForServer)
			m.mu.Unlock()
		}
	}
}

// Statuses 返回全部服务器状态快照。
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, *st)
	}
	return out
}

// Close 关闭全部会话。
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.sessions {
		if s != nil {
			_ = s.Close()
		}
		delete(m.sessions, name)
	}
}

// Reset 关闭全部会话并清空状态（配置变更后重新连接前调用）。
func (m *Manager) Reset() {
	m.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses = map[string]*Status{}
}

func (m *Manager) status(name string) *Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statuses[name]; ok {
		return st
	}
	return &Status{Name: name}
}

// listTools 拉取服务器全部工具（处理分页）。
func listTools(ctx context.Context, session *mcp.ClientSession) ([]*mcp.Tool, error) {
	var out []*mcp.Tool
	cursor := ""
	for {
		res, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			return out, nil
		}
		cursor = res.NextCursor
	}
}

// convertTool 把 MCP 工具定义转成 omni 的 *tools.Tool。
func convertTool(server string, session *mcp.ClientSession, tl *mcp.Tool) *tools.Tool {
	name := Prefix + server + "__" + tl.Name
	desc := strings.TrimSpace(tl.Description)
	if tl.Annotations != nil && tl.Annotations.Title != "" {
		desc = tl.Annotations.Title + ": " + desc
	}
	desc = fmt.Sprintf("[mcp:%s] %s", server, desc)
	readOnly := tl.Annotations != nil && tl.Annotations.ReadOnlyHint

	return &tools.Tool{
		Name:        name,
		Description: desc,
		Parameters:  inputSchema(tl.InputSchema),
		ReadOnly:    readOnly,
		Execute: func(ctx context.Context, args map[string]any, env *tools.Env) (string, error) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      tl.Name,
				Arguments: args,
			})
			if err != nil {
				return "", err
			}
			out := contentToText(res.Content)
			if out == "" && res.StructuredContent != nil {
				if b, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
					out = string(b)
				}
			}
			if res.IsError {
				return out, fmt.Errorf("%s", out)
			}
			return out, nil
		},
	}
}

// inputSchema 把 MCP 的 inputSchema 转为 JSON Schema map。
func inputSchema(v any) map[string]any {
	if v == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return m
}

// contentToText 把 MCP 内容块转成纯文本。
func contentToText(contents []mcp.Content) string {
	var sb strings.Builder
	for _, c := range contents {
		switch t := c.(type) {
		case *mcp.TextContent:
			sb.WriteString(t.Text)
		case *mcp.ImageContent:
			fmt.Fprintf(&sb, "[图片: %s]", t.MIMEType)
		case *mcp.AudioContent:
			fmt.Fprintf(&sb, "[音频: %s]", t.MIMEType)
		case *mcp.ResourceLink:
			fmt.Fprintf(&sb, "[资源链接: %s]", t.URI)
		case *mcp.EmbeddedResource:
			if t.Resource != nil {
				fmt.Fprintf(&sb, "[资源: %s]", t.Resource.URI)
			}
		default:
			sb.WriteString("[未知内容类型]")
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func envPairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
