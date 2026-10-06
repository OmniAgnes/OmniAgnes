package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/data":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		case "/vnd":
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.Write([]byte(`{}`))
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<html></html>`))
		case "/head-reject":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cases := []struct {
		url  string
		want bool
	}{
		{srv.URL + "/api/data", true},    // Content-Type: application/json
		{srv.URL + "/vnd", true},         // +json 后缀类型同样命中
		{srv.URL + "/page", false},       // HTML 页面照常自动展开
		{srv.URL + "/missing", false},    // 5xx 且非 JSON 按非 JSON 处理
		{srv.URL + "/head-reject", true}, // HEAD 被拒退化 GET 探测
		{"example.com/data.json", true},  // .json 扩展名直接判定，不发请求
	}
	for _, c := range cases {
		if got := probeJSON(context.Background(), c.url); got != c.want {
			t.Errorf("probeJSON(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}
