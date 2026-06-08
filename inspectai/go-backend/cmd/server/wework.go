package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WeWorkConfig struct {
	BaseURL   string
	CorpID    string
	AgentID   string
	AppSecret string
}

type WeWorkClient struct {
	baseURL   string
	corpID    string
	agentID   int
	appSecret string
	http      *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

type WeWorkSendResult struct {
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
	InvalidUser  string `json:"invaliduser,omitempty"`
	InvalidParty string `json:"invalidparty,omitempty"`
	InvalidTag   string `json:"invalidtag,omitempty"`
	MsgID        string `json:"msgid,omitempty"`
	ResponseCode string `json:"response_code,omitempty"`
}

type weWorkTokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func NewDisabledWeWorkClient() *WeWorkClient {
	return &WeWorkClient{http: &http.Client{Timeout: 10 * time.Second}}
}

func NewWeWorkClient(cfg WeWorkConfig) (*WeWorkClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	agentID := strings.TrimSpace(cfg.AgentID)
	parsedAgentID := 0
	if agentID != "" {
		n, err := strconv.Atoi(agentID)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("WEWORK_AGENT_ID 必须是正整数")
		}
		parsedAgentID = n
	}
	return &WeWorkClient{
		baseURL:   baseURL,
		corpID:    strings.TrimSpace(cfg.CorpID),
		agentID:   parsedAgentID,
		appSecret: strings.TrimSpace(cfg.AppSecret),
		http:      &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *WeWorkClient) Enabled() bool {
	return c != nil && c.corpID != "" && c.agentID > 0 && c.appSecret != ""
}

func (c *WeWorkClient) SendText(ctx context.Context, toUsers []string, content string) (*WeWorkSendResult, error) {
	if !c.Enabled() {
		return nil, errors.New("企业微信未配置：需要 WEWORK_CORP_ID / WEWORK_AGENT_ID / WEWORK_APP_SECRET")
	}
	toUsers = normalizeWeWorkIDs(toUsers)
	if len(toUsers) == 0 {
		return nil, errors.New("缺少企业微信 UserID")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("消息内容不能为空")
	}
	if len([]rune(content)) > 1800 {
		return nil, errors.New("消息内容过长，请控制在 1800 字以内")
	}

	token, err := c.accessTokenFor(ctx, false)
	if err != nil {
		return nil, err
	}
	result, err := c.sendTextWithToken(ctx, token, toUsers, content)
	if err != nil {
		return nil, err
	}
	if result.ErrCode == 40014 || result.ErrCode == 42001 {
		token, err = c.accessTokenFor(ctx, true)
		if err != nil {
			return nil, err
		}
		result, err = c.sendTextWithToken(ctx, token, toUsers, content)
	}
	if err != nil {
		return nil, err
	}
	if result.ErrCode != 0 {
		return result, fmt.Errorf("企业微信发送失败: errcode=%d errmsg=%s", result.ErrCode, result.ErrMsg)
	}
	return result, nil
}

func (c *WeWorkClient) accessTokenFor(ctx context.Context, forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !forceRefresh && c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	endpoint := c.baseURL + "/cgi-bin/gettoken?corpid=" + url.QueryEscape(c.corpID) + "&corpsecret=" + url.QueryEscape(c.appSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var data weWorkTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("企业微信 token 请求失败: status=%d", resp.StatusCode)
	}
	if data.ErrCode != 0 {
		return "", fmt.Errorf("企业微信 token 获取失败: errcode=%d errmsg=%s", data.ErrCode, data.ErrMsg)
	}
	if strings.TrimSpace(data.AccessToken) == "" {
		return "", errors.New("企业微信 token 响应为空")
	}
	ttl := time.Duration(data.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if ttl > 5*time.Minute {
		ttl -= 2 * time.Minute
	}
	c.accessToken = data.AccessToken
	c.tokenExpiry = time.Now().Add(ttl)
	return c.accessToken, nil
}

func (c *WeWorkClient) sendTextWithToken(ctx context.Context, token string, toUsers []string, content string) (*WeWorkSendResult, error) {
	payload := map[string]any{
		"touser":                   strings.Join(toUsers, "|"),
		"msgtype":                  "text",
		"agentid":                  c.agentID,
		"text":                     map[string]string{"content": content},
		"safe":                     0,
		"enable_duplicate_check":   1,
		"duplicate_check_interval": 600,
	}
	body, _ := json.Marshal(payload)
	endpoint := c.baseURL + "/cgi-bin/message/send?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result WeWorkSendResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &result, fmt.Errorf("企业微信发送请求失败: status=%d", resp.StatusCode)
	}
	return &result, nil
}

func normalizeWeWorkIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
