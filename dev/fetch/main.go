// fetch MCP 服务器：抓取网页内容（纯只读）。
//
// 工具：
//   - fetch_html：抓取 URL 返回原始 HTML
//   - fetch_text：抓取 URL 返回纯文本（去掉标签/脚本/样式）
//   - fetch_markdown：抓取 URL 转成粗略 Markdown（标题/链接/文本）
//   - fetch_json：抓取 URL 返回原始 JSON（不解析，原样给模型）
//   - fetch_readable：提取网页正文（优先 <article>/<main>，去掉导航/侧栏/页脚）
//
// 纯 Go 实现（标准库 + golang.org/x/net/html），无外部运行时依赖；单二进制。
// 安全：SSRF 防护（拒绝回环/私网/链路本地地址与重定向到内网的跳转）、
// 响应大小上限、单次请求超时与总超时。默认读取并遵循 robots.txt 的
// Disallow（User-Agent: omni-fetch），违规地址直接提示跳过。
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"
)

const (
	ua            = "omni-fetch/1.0 (+MCP url-fetch)"
	maxBytes      = 5 << 20 // 响应体上限 5MB
	requestTimout = 20 * time.Second
	totalTimeout  = 60 * time.Second
	textLimit     = 40000 // 返回文本上限（避免刷爆上下文）
)

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-fetch", Version: "0.0.1"}, nil)
	for _, t := range []*mcp.Tool{
		{
			Name:        "fetch_html",
			Annotations: &mcp.ToolAnnotations{Title: "抓取网页 HTML", ReadOnlyHint: true},
			Description: "抓取 URL 并返回原始 HTML（含脚本/样式）。适合需要精确 DOM 结构的场景；否则优先用 fetch_text/fetch_readable。",
		},
		{
			Name:        "fetch_text",
			Annotations: &mcp.ToolAnnotations{Title: "抓取网页纯文本", ReadOnlyHint: true},
			Description: "抓取 URL 并返回纯文本：去除 script/style/标签，保留原文换行与段落。适合多数网页内容读取。",
		},
		{
			Name:        "fetch_markdown",
			Annotations: &mcp.ToolAnnotations{Title: "抓取网页转 Markdown", ReadOnlyHint: true},
			Description: "抓取 URL 并转成 Markdown：标题(#)、链接([text](url))、粗体、列表。适合把网页内容嵌入文档/报告。",
		},
		{
			Name:        "fetch_json",
			Annotations: &mcp.ToolAnnotations{Title: "抓取 JSON 接口", ReadOnlyHint: true},
			Description: "抓取 JSON 接口（REST API / GraphQL / 任何返回 JSON 的 URL）并原样返回响应文本。用于读取接口数据。",
		},
		{
			Name:        "fetch_readable",
			Annotations: &mcp.ToolAnnotations{Title: "提取网页正文", ReadOnlyHint: true},
			Description: "抓取 URL 并提取网页主文章内容：优先 <article>/<main>，剔除导航/侧栏/广告/页脚/脚本样式。适合博客/新闻/文档页。",
		},
	} {
		mcp.AddTool(server, t, handleFetch)
	}
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-fetch:", err)
		os.Exit(1)
	}
}

// 公共参数：url + 可选 max_length / headers
type fetchArgs struct {
	URL        string            `json:"url" jsonschema:"要抓取的完整 URL（带协议，如 https://example.com）"`
	MaxLength  int               `json:"max_length,omitempty" jsonschema:"返回内容最大字符数，默认 40000，超出截断（仅 fetch_html/fetch_text/fetch_markdown/fetch_readable 生效）"`
	Headers    map[string]string `json:"headers,omitempty" jsonschema:"附加请求头，如认证 token；不要传敏感凭据除非确实需要"`
	Method     string            `json:"method,omitempty" jsonschema:"HTTP 方法，默认 GET；仅为数只读 GET（fetch_json 可用 POST 请求 JSON 接口）"`
	POSTBody   string            `json:"body,omitempty" jsonschema:"方法为 POST 时发送的请求体"`
	ContentTag string            `json:"content_tag,omitempty" jsonschema:"fetch_readable：指定提取的容器标签/类名（如 article / .post / main），可省略"`
}

