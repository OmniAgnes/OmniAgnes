// Package web 提供前端静态资源服务（SPA：非 /api 路径回退 index.html）。
package web

import (
	"net/http"
	"os"
	"path/filepath"
)

// DirFS 以磁盘目录托管前端 dist；index.html 不存在或文件缺失时回退 SPA 入口。
func DirFS(root string) http.Handler {
	if root == "" {
		return nil
	}
	index := filepath.Join(root, "index.html")
	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fileServer.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.ServeFile(w, r, index)
			return
		}
		p := filepath.Join(root, filepath.FromSlash(r.URL.Path))
		// 存在真实文件就正常服务，否则回退 SPA（react-router/前端路由）
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}