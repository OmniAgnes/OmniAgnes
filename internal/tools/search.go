package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, ".idea": true, ".vscode": true, "dist": true, "build": true,
	".codeartsdoer": true,
}

func execGrep(ctx context.Context, args map[string]any, env *Env) (string, error) {
	pattern, err := argString(args, "pattern")
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("grep: 无效正则 %q: %v", pattern, err)
	}
	searchPath, _ := args["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}
	abs, err := resolvePath(env.Cwd, searchPath, env.Unsafe)
	if err != nil {
		return "", err
	}
	include := ""
	if v, ok := args["include"].(string); ok {
		include = v
	}
	var includes []string
	if include != "" {
		for _, p := range strings.Split(include, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				includes = append(includes, p)
			}
		}
	}

	maxHits := 200
	outLines := []string{}
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			if path != abs && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(includes) > 0 && !matchAny(includes, d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(env.Cwd, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		f, ferr := os.Open(path)
		if ferr != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			if len(outLines) >= maxHits {
				return errStop
			}
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				outLines = append(outLines, fmt.Sprintf("%s:%d:%s", rel, lineNo, strings.TrimRight(line, "\r")))
			}
		}
		return nil
	})
	if err != nil && err != errStop {
		if err == context.Canceled {
			return "", fmt.Errorf("grep: 已取消")
		}
		return "", fmt.Errorf("grep: %v", err)
	}
	if len(outLines) == 0 {
		return "（无匹配）", nil
	}
	// 按 rel 排序再按行号排序比较麻烦，保持 WalkDir 顺序即可
	sort.Strings(outLines)
	result := strings.Join(outLines, "\n")
	if len(outLines) >= maxHits {
		result += fmt.Sprintf("\n...（已截断，最多 %d 条）", maxHits)
	}
	if env.MaxOutput > 0 && len(result) > env.MaxOutput {
		result = result[:env.MaxOutput] + "\n...（输出截断）"
	}
	return result, nil
}

var errStop = fmt.Errorf("stop")

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

func execGlob(ctx context.Context, args map[string]any, env *Env) (string, error) {
	pattern, err := argString(args, "pattern")
	if err != nil {
		return "", err
	}
	var matches []string
	// 支持 **：转换为递归遍历
	if strings.Contains(pattern, "**") {
		err = filepath.WalkDir(env.Cwd, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && path != env.Cwd && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			rel, relErr := filepath.Rel(env.Cwd, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			ok, mErr := globMatch(pattern, rel)
			if mErr == nil && ok {
				matches = append(matches, rel)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	} else {
		full := pattern
		if !filepath.IsAbs(full) {
			full = filepath.Join(env.Cwd, full)
		}
		mm, gErr := filepath.Glob(full)
		if gErr != nil {
			return "", fmt.Errorf("glob: %v", gErr)
		}
		for _, m := range mm {
			rel, relErr := filepath.Rel(env.Cwd, m)
			if relErr != nil {
				rel = m
			}
			matches = append(matches, filepath.ToSlash(rel))
		}
	}
	sort.Strings(matches)
	if len(matches) > 500 {
		matches = matches[:500]
	}
	if len(matches) == 0 {
		return "（无匹配文件）", nil
	}
	return strings.Join(matches, "\n"), nil
}

// globMatch 兼容 ** 的匹配：**可跨目录，* 不跨目录。
func globMatch(pattern, name string) (bool, error) {
	re, err := globToRegex(pattern)
	if err != nil {
		// 匹配失败时回退到 path.Match
		return filepath.Match(pattern, name)
	}
	return re.MatchString(name), nil
}

// globToRegex 将含 ** 的 glob 转为正则。
func globToRegex(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(pattern) {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				for i < len(pattern) && pattern[i] == '*' {
					i++
				}
				if i < len(pattern) && (pattern[i] == '/' || pattern[i] == '\\') {
					i++
				}
				b.WriteString("(.*/)?")
				continue
			}
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		case '.':
			b.WriteString("\\.")
			i++
		case '/', '\\':
			b.WriteString("/")
			i++
		default:
			if strings.ContainsRune("\\^$+()[]{}|", rune(c)) {
				b.WriteString("\\")
			}
			b.WriteByte(c)
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
