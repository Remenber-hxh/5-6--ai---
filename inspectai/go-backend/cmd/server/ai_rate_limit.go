package main

import (
	"net/http"
	"sync"
	"time"
)

// ===== AI 接口限流 =====
//
// 登录和注册早就有限流,调模型的接口一个都没有。视觉走的是开了"仅免费额度"的
// DashScope 账户,问答走预充值的 DeepSeek —— 一个脚本、一个卡住的前端重试循环,
// 都能在几分钟里把额度烧光,而额度一光,所有人的拍照识别一起停。
//
// 【按人限,不按接口总量限】一个人刷不该让别人等。上限按正常使用的好几倍给:
// 现场一个人一分钟拍不了 30 次照,主管一分钟也问不了 20 个问题 —— 超过就不是人在用。

const aiRateWindow = time.Minute

// 每个 AI 功能每人每分钟的上限
const (
	aiLimitManagementChat = 20
	aiLimitManagementGen  = 10 // 报告、重点关注摘要的重新生成
	aiLimitClassify       = 30
	aiLimitAnalyze        = 30
	aiLimitDraftFields    = 10
)

type aiRateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

// allow 这一次放不放行。放行时顺手记一笔。
func (l *aiRateLimiter) allow(key string, limit int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cutoff := now.Add(-window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	// 偶尔扫一遍,别让早就不来的人一直占着内存
	if len(l.hits) > 1000 {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// allowAICall 超限时直接写 429 并返回 false。
func (s *Server) allowAICall(w http.ResponseWriter, r *http.Request, bucket string, limit int) bool {
	who := s.currentUserID(r)
	if who == "" {
		who = loginGuardKey("", r) // 没登录身份(静态 token)时按来源地址
	}
	if s.aiLimiter.allow(bucket+"|"+who, limit, aiRateWindow, time.Now()) {
		return true
	}
	writeError(w, http.StatusTooManyRequests, "ai_rate_limited", "操作太频繁,请稍等一分钟再试")
	return false
}
