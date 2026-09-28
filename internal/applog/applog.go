// Package applog is a tiny file logger for diagnostics (no secrets inside).
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	path string
)

func Init(appDir string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(appDir, "skillsmcp.log")
	_ = os.MkdirAll(appDir, 0o700)
}

func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return path
}

func appendLine(level, msg string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if path == "" {
		return
	}
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, fmt.Sprintf(msg, args...))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func Info(msg string, args ...any)  { appendLine("INFO", msg, args...) }
func Error(op string, err error) {
	if err == nil {
		return
	}
	appendLine("ERROR", "%s: %v", op, err)
}

func Tail(n int) []string {
	mu.Lock()
	p := path
	mu.Unlock()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return []string{"no log yet"}
	}
	lines := splitLines(string(b))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func Clear() error {
	mu.Lock()
	p := path
	mu.Unlock()
	if p == "" {
		return nil
	}
	return os.WriteFile(p, nil, 0o600)
}