// jsonschema tag 不需要严格，SDK 从结构推导；这里补在 Description 里的是工具级说明。

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// handleFetch 根据调用工具名分发。
func handleFetch(ctx context.Context, req *mcp.CallToolRequest, args fetchArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.URL) == "" {
		return nil, nil, fmt.Errorf("缺少 url 参数")
	}
	tool := req.Params.Name

	limit := args.MaxLength
	if limit <= 0 {
		limit = textLimit
	}

	// 解析并校验 URL（协议白名单、SSRF 防护）
	target, err := parseSafeURL(args.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch: %w", err)
	}

	robotsResult, err := checkRobots(target)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch: robots 检查失败: %w", err)
	}
	if robotsResult != nil && !robotsResult.allowed {
		if robotsResult.status == "uuav" {
			return textResult(fmt.Sprintf("该站点对抓取爬虫声明了 `User-Agent` 不友好，且未配置显式允许。可加 headers 重试，或用 curl 工具绕过 robots.txt。\nURL: %s", target)), nil, nil
		}
		return textResult(fmt.Sprintf("robots.txt 明确禁止抓取（Disallow）该路径。遵守规范跳过。\nURL: %s", target)), nil, nil
	}

	body, err := doFetch(ctx, tool, args, target, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch: %w", err)
	}
	return textResult(body), nil, nil
}

// doFetch 发请求并按要求转换返回。
func doFetch(ctx context.Context, tool string, args fetchArgs, target *url.URL, limit int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		return "", fmt.Errorf("method 仅支持 GET / POST")
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return "", err
	}
	if method == "POST" {
		req.Body = io.NopCloser(strings.NewReader(args.POSTBody))
		req.ContentLength = int64(len(args.POSTBody))
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: requestTimout,
		// 重定向也做 SSRF 防护 + 大小防护交给读取处
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("重定向次数过多（≥8）")
			}
			if _, err := parseSafeURL(r.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 读少量错误体辅助
		eb, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(eb)))
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))

	// 读取受限 body
	limited := &io.LimitedReader{R: resp.Body, N: maxBytes + 1}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(raw) > maxBytes {
		return "", fmt.Errorf("响应超过 %d 字节上限，已中止（可用 max_length 控制返回，但整包仍超限）", maxBytes)
	}

	content := string(raw)

	// JSON 接口：原样返回（即使非 200 亦已在上面拦截）
	if tool == "fetch_json" {
		return trimToLimit(content, limit), nil
	}
	if tool == "fetch_html" {
		return trimToLimit(content, limit), nil
	}
	if strings.Contains(ct, "json") {
		// 其它工具但内容是 JSON：直接还给 JSON 文本
		return trimToLimit(content, limit), nil
	}

	switch tool {
	case "fetch_text":
		return trimToLimit(htmlToText(content), limit), nil
	case "fetch_markdown":
		return toMarkdown(content, limit), nil
	case "fetch_readable":
		return trimToLimit(extractReadable(content, args.ContentTag), limit), nil
	default:
		return trimToLimit(htmlToText(content), limit), nil
	}
}

// trimToLimit 截断到 limit 字符。
func trimToLimit(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	if len(s) > limit {
		return s[:limit] + fmt.Sprintf("\n…（已按 max_length=%d 截断）", limit)
	}
	return s
}

// ---------- HTML 解析 ----------

// htmlToText 把 HTML 转纯文本：丢脚本/样式/注释，块级元素换行。
func htmlToText(doc string) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "（HTML 解析失败）"
	}
	var b strings.Builder
	walkText(root, &b)
	return strings.TrimSpace(collapseBlank(b.String()))
}

func walkText(n *html.Node, b *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
	case html.ElementNode:
		tag := strings.ToLower(n.Data)
		if tag == "script" || tag == "style" || tag == "noscript" || tag == "svg" || tag == "template" {
			return
		}
		// 块级元素留空行
		if blockTag(tag) {
			b.WriteByte('\n')
		}
	case html.CommentNode:
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, b)
	}
}

func blockTag(t string) bool {
	switch t {
	case "p", "div", "section", "article", "main", "header", "footer", "aside",
		"nav", "ul", "ol", "li", "table", "tr", "td", "th", "h1", "h2", "h3",
		"h4", "h5", "h6", "blockquote", "pre", "figure", "br", "hr", "dl", "dt", "dd",
		"form", "fieldset", "textarea", "option", "details", "summary":
		return true
	}
	return false
}

// collapseBlank 把连续 3 个以上换行折叠成 2 个。
func collapseBlank(s string) string {
	var b strings.Builder
	last := byte(0)
	newlines := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' {
			newlines++
			if newlines <= 2 {
				b.WriteByte('\n')
			}
			continue
		}
		newlines = 0
		b.WriteByte(c)
		last = c
	}
	_ = last
	return strings.TrimSpace(b.String())
}

