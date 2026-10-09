package webui

import (
	"context"
	"sync"
	"time"

	"omni/internal/config"
	"omni/internal/llm"
)

// openCodeFreeTTL 是网关注册表的缓存时长：模型上下架很慢，6 小时足够新鲜。
const openCodeFreeTTL = 6 * time.Hour

// openCodeFreeWait 是 /api/models 为拉取清单最多等待的时间。
// 启动时已预热过一次，这里只是兜底；网关不可达时宁可先用兜底清单，
// 也不能让模型菜单干等十几秒。
const openCodeFreeWait = 4 * time.Second

// openCodeFreeMu 保护「免费模型组」相关的 cfg.Providers 改写：后台刷新协程
// 与请求处理（/api/models、PUT /api/providers、POST /api/settings）会并发。
var openCodeFreeMu sync.Mutex

// openCodeFreeFetch 串行化对网关的拉取。启动预热与首屏请求可能同时触发；
// 用 TryLock 让后来者直接用当前清单，不排队也不重复打同一个网关。
var openCodeFreeFetch sync.Mutex

var openCodeFreeCache struct {
	// models 为最近一次成功拉到的清单（拉取失败时原样保留）
	models []string
	// at 为最近一次成功拉取时间
	at time.Time
}

// ensureOpenCodeFree 保证免费模型组用的是尽可能新的清单。
// 缓存新鲜直接返回；其余情况同步拉一次（上限 openCodeFreeWait）：
// 菜单是一次性渲染的，返回旧清单用户就再也没机会看到新模型了。
// 已有拉取在途（多半是启动预热）时直接用当前清单，不排队。
func (s *server) ensureOpenCodeFree(ctx context.Context) {
	// 组没注入（如测试直接用 config.Default() 组装）就不去打网关
	if !s.cfg.HasOpenCodeFree() {
		return
	}
	openCodeFreeMu.Lock()
	fresh := len(openCodeFreeCache.models) > 0 && time.Since(openCodeFreeCache.at) < openCodeFreeTTL
	openCodeFreeMu.Unlock()
	if fresh {
		return
	}
	if !openCodeFreeFetch.TryLock() {
		return
	}
	defer openCodeFreeFetch.Unlock()
	ctx, cancel := context.WithTimeout(ctx, openCodeFreeWait)
	defer cancel()
	s.refreshOpenCodeFree(ctx)
}

// refreshOpenCodeFree 拉一次网关清单并写回虚拟供应商组。
// 拉取失败不改任何东西：已有清单继续用，首次则由 config 的兜底种子顶着——
// 免费组本就是无网也能选的通道，不能因为网关抽风就把菜单清空。
//
// 后台调用（启动预热 / 开关刚打开）也会改写 cfg.Providers，这和请求处理里
// PUT /api/providers 整体替换 providers 是同一套约定：cfg 本身没有全局锁，
// 写方之间由 openCodeFreeMu 互斥。读方（聊天/计划任务按模型反查供应商）
// 撞上后台写的窗口极小——每 6 小时至多一次，且首次几乎总在客户端接入之前。
func (s *server) refreshOpenCodeFree(ctx context.Context) {
	if !s.cfg.HasOpenCodeFree() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, openCodeFreeWait*3)
	defer cancel()
	prov, err := llm.New(llm.ConfigP{Type: config.OpenCodeProviderType})
	if err != nil {
		return
	}
	// 网络请求放在锁外，只在写回结果时持锁
	models, err := prov.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return
	}
	openCodeFreeMu.Lock()
	defer openCodeFreeMu.Unlock()
	openCodeFreeCache.models = models
	openCodeFreeCache.at = time.Now()
	s.cfg.SyncOpenCodeFree(models)
}

// setOpenCodeFree 切换开关并立刻反映到供应商列表上。
func (s *server) setOpenCodeFree(on bool) {
	openCodeFreeMu.Lock()
	defer openCodeFreeMu.Unlock()
	if !on {
		s.cfg.DropOpenCodeFree()
		return
	}
	s.cfg.SetOpenCodeFree(true)
	s.cfg.SyncOpenCodeFree(openCodeFreeCache.models)
}