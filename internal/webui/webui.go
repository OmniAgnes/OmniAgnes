// Package webui 提供网页版工作台：任务/项目管理、聊天与设置。
// 本地 HTTP 服务 + SSE 流式输出，每个任务使用独立 Agent 并持久化到 store。
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/extras"
	"omni/internal/llm"
	"omni/internal/store"
	"omni/internal/tools"
)

//go:embed all:dist
var distFS embed.FS

// Options 是 webui 的启动参数。
type Options struct {
	// ConfigPath 配置文件路径（设置改动会写回）。
	ConfigPath string
	// Extras 是技能/记忆/MCP 扩展（与 TUI 共享）。
	Extras *extras.Extras
	// StartCwd 是 omni 的启动目录，无项目任务的默认工作目录。
	StartCwd string
	// Port 固定监听端口（桌面版传入，保证 localStorage 跨重启不丢）；为空则随机。
	Port string
	// Version 版本号（状态展示用）。
	Version string
}

var (
	mu     sync.Mutex
	srvURL string
)

// Start 启动网页版服务（仅监听 127.0.0.1，重复调用复用已有服务），返回访问地址。
func Start(opts Options) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if srvURL != "" {
		return srvURL, nil
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return "", err
	}
	addr := "127.0.0.1:0"
	if opts.Port != "" {
		addr = "127.0.0.1:" + opts.Port
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil && opts.Port != "" {
		// 固定端口被占用（已有实例在跑）时退回随机端口，保证仍能启动
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return "", err
	}
	mob, err := newMobile()
	if err != nil {
		_ = ln.Close()
		return "", err
	}
	s := &server{
		opts:   opts,
		cfg:    cfg,
		store:  store.New(config.DefaultDataDir()),
		agents: map[string]*agent.Agent{},
		regs:   map[string]*tools.Registry{},
		approvals: map[string]*approvalReq{},
		questions: map[string]*questionReq{},
		browser: newBrowserBridge(),
		hub:     newEventHub(),
		mobile:  mob,
	}
	srv := &http.Server{Handler: s.routes()}
	go func() { _ = srv.Serve(ln) }()
	s.startScheduler()
	// 后台预热 opencode 免费模型清单：启动时顺手拉一次，等用户点开模型菜单时
	// 通常已经是最新清单（拉不到也不阻塞启动，菜单用兜底种子）。
	if cfg.OpenCodeFreeEnabled() {
		go s.refreshOpenCodeFree(context.Background())
	}
	// 移动端访问开关是记住的：上次是开的就自动把局域网监听拉起来，
	// 省得每次重启都要重新点一次开关（二维码里的 token 不变，扫码照旧）。
	if mob.persistEnabled {
		if _, err := s.mobileStart(); err != nil {
			fmt.Fprintf(os.Stderr, "omni: 恢复移动端访问失败: %v\n", err)
		}
	}
	srvURL = "http://" + ln.Addr().String()
	return srvURL, nil
}

// Launch 启动服务并尝试用系统浏览器打开，返回 URL。
func Launch(opts Options) (string, error) {
	url, err := Start(opts)
	if err != nil {
		return "", err
	}
	_ = openBrowser(url)
	return url, nil
}