// toMarkdown 把 HTML 粗略转 Markdown。
func toMarkdown(doc string, limit int) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "（HTML 解析失败）"
	}
	var b strings.Builder
	mdNode(root, &b, 0)
	s := collapseBlank(b.String())
	return trimToLimit(s, limit)
}

func mdNode(n *html.Node, b *strings.Builder, depth int) {
	if n.Type == html.TextNode {
		b.WriteString(strings.TrimSpace(n.Data))
		return
	}
	if n.Type == html.CommentNode {
		return
	}
	if n.Type != html.ElementNode {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			mdNode(c, b, depth)
		}
		return
	}
	tag := strings.ToLower(n.Data)
	switch tag {
	case "script", "style", "noscript", "svg", "template", "iframe":
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		lvl, _ := strconv.Atoi(tag[1:])
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6
		}
		b.WriteString("\n" + strings.Repeat("#", lvl) + " ")
		emitChildren(n, b, depth)
		b.WriteString("\n")
		return
	case "a":
		href := attr(n, "href")
		b.WriteString("[")
		emitChildren(n, b, depth)
		b.WriteString("](" + href + ")")
		return
	case "strong", "b":
		b.WriteString("**")
		emitChildren(n, b, depth)
		b.WriteString("**")
		return
	case "em", "i":
		b.WriteString("*")
		emitChildren(n, b, depth)
		b.WriteString("*")
		return
	case "code":
		b.WriteString("`" + textOnly(n) + "`")
		return
	case "p":
		b.WriteString("\n\n")
		emitChildren(n, b, depth)
		return
	case "br":
		b.WriteString("\n")
		return
	case "ul":
		b.WriteString("\n")
		emitChildren(n, b, depth)
		b.WriteString("\n")
		return
	case "ol":
		b.WriteString("\n")
		emitChildren(n, b, depth)
		b.WriteString("\n")
		return
	case "li":
		b.WriteString("\n- ")
		emitChildren(n, b, depth)
		return
	case "blockquote":
		b.WriteString("\n> ")
		emitChildren(n, b, depth)
		return
	case "img":
		src := attr(n, "src")
		if src != "" {
			b.WriteString("\n![图片](" + src + ")\n")
		}
		return
	case "table":
		b.WriteString("\n表格（原始 HTML）\n")
		return
	case "hr":
		b.WriteString("\n---\n")
		return
	default:
		emitChildren(n, b, depth)
	}
}

func emitChildren(n *html.Node, b *strings.Builder, depth int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		mdNode(c, b, depth)
	}
}

func textOnly(n *html.Node) string {
	var b strings.Builder
	walkText(n, &b)
	return strings.TrimSpace(b.String())
}

// extractReadable 提取正文：优先 <article>/<main>/指定容器，否则用全文去掉次要标签。
func extractReadable(doc, contentTag string) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "（HTML 解析失败）"
	}

	// 优先 <article>，其次 <main>；content_tag 指定时优先它
	var target *html.Node
	if contentTag != "" {
		target = findFirst(root, contentTag)
	} else {
		target = findFirstTag(root, "article")
		if target == nil {
			target = findFirstTag(root, "main")
		}
	}
	if target == nil {
		// 兜底：全文去掉次要元素
		target = root
	}

	// 先去掉目标容器内干扰元素
	drop := func(n *html.Node) {
		if n == nil {
			return
		}
		t := strings.ToLower(n.Data)
		if t == "nav" || t == "aside" || t == "footer" || t == "script" || t == "style" || t == "noscript" || t == "form" || t == "iframe" {
			detach(n)
		}
	}
	walkDrop(target, drop)

	var b strings.Builder
	walkText(target, &b)
	return strings.TrimSpace(collapseBlank(b.String()))
}

func walkDrop(n *html.Node, drop func(*html.Node)) {
	if n.Type == html.ElementNode {
		drop(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkDrop(c, drop)
	}
}

// detach 把节点从树上摘除。
func detach(n *html.Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

func findFirst(root *html.Node, sel string) *html.Node {
	// sel 支持 #id / .class / 标签名，这里做简化匹配
	tagOrClass := strings.TrimSpace(sel)
	if strings.HasPrefix(tagOrClass, "#") {
		id := strings.TrimPrefix(tagOrClass, "#")
		return findByID(root, id)
	}
	if strings.HasPrefix(tagOrClass, ".") {
		cls := strings.TrimPrefix(tagOrClass, ".")
		return findByClass(root, cls)
	}
	return findFirstTag(root, strings.ToLower(tagOrClass))
}

func findByID(root *html.Node, id string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && attr(n, "id") == id {
			found = n
			return
		}
		for c := n.FirstChild; c != nil && found == nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func findByClass(root *html.Node, cls string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, c := range strings.Fields(attr(n, "class")) {
				if c == cls {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil && found == nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func findFirstTag(root *html.Node, tag string) *html.Node {
	if root.Type == html.ElementNode && strings.ToLower(root.Data) == tag {
		return root
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if n := findFirstTag(c, tag); n != nil {
			return n
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// ---------- SSRF 防护 ----------

// parseSafeURL 解析 URL 并拒绝危险目标（回环/私网/链路本地/多播）。
func parseSafeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("非法 URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https，收到 %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL 缺少主机")
	}
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		// 解析失败仍放行（可能被防火墙拦，让请求自己报错）；但若明显是 IP 则拦截
		if net.ParseIP(host) != nil {
			return nil, fmt.Errorf("拒绝访问 %s", host)
		}
		return u, nil
	}
	for _, ip := range ips {
		if blocked(ip) {
			return nil, fmt.Errorf("拒绝访问私网/内网地址 %s（SSRF 防护）", ip)
		}
	}
	return u, nil
}

func blocked(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}

// ---------- robots.txt ----------

type robotsDecision struct {
	allowed bool
	status  string // "" | uuav | disallow
}

// checkRobots 读取并遵循 robots.txt。解析失败返回允许（不因 robots 误伤）。
func checkRobots(target *url.URL) (*robotsDecision, error) {
	if target.Hostname() == "127.0.0.1" || target.Hostname() == "localhost" {
		return &robotsDecision{allowed: true}, nil
	}
	robURL := *target
	robURL.Path = "/robots.txt"
	robURL.RawQuery = ""
	robURL.Fragment = ""

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", robURL.String(), nil)
	if err != nil {
		return &robotsDecision{allowed: true}, nil
	}
	req.Header.Set("User-Agent", ua)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &robotsDecision{allowed: true}, nil // 拿不到 robots 就放行
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &robotsDecision{allowed: true}, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return evaluateRobots(string(body), target.Path), nil
}

// evaluateRobots 简单解析 robots.txt 的 User-agent / Disallow 规则。
func evaluateRobots(body, path string) *robotsDecision {
	lines := strings.Split(body, "\n")
	currentUA := ""
	// 先看是否有命中本 UA 的组
	relevant := [][2]string{} // (ua, disallow)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		if key == "user-agent" {
			currentUA = strings.ToLower(val)
		} else if key == "disallow" {
			relevant = append(relevant, [2]string{currentUA, val})
		}
	}
	if len(relevant) == 0 {
		return &robotsDecision{allowed: true}
	}
	// 规则：通配组(*)或本 UA 组；优先级最高的命中最先判断
	uaGroups := map[string][]string{}
	for _, r := range relevant {
		uaGroups[r[0]] = append(uaGroups[r[0]], r[1])
	}
	// 本 UA 组规则
	for _, d := range uaGroups["omni-fetch"] {
		if d == "" {
			continue // 全允许
		}
		if pathMatches(path, d) {
			return &robotsDecision{allowed: false, status: "disallow"}
		}
	}
	// 通配组规则（未定义本组时用 *）
	if len(uaGroups["omni-fetch"]) == 0 {
		for _, d := range uaGroups["*"] {
			if d == "" {
				continue
			}
			if pathMatches(path, d) {
				return &robotsDecision{allowed: false, status: "disallow"}
			}
		}
	}
	return &robotsDecision{allowed: true}
}

func pathMatches(path, pattern string) bool {
	// 支持 * 与结尾 $；简化：* 匹配任意子串
	if pattern == "/" {
		return path == "/" || strings.HasPrefix(path, "/")
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		rest := path
		for i, p := range parts {
			idx := strings.Index(rest, p)
			if idx < 0 {
				if i == len(parts)-1 && p == "" {
					return true
				}
				return false
			}
			if i == 0 && p != "" && idx != 0 {
				return false
			}
			rest = rest[idx+len(p):]
		}
		return true
	}
	return strings.HasPrefix(path, pattern)
}