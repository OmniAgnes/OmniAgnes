package webui

import (
	"encoding/json"
	"net/http"
	"sync"
)

// eventHub 是极简的变更广播：订阅者只收一个「有变化」信号（无载荷），
// 前端据此拉取任务列表。不带数据，避免广播方与订阅逻辑耦合。
type eventHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[chan struct{}]struct{}{}}
}

func (h *eventHub) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// broadcast 非阻塞唤醒所有订阅者（容量 1 的通道自动合并重复信号）。
// nil 接收者安全：无 hub（如测试裸构造 server）时静默跳过。
func (h *eventHub) broadcast() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// handleEvents 全局 SSE：前端常驻一条连接，收到 tasks_changed 就刷新任务列表。
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, "当前连接不支持流式输出", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
		fl.Flush()
	}
	send(map[string]any{"type": "hello"})
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			send(map[string]any{"type": "tasks_changed"})
		}
	}
}
