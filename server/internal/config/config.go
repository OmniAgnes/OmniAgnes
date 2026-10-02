// Package config 加载 server 运行配置（统一域名 base_url、监听、数据库、测试模式）。
package config

import (
	"flag"
	"os"
)

type Config struct {
	Addr     string // 监听地址 :9000
	BaseURL  string // 统一域名，默认 http://127.0.0.1:9000，前端 API 地址与联调用
	DBPath   string // SQLite 文件
	TestMode bool   // 测试模式：验证码固定 111111，可自由登录/自动注册
	WebDir   string // 前端静态目录（dist），供本地开发用盘载服务
}

func Load() *Config {
	addr := flag.String("addr", envOr("OMNI_SERVER_ADDR", ":9000"), "监听地址")
	base := flag.String("base", envOr("OMNI_SERVER_BASE", "http://127.0.0.1:9000"), "统一域名 base_url")
	db := flag.String("db", envOr("OMNI_SERVER_DB", "server.db"), "SQLite 文件路径")
	testMode := flag.Bool("test-mode", envBool("OMNI_SERVER_TEST", true), "测试模式（验证码 111111）")
	web := flag.String("web", "dist", "前端静态目录（dist）")
	flag.Parse()
	return &Config{
		Addr:     *addr,
		BaseURL:  *base,
		DBPath:   *db,
		TestMode: *testMode,
		WebDir:   *web,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "1" || v == "true" || v == "on"
	}
	return def
}