package main

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ===== 登录防爆破:同一「用户名+来源IP」连续失败即锁定 =====

const (
	loginMaxFails   = 5                // 连续失败次数上限
	loginLockWindow = 10 * time.Minute // 锁定时长
	loginFailExpiry = 30 * time.Minute // 失败计数的自然过期
)

type loginFailState struct {
	count       int
	lockedUntil time.Time
	lastFail    time.Time
}

type loginGuard struct {
	mu    sync.Mutex
	fails map[string]*loginFailState
}

func newLoginGuard() *loginGuard {
	return &loginGuard{fails: map[string]*loginFailState{}}
}

func loginGuardKey(username string, r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// 经 nginx 反代时以 X-Forwarded-For 首个地址为准
	if fwd := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); fwd != "" {
		host = fwd
	}
	return strings.ToLower(strings.TrimSpace(username)) + "|" + host
}

// locked 返回是否处于锁定期,以及剩余秒数。
func (g *loginGuard) locked(key string) (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.fails[key]
	if !ok {
		return false, 0
	}
	if time.Now().Before(st.lockedUntil) {
		return true, int(time.Until(st.lockedUntil).Seconds()) + 1
	}
	return false, 0
}

// fail 记录一次失败,达到上限则进入锁定;返回是否刚触发锁定。
func (g *loginGuard) fail(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepLocked()
	st := g.fails[key]
	if st == nil {
		st = &loginFailState{}
		g.fails[key] = st
	}
	// 过期的旧计数重置
	if time.Since(st.lastFail) > loginFailExpiry {
		st.count = 0
	}
	st.count++
	st.lastFail = time.Now()
	if st.count >= loginMaxFails {
		st.lockedUntil = time.Now().Add(loginLockWindow)
		st.count = 0
		return true
	}
	return false
}

// success 清除该 key 的失败记录。
func (g *loginGuard) success(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, key)
}

// sweepLocked 顺手清理长时间不活跃的条目(调用方须已持锁)。
func (g *loginGuard) sweepLocked() {
	if len(g.fails) < 1024 {
		return
	}
	now := time.Now()
	for k, st := range g.fails {
		if now.After(st.lockedUntil) && now.Sub(st.lastFail) > loginFailExpiry {
			delete(g.fails, k)
		}
	}
}

// ===== AI 识别并发上限 =====

// aiConcurrencyFromEnv 读取 INSPECTAI_AI_CONCURRENCY(默认 3,范围 1-16)。
// 免费档模型限流严格,并发过高整体反而更慢。
func aiConcurrencyFromEnv() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("INSPECTAI_AI_CONCURRENCY")))
	if err != nil || n < 1 {
		return 3
	}
	if n > 16 {
		return 16
	}
	return n
}
