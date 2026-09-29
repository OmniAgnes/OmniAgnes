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
	"omni/internal/llm"
	"omni/internal/tools"
	"omni/internal/tui"
)

var version = "0.1.0"

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

	prov, model, err := resolveProvider(cfg)
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

	reg := tools.NewRegistry(&tools.Env{
		Cwd:       absCwd,
		Unsafe:    *flagUnsafe,
		MaxOutput: cfg.MaxToolOutput,
		Timeout:   time.Duration(cfg.BashTimeoutSec) * time.Second,
	})

	mode := agent.ModeBuild
	if *flagMode != "" {
		mode = agent.Mode(*flagMode)
	}
	if mode != agent.ModeBuild && mode != agent.ModePlan {
		exitErr("未知 agent 模式: %s（可选 build / plan）", mode)
	}
	a := agent.New(prov, reg, agent.Options{
		Model:             model,
		Mode:              mode,
		Cwd:               absCwd,
		MaxIterations:     32,
		MaxTokens:         8192,
		SystemPromptExtra: cfg.SystemPromptExtra,
	})

	if *flagPrint != "" {
		runPrint(*flagPrint, a, *flagTimeout)
		return
	}

	sessionID := newSessionID()
	configPath := *flagConfig
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	p := tea.NewProgram(tui.New(a, sessionID, configPath), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		exitErr("TUI 运行失败: %v", err)
	}
}

func exitErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omni: "+format+"\n", args...)
	os.Exit(1)
}

// resolveProvider 从配置或环境变量解析 LLM provider。
func resolveProvider(cfg *config.Config) (llm.Provider, string, error) {
	if len(cfg.Providers) > 0 {
		target := cfg.Provider
		if target == "" {
			target = cfg.Model
		}
		var pick *config.ProviderConfig
		if target != "" {
			for i := range cfg.Providers {
				if cfg.Providers[i].Model == target {
					pick = &cfg.Providers[i]
					break
				}
			}
		}
		if pick == nil {
			for i := range cfg.Providers {
				if cfg.Providers[i].Default {
					pick = &cfg.Providers[i]
					break
				}
			}
		}
		if pick == nil {
			pick = &cfg.Providers[0]
		}
		p := *pick
		apiKey := p.ResolveAPIKey()
		// ollama/本地端点无需 key
		needKey := apiKey == ""
		if needKey && p.Type == "ollama" {
			needKey = false
		}
		if needKey && strings.Contains(p.BaseURL, "localhost") {
			needKey = false
		}
		if needKey {
			return nil, "", fmt.Errorf("provider %s 缺少 API Key：请配置 providers[].api_key 或 env_key，或设置对应环境变量", p.Type)
		}
		model := p.Model
		prov, err := llm.New(llm.ConfigP{Type: p.Type, Model: model, BaseURL: p.BaseURL, APIKey: apiKey})
		if err != nil {
			return nil, "", err
		}
		return prov, model, nil
	}

	typ, model, base, ok := config.DetectFromEnv()
	if !ok {
		return nil, "", fmt.Errorf(
			"未找到可用的 LLM Provider。请在 %s 配置 providers，或设置 OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY / OLLAMA_BASE_URL 环境变量",
			config.DefaultPath(),
		)
	}
	apiKey := ""
	switch typ {
	case "openai":
		apiKey = os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	case "gemini":
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	prov, err := llm.New(llm.ConfigP{Type: typ, Model: model, BaseURL: base, APIKey: apiKey})
	if err != nil {
		return nil, "", err
	}
	return prov, model, nil
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
