package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/extras"
	"omni/internal/llm"
	"omni/internal/mcp"
	"omni/internal/skills"
	"omni/internal/tools"
	"omni/internal/tui"
	"omni/internal/webui"
)

var version = "0.2.0"

func main() {
	var (
		flagConfig  = flag.String("config", "", "配置文件路径（默认 ~/.config/omni/config.json）")
		flagModel   = flag.String("model", "", "覆盖模型名")
		flagMode    = flag.String("mode", "", "agent 模式：build 或 plan")
		flagCwd     = flag.String("cwd", "", "工作目录（默认当前目录）")
		flagUnsafe  = flag.Bool("unsafe", false, "允许工具访问工作目录之外的文件")
		flagPrint   = flag.String("print", "", "非交互模式：执行单轮问答并输出结果后退出")
		flagVersion = flag.Bool("version", false, "打印版本")
		flagTimeout = flag.Duration("timeout", 0, "非交互模式整体超时（如 5m）")
		flagWeb     = flag.Bool("web", false, "不进 TUI，直接启动网页版工作台")
		flagNoOpen  = flag.Bool("no-open", false, "启动网页版时不自动打开系统浏览器（桌面版/调试用）")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Printf("omni %s\n", version)
		return
	}

	cfg, err := config.Load(*flagConfig)
	if err != nil {
		exitErr("加载配置失败: %v", err)
	}

	prov, model, err := llm.ResolveProvider(cfg)
	if err != nil {
		// 交互模式允许无配置启动，进 TUI 后用 /config 配置；非交互模式必须可用。
		if *flagPrint != "" {
			exitErr("%v", err)
		}
		fmt.Fprintln(os.Stderr, "omni: "+err.Error()+"（已进入 TUI，请用 /config 配置模型）")
	}

	if *flagModel != "" {
		model = *flagModel
	}

	cwd := *flagCwd
	if cwd == "" {
		cwd = cfg.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		exitErr("解析工作目录失败: %v", err)
	}
	if st, err := os.Stat(absCwd); err != nil || !st.IsDir() {
		exitErr("工作目录不存在: %s", absCwd)
	}

	// 运行时扩展：MCP 服务器、技能、全局记忆
	// 内置技能（go:embed 打包）先补齐到全局技能目录，缺什么补什么、不覆盖用户改动
	if err := skills.SeedBuiltIn(config.DefaultSkillsDir()); err != nil {
		fmt.Fprintf(os.Stderr, "omni: 内置技能同步失败: %v\n", err)
	}
	// 技能禁用名单 → 集合，供 Store 过滤
	disabled := map[string]bool{}
	for _, n := range cfg.DisabledSkills {
		disabled[n] = true
	}
	ext := &extras.Extras{
		Skills:     &skills.Store{GlobalDir: config.DefaultSkillsDir(), Disabled: disabled},
		MemoryPath: config.DefaultMemoryPath(),
	}
	reg := tools.NewRegistry(&tools.Env{
		Cwd:       absCwd,
		Unsafe:    *flagUnsafe,
		MaxOutput: cfg.MaxToolOutput,
		Timeout:   time.Duration(cfg.BashTimeoutSec) * time.Second,
	})
	if len(cfg.MCPServers) > 0 {
		mgr := mcp.NewManager(version)
		ext.MCP = mgr
		mgr.SetOnChange(func() { mgr.RegisterInto(reg) })
		ext.RegisterTools(reg, absCwd)
		for _, st := range mgr.Start(context.Background(), cfg.MCPServers) {
			fmt.Fprintf(os.Stderr, "omni: mcp %s 连接失败: %s\n", st.Name, st.Err)
		}
	}
	ext.RegisterTools(reg, absCwd)

	mode := agent.ModeBuild
	if *flagMode != "" {
		mode = agent.Mode(*flagMode)
	}
	if mode != agent.ModeBuild && mode != agent.ModePlan {
		exitErr("未知 agent 模式: %s（可选 build / plan）", mode)
	}
	maxTokens := 8192
	if p := cfg.ProviderForModel(model); p != nil {
		if n := p.OptionsFor(model).MaxOutputTokens; n > 0 {
			maxTokens = n
		}
	}
	a := agent.New(prov, reg, agent.Options{
		Model:             model,
		Mode:              mode,
		Cwd:               absCwd,
		MaxIterations:     500,
		MaxTokens:         maxTokens,
		SystemPromptExtra: cfg.SystemPromptExtra,
		ExtraPrompt:       func() string { return ext.ExtraPrompt(absCwd) },
	})

	if *flagPrint != "" {
		runPrint(*flagPrint, a, *flagTimeout)
		return
	}

	configPath := *flagConfig
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	opts := webui.Options{
		ConfigPath: configPath,
		Extras:     ext,
		StartCwd:   absCwd,
		Version:    version,
	}

	if *flagWeb {
		var (
			url string
			err error
		)
		if *flagNoOpen {
			url, err = webui.Start(opts)
		} else {
			url, err = webui.Launch(opts)
		}
		if err != nil {
			exitErr("启动网页版失败: %v", err)
		}
		fmt.Printf("omni 工作台已启动: %s（Ctrl+C 退出）\n", url)
		select {}
	}

	sessionID := newSessionID()
	p := tea.NewProgram(tui.New(a, sessionID, configPath, ext, absCwd), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		exitErr("TUI 运行失败: %v", err)
	}
}

func exitErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omni: "+format+"\n", args...)
	os.Exit(1)
}

// runPrint 非交互模式执行一次问答并输出。
func runPrint(query string, a *agent.Agent, timeout time.Duration) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	_ = a.Prepare(query)
	var sb strings.Builder
	var toolEvents []string
	err := a.Execute(ctx, func(ev agent.Event) {
		switch ev.Type {
		case agent.EventText:
			sb.WriteString(ev.Text)
		case agent.EventToolStart:
			toolEvents = append(toolEvents, fmt.Sprintf("[tool] %s %s", ev.Tool.Name, llm.MarshalToolArgs(ev.Tool.Args)))
		case agent.EventToolOutput:
			toolEvents = append(toolEvents, fmt.Sprintf("[tool-output] %s -> %s", ev.Tool.Name, truncateTo(ev.Output, 500)))
		case agent.EventError:
			fmt.Fprintf(os.Stderr, "omni: %v\n", ev.Err)
		}
	})
	for _, line := range toolEvents {
		fmt.Fprintln(os.Stderr, line)
	}
	if err != nil {
		if timeout > 0 && ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "omni: 超时\n")
			os.Exit(124)
		}
		fmt.Fprintf(os.Stderr, "omni: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(sb.String())
}

func truncateTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func newSessionID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b)[:8]
}