// routes 组装全部路由。
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// 前端静态资源（dist 内 index.html + assets）
	sub, err := fs.Sub(distFS, "dist")
	if err == nil {
		mux.Handle("GET /", http.FileServerFS(sub))
	}
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/server-url", s.handleServerURL)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("POST /api/test-model", s.handleTestModel)
	mux.HandleFunc("GET /api/providers", s.handleProvidersGet)
	mux.HandleFunc("PUT /api/providers", s.handleProvidersPut)
	mux.HandleFunc("GET /api/projects", s.handleProjectsList)
	mux.HandleFunc("POST /api/projects", s.handleProjectsAdd)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleProjectDelete)
	mux.HandleFunc("POST /api/projects/{id}/open", s.handleProjectOpen)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("GET /api/tasks", s.handleTasksList)
	mux.HandleFunc("POST /api/tasks", s.handleTaskCreate)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTaskGet)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.handleTaskDelete)
	mux.HandleFunc("GET /api/schedules", s.handleSchedulesList)
	mux.HandleFunc("POST /api/schedules", s.handleScheduleCreate)
	mux.HandleFunc("PUT /api/schedules/{id}", s.handleScheduleUpdate)
	mux.HandleFunc("DELETE /api/schedules/{id}", s.handleScheduleDelete)
	mux.HandleFunc("POST /api/schedules/{id}/run", s.handleScheduleRun)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("GET /api/attachment", s.handleAttachment)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/approvals/{id}", s.handleApproval)
	mux.HandleFunc("POST /api/questions/{id}", s.handleQuestionAnswer)
	mux.HandleFunc("GET /api/settings", s.handleSettingsGet)
	mux.HandleFunc("POST /api/settings", s.handleSettingsSet)
	mux.HandleFunc("GET /api/memory", s.handleMemoryGet)
	mux.HandleFunc("PUT /api/memory", s.handleMemorySet)
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	mux.HandleFunc("GET /api/mcp", s.handleMCP)
	mux.HandleFunc("GET /api/mcp/config", s.handleMCPConfigGet)
	mux.HandleFunc("PUT /api/mcp/config", s.handleMCPConfigPut)
	mux.HandleFunc("POST /api/mcp/open", s.handleMCPOpen)
	mux.HandleFunc("GET /api/skills", s.handleSkills)
	mux.HandleFunc("GET /api/skills/dirs", s.handleSkillDirs)
	mux.HandleFunc("POST /api/skills/import", s.handleSkillImport)
	mux.HandleFunc("POST /api/skills/open", s.handleSkillOpen)
	mux.HandleFunc("POST /api/skills/toggle", s.handleSkillToggle)
	mux.HandleFunc("GET /api/skills/{name}", s.handleSkillGet)
	mux.HandleFunc("POST /api/skills", s.handleSkillCreate)
	mux.HandleFunc("PUT /api/skills/{name}", s.handleSkillUpdate)
	mux.HandleFunc("DELETE /api/skills/{name}", s.handleSkillDelete)
	mux.HandleFunc("GET /api/browser/next", s.handleBrowserNext)
	mux.HandleFunc("POST /api/browser/result", s.handleBrowserResult)
	// 移动端远程访问：开关、扫码信息与换发 token（仅桌面端 127.0.0.1 服务可调用）
	mux.HandleFunc("GET /api/mobile", s.handleMobileInfo)
	mux.HandleFunc("POST /api/mobile", s.handleMobileToggle)
	mux.HandleFunc("POST /api/mobile/rotate", s.handleMobileRotate)
	return mux
}

// server 是 webui 服务端状态。
type server struct {
	opts   Options
	cfg    *config.Config
	store  *store.Store
	mu     sync.Mutex
	agents map[string]*agent.Agent
	// regs 与 agents 一一对应：MCP 配置变更后据此把新工具重新注册进各任务。
	regs map[string]*tools.Registry
	// approvals 保存等待用户确认的写操作（普通访问模式）。
	approvals map[string]*approvalReq
	// questions 保存等待用户回答的 ask 提问。
	questions map[string]*questionReq
	// browser 是内置浏览器桥（desktop 壳经 /api/browser/* 长轮询驱动）。
	browser *BrowserBridge
	// hub 是任务变更广播（计划任务后台建/改任务时通知前端刷新列表）。
	hub *eventHub
	// mobile 是局域网监听器状态（设备唯一 token + 开关）。
	mobile *mobile
	// mobileOnce 保证 mobile 懒初始化只跑一次（测试直接构造 &server{} 时也安全）。
	mobileOnce sync.Once
	// runningTasks 是后台无人值守执行中的任务 ID（计划任务轮次），供任务列表标注运行状态。
	runningTasks sync.Map
	// approvalSeq 生成审批请求 ID。
	approvalSeq atomic.Int64
	// provider 非空时优先使用（测试注入口），否则按 cfg 解析。
	provider llm.Provider
}

// approvalReq 是一次待用户确认的工具调用。
type approvalReq struct {
	id   string
	name string
	args map[string]any
	ch   chan bool
}

// questionReq 是一次 ask 工具发起、等待用户回答的提问。
type questionReq struct {
	id       string
	question string
	options  []string
	ch       chan string
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

// decode 读取 JSON 请求体。
func decode[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(r.Body).Decode(&v)
	return v, err
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
