// omni-server：模型网关 + 计费/管理后台。默认监听 127.0.0.1:9000，
// 提供用户面（手机号验证码登录/积分）与管理面（会员/模型/供应商）。
package main

import (
	"log"
	"net/http"

	"omni-server/internal/api"
	"omni-server/internal/config"
	"omni-server/internal/store"
	"omni-server/internal/web"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	srv := api.New(st, cfg, web.DirFS(cfg.WebDir))
	log.Printf("omni-server 启动: %s  (test_mode=%v, db=%s)", cfg.BaseURL, cfg.TestMode, cfg.DBPath)
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}