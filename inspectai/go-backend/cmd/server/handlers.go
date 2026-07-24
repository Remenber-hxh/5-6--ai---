package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===== 路由 =====

func (s *Server) router(w http.ResponseWriter, r *http.Request) {
	corsAllowed := s.applyCORS(w, r)
	if r.Method == http.MethodOptions {
		if !corsAllowed {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "缺少或无效的访问令牌")
		return
	}
	s.setAuthCookieIfNeeded(w, r)

	// 精确匹配走路由权限表(routes.go):先表层准入,再进 handler
	for _, rt := range apiRoutes {
		if r.URL.Path == rt.path && r.Method == rt.method {
			if !s.allow(w, r, rt) {
				return
			}
			rt.handle(s, w, r)
			return
		}
	}

	// 前缀/动态路由:按方法在各自 handleXxxRoutes 内部分权
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/users/"):
		s.handleUserRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/roles/"):
		s.handleRoleRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/engineering/tasks/"):
		s.handleEngineeringTaskRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/assets/"):
		s.handleAssetRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/inspection/records/"):
		s.handleRecordRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/ai/tasks/") && r.Method == http.MethodGet:
		s.handleGetTask(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/change-requests/"):
		s.handleChangeRequestRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/prompt/templates/"):
		s.handlePromptTemplateRoutes(w, r)
	default:
		s.serveStatic(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	if r.URL.Path == "/health" {
		return true
	}
	if r.URL.Path == "/api/auth/login" {
		return true
	}
	protected := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/storage/")
	if !protected {
		return true
	}
	if _, ok := s.roleForToken(s.tokenFromRequest(r)); ok {
		return true
	}
	if _, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		return true
	}
	return s.localNoAuthAllowed(r)
}

const authCookieName = "inspectai_token"

func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if origin == "" {
		return true
	}
	if !s.corsAllowedOrigins[origin] {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Idempotency-Key,Authorization,X-User-Role,X-User-Name,X-InspectAI-Token")
	return true
}

func (s *Server) tokenFromRequest(r *http.Request) string {
	if token := s.explicitTokenFromRequest(r); token != "" {
		return token
	}
	if cookie, err := r.Cookie(authCookieName); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func (s *Server) explicitTokenFromRequest(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get("X-InspectAI-Token")); token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return ""
}

func (s *Server) roleForToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	if s.supervisorToken != "" && secureCompare(token, s.supervisorToken) {
		return roleSupervisor, true
	}
	if s.authToken != "" && secureCompare(token, s.authToken) {
		return roleInspector, true
	}
	return "", false
}

func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) localNoAuthAllowed(r *http.Request) bool {
	if s.authToken != "" || s.supervisorToken != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	if ip == nil || !ip.IsPrivate() {
		return false
	}
	forwardedFor := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(forwardedFor) == 0 {
		return false
	}
	clientIP := net.ParseIP(strings.TrimSpace(forwardedFor[0]))
	return clientIP != nil && clientIP.IsLoopback()
}

func (s *Server) setAuthCookieIfNeeded(w http.ResponseWriter, r *http.Request) {
	token := s.explicitTokenFromRequest(r)
	if _, ok := s.roleForToken(token); !ok {
		if _, sessionOK := s.userFromSessionToken(token); !sessionOK {
			return
		}
	}
	s.setAuthCookie(w, r, token, 8*60*60)
}

func (s *Server) setAuthCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	if strings.TrimSpace(token) == "" && maxAge > 0 {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

// ===== 角色辅助（一期不接企微，直接信任 header） =====
const (
	roleInspector  = "inspector"
	roleSupervisor = "supervisor"
	roleManager    = "manager"
	roleAdmin      = "admin"
)

func (s *Server) userRole(r *http.Request) string {
	if role, ok := s.roleForToken(s.tokenFromRequest(r)); ok {
		return role
	}
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		return user.RoleCode
	}
	if s.localNoAuthAllowed(r) {
		role := strings.TrimSpace(r.Header.Get("X-User-Role"))
		if role == roleSupervisor {
			return roleSupervisor
		}
	}
	return roleInspector
}

func (s *Server) userFromSessionToken(token string) (*User, bool) {
	user, err := s.store.GetUserBySession(token)
	if err != nil {
		return nil, false
	}
	return user, true
}

// tenantForRequest 解析当前请求所属租户,给 Store 层过滤用(Phase 0 第 4 步接入)。
// 与 userRole/currentUserName 一致:按需从会话解析,不改中间件。
// 无会话(本地免鉴权 / header 兜底)或用户未绑定租户时,回落默认租户 = 单租户安全行为。
func (s *Server) tenantForRequest(r *http.Request) string {
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		if t := strings.TrimSpace(user.TenantID); t != "" {
			return t
		}
	}
	return defaultTenantID
}

func (s *Server) currentUserName(r *http.Request) string {
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		if strings.TrimSpace(user.DisplayName) != "" {
			return user.DisplayName
		}
		return user.Username
	}
	return userName(r)
}

func (s *Server) hasSupervisorAccess(r *http.Request) bool {
	role := s.userRole(r)
	switch role {
	case roleAdmin, roleManager, roleSupervisor:
		return true
	}
	// 自定义角色:权限矩阵授予过任一能力即视为管理角色
	return s.permCache.anyAllowed(role)
}

func (s *Server) requireSupervisorAccess(w http.ResponseWriter, r *http.Request) bool {
	if s.hasSupervisorAccess(r) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden", "仅管理角色可访问该接口")
	return false
}

func recordOwnedBy(rec *Record, inspectorUserID, displayName, username string) bool {
	if rec == nil {
		return false
	}
	if ownerID := strings.TrimSpace(rec.InspectorUserID); ownerID != "" {
		return strings.TrimSpace(inspectorUserID) != "" && ownerID == strings.TrimSpace(inspectorUserID)
	}
	owner := strings.TrimSpace(rec.Inspector)
	return owner != "" && (owner == strings.TrimSpace(displayName) || owner == strings.TrimSpace(username))
}

func (s *Server) canAccessRecord(r *http.Request, rec *Record, write bool) bool {
	role := s.userRole(r)
	if role == roleAdmin || (!write && (role == roleManager || role == roleSupervisor)) {
		return true
	}
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		return recordOwnedBy(rec, user.ID, user.DisplayName, user.Username)
	}
	if s.localNoAuthAllowed(r) {
		return recordOwnedBy(rec, "", userName(r), "")
	}
	return false
}

func (s *Server) requireRecordAccess(w http.ResponseWriter, r *http.Request, recordID string, write bool) (*Record, bool) {
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return nil, false
	}
	if !s.canAccessRecord(r, rec, write) {
		writeError(w, http.StatusForbidden, "forbidden", "无权访问该巡检记录")
		return nil, false
	}
	return rec, true
}

func userName(r *http.Request) string {
	n := strings.TrimSpace(r.Header.Get("X-User-Name"))
	if n == "" {
		return "匿名"
	}
	// 前端用 encodeURIComponent 编码非 ASCII；解码失败时按原样返回。
	if dec, err := url.QueryUnescape(n); err == nil {
		dec = strings.TrimSpace(dec)
		if dec != "" {
			return dec
		}
	}
	return n
}

// ===== handlers =====

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"service":      "go-backend",
		"aiServiceUrl": s.aiClient.baseURL,
		"storeKind":    s.storeKind,
		"wework":       s.wework != nil && s.wework.Enabled(),
		"weworkBot":    s.weworkBot != nil && s.weworkBot.Enabled(),
		"time":         time.Now(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// 防爆破:同一用户名+IP 连续失败 5 次锁 10 分钟
	guardKey := loginGuardKey(req.Username, r)
	if locked, retryAfter := s.loginGuard.locked(guardKey); locked {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeError(w, http.StatusTooManyRequests, "login_locked",
			fmt.Sprintf("失败次数过多,请 %d 分钟后再试", (retryAfter+59)/60))
		return
	}
	user, session, err := s.store.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		status := http.StatusInternalServerError
		code := "login_failed"
		msg := err.Error()
		if errors.Is(err, errInvalidCredentials) {
			status = http.StatusUnauthorized
			code = "invalid_credentials"
			msg = "账号或密码错误"
			if s.loginGuard.fail(guardKey) {
				_ = s.store.CreateOperationLog(&OperationLog{
					ActorName:  req.Username,
					Action:     "login_locked",
					TargetType: "user",
					Detail:     map[string]any{"username": req.Username, "reason": "连续失败触发锁定"},
				})
			}
		}
		writeError(w, status, code, msg)
		return
	}
	s.loginGuard.success(guardKey)
	s.setAuthCookie(w, r, session.Token, int(sessionTTL.Seconds()))
	_ = s.store.CreateOperationLog(&OperationLog{
		UserID:     user.ID,
		ActorName:  user.DisplayName,
		Action:     "login",
		TargetType: "user",
		TargetID:   user.ID,
		Detail:     map[string]any{"username": user.Username, "role": user.RoleCode},
	})
	resp := map[string]any{
		"user":      user,
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
		"perms":     s.permsForRole(user.RoleCode),
	}
	// 默认密码仍在使用 → 前端强提示修改
	if req.Password == defaultAdminPass {
		resp["mustChangePassword"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		writeJSON(w, http.StatusOK, map[string]any{"user": user, "perms": s.permsForRole(user.RoleCode)})
		return
	}
	role := s.userRole(r)
	name := s.currentUserName(r)
	roleName := map[string]string{
		roleAdmin:      "系统管理员",
		roleManager:    "管理人员",
		roleSupervisor: "复核审批人员",
		roleInspector:  "一线巡检员",
	}[role]
	if roleName == "" {
		roleName = role
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": &User{
			ID:          "local_" + role,
			Username:    role,
			DisplayName: name,
			RoleCode:    role,
			RoleName:    roleName,
			Status:      "local",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := s.tokenFromRequest(r)
	if user, ok := s.userFromSessionToken(token); ok {
		_ = s.store.CreateOperationLog(&OperationLog{
			UserID:     user.ID,
			ActorName:  user.DisplayName,
			Action:     "logout",
			TargetType: "user",
			TargetID:   user.ID,
			Detail:     map[string]any{"username": user.Username},
		})
	}
	_ = s.store.DeleteSession(token)
	s.setAuthCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "需要管理权限")
		return
	}
	users, err := s.store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_users_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleSendWeWorkMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "wework_send") {
		return
	}
	if s.wework == nil || !s.wework.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "wework_disabled", "企业微信未配置或未启用")
		return
	}
	var req struct {
		UserIDs       []string `json:"userIds"`
		WeWorkUserIDs []string `json:"weworkUserIds"`
		Content       string   `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "empty_content", "消息内容不能为空")
		return
	}
	targets, missing, err := s.resolveWeWorkTargets(req.UserIDs, req.WeWorkUserIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve_targets_failed", err.Error())
		return
	}
	if len(targets) == 0 {
		msg := "没有可发送的企业微信 UserID"
		if len(missing) > 0 {
			msg += "，未绑定用户：" + strings.Join(missing, "、")
		}
		writeError(w, http.StatusBadRequest, "empty_targets", msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := s.wework.SendText(ctx, targets, content)
	if err != nil {
		status := http.StatusBadGateway
		if result == nil {
			result = &WeWorkSendResult{}
		}
		writeError(w, status, "wework_send_failed", err.Error())
		return
	}
	s.recordOperation(r, "wework.message.send", "wework", strings.Join(targets, "|"), map[string]any{
		"targetCount": len(targets),
		"missing":     missing,
		"invalidUser": result.InvalidUser,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"targets": targets,
		"missing": missing,
		"result":  result,
	})
}

func (s *Server) handleSendWeWorkGroupMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "wework_send") {
		return
	}
	if s.weworkBot == nil || !s.weworkBot.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "wework_bot_disabled", "企业微信群机器人 Webhook 未配置或未启用")
		return
	}
	var req struct {
		MsgType             string   `json:"msgtype"`
		Content             string   `json:"content"`
		MentionedList       []string `json:"mentionedList"`
		MentionedMobileList []string `json:"mentionedMobileList"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	msgType := strings.ToLower(strings.TrimSpace(req.MsgType))
	if msgType == "" {
		msgType = "text"
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "empty_content", "消息内容不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var (
		result *WeWorkBotSendResult
		err    error
	)
	switch msgType {
	case "text":
		result, err = s.weworkBot.SendText(ctx, content, req.MentionedList, req.MentionedMobileList)
	case "markdown":
		result, err = s.weworkBot.SendMarkdown(ctx, content)
	default:
		writeError(w, http.StatusBadRequest, "bad_msgtype", "msgtype 仅支持 text / markdown")
		return
	}
	if err != nil {
		if result == nil {
			result = &WeWorkBotSendResult{}
		}
		writeError(w, http.StatusBadGateway, "wework_bot_send_failed", err.Error())
		return
	}
	s.recordOperation(r, "wework.bot_message.send", "wework_bot", msgType, map[string]any{
		"msgtype":             msgType,
		"mentionedList":       normalizeWeWorkIDs(req.MentionedList),
		"mentionedMobileList": normalizeWeWorkIDs(req.MentionedMobileList),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"msgtype": msgType,
		"result":  result,
	})
}

func (s *Server) resolveWeWorkTargets(userIDs, weworkUserIDs []string) ([]string, []string, error) {
	targets := normalizeWeWorkIDs(weworkUserIDs)
	missing := []string{}
	seen := map[string]bool{}
	for _, id := range targets {
		seen[id] = true
	}
	for _, userID := range normalizeWeWorkIDs(userIDs) {
		user, err := s.store.GetUser(userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				missing = append(missing, userID+"(用户不存在)")
				continue
			}
			return nil, nil, err
		}
		weworkID := strings.TrimSpace(user.WeworkUserID)
		if weworkID == "" {
			name := strings.TrimSpace(user.DisplayName)
			if name == "" {
				name = user.Username
			}
			missing = append(missing, name+"(未绑定企业微信 UserID)")
			continue
		}
		if !seen[weworkID] {
			targets = append(targets, weworkID)
			seen[weworkID] = true
		}
	}
	return targets, missing, nil
}

// hasAdminAccess 仅 admin 角色（用户管理操作）。
// hasSupervisorAccess 更宽松（admin/manager/supervisor 都算）。
func (s *Server) hasAdminAccess(r *http.Request) bool {
	return s.userRole(r) == roleAdmin
}

type userUpsertRequest struct {
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	Phone        string `json:"phone"`
	Avatar       string `json:"avatar"`
	RoleCode     string `json:"roleCode"`
	DepartmentID string `json:"departmentId"`
	WeworkUserID string `json:"weworkUserId"`
	Password     string `json:"password"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可新建账号")
		return
	}
	var req userUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.DisplayName == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "账号 / 姓名 / 初始密码 都不能为空")
		return
	}
	if _, ok, _ := s.store.GetRoleByCode(req.RoleCode); !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "角色无效")
		return
	}
	user := &User{
		Username:     req.Username,
		DisplayName:  req.DisplayName,
		Phone:        strings.TrimSpace(req.Phone),
		Avatar:       strings.TrimSpace(req.Avatar),
		RoleCode:     req.RoleCode,
		DepartmentID: strings.TrimSpace(req.DepartmentID),
		WeworkUserID: strings.TrimSpace(req.WeworkUserID),
		Status:       "active",
	}
	if err := s.store.CreateUser(user, req.Password); err != nil {
		status := http.StatusInternalServerError
		code := "create_user_failed"
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
			code = "username_exists"
		}
		writeError(w, status, code, err.Error())
		return
	}
	s.recordOperation(r, "user.create", "user", user.ID, map[string]any{
		"username": user.Username,
		"role":     user.RoleCode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

// handleUserRoutes 处理 /api/users/<id>[/password|/status]
func (s *Server) handleUserRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理账号")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/users/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "缺少用户 ID")
		return
	}
	userID := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch {
	case sub == "" && r.Method == http.MethodPut:
		s.handleUpdateUser(w, r, userID)
	case sub == "password" && r.Method == http.MethodPost:
		s.handleResetUserPassword(w, r, userID)
	case sub == "status" && r.Method == http.MethodPost:
		s.handleSetUserStatus(w, r, userID)
	default:
		writeError(w, http.StatusNotFound, "not_found", "未匹配的用户路由")
	}
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, userID string) {
	var req userUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	reqRole, reqRoleOK, _ := s.store.GetRoleByCode(req.RoleCode)
	if req.RoleCode != "" && !reqRoleOK {
		writeError(w, http.StatusBadRequest, "bad_request", "角色无效")
		return
	}
	err := s.store.UpdateUserProfile(userID, func(u *User) {
		if v := strings.TrimSpace(req.DisplayName); v != "" {
			u.DisplayName = v
		}
		if req.Phone != "" {
			u.Phone = strings.TrimSpace(req.Phone)
		}
		if req.Avatar != "" {
			u.Avatar = strings.TrimSpace(req.Avatar)
		}
		if req.RoleCode != "" {
			u.RoleCode = req.RoleCode
			if reqRole != nil {
				u.RoleID = reqRole.ID
			}
		}
		if req.DepartmentID != "" {
			u.DepartmentID = strings.TrimSpace(req.DepartmentID)
		}
		if req.WeworkUserID != "" {
			u.WeworkUserID = strings.TrimSpace(req.WeworkUserID)
		}
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "update_user_failed", err.Error())
		return
	}
	user, _ := s.store.GetUser(userID)
	s.recordOperation(r, "user.update", "user", userID, map[string]any{
		"role": req.RoleCode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Password = strings.TrimSpace(req.Password)
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "bad_request", "密码长度至少 6 位")
		return
	}
	if err := s.store.SetUserPassword(userID, req.Password); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "reset_password_failed", err.Error())
		return
	}
	s.recordOperation(r, "user.reset_password", "user", userID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetUserStatus(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if req.Status != "active" && req.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "bad_request", "status 必须是 active / disabled")
		return
	}
	// 安全护栏：不允许停用自己 / 不允许停用最后一个 admin
	if currentUser, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok && currentUser.ID == userID && req.Status == "disabled" {
		writeError(w, http.StatusBadRequest, "self_disable", "不能停用当前登录账号")
		return
	}
	if req.Status == "disabled" {
		if target, err := s.store.GetUser(userID); err == nil && target.RoleCode == roleAdmin {
			users, _ := s.store.ListUsers()
			activeAdmins := 0
			for _, u := range users {
				if u.RoleCode == roleAdmin && u.Status == "active" {
					activeAdmins++
				}
			}
			if activeAdmins <= 1 {
				writeError(w, http.StatusBadRequest, "last_admin", "至少需要 1 个启用状态的系统管理员")
				return
			}
		}
	}
	if err := s.store.SetUserStatus(userID, req.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "set_status_failed", err.Error())
		return
	}
	s.recordOperation(r, "user.set_status", "user", userID, map[string]any{"status": req.Status})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": req.Status})
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "需要管理权限")
		return
	}
	roles, err := s.store.ListRoles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_roles_failed", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		out = append(out, map[string]any{
			"id": role.ID, "code": role.Code, "name": role.Name,
			"description": role.Description, "builtin": builtinRoleCode(role.Code),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": out})
}

func (s *Server) handleListDepartments(w http.ResponseWriter, r *http.Request) {
	if !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "需要管理权限")
		return
	}
	depts, err := s.store.ListDepartments()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_departments_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"departments": depts})
}

func (s *Server) handleListOperationLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "audit_view") {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := s.store.ListOperationLogs(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_operation_logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) recordOperation(r *http.Request, action, targetType, targetID string, detail map[string]any) {
	item := &OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
	}
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		item.UserID = user.ID
	}
	_ = s.store.CreateOperationLog(item)
}

func (s *Server) handleListPoints(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"points": seedPoints()})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"templates": reportTemplates()})
}

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.loadAssetsForDisplay(s.tenantForRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_assets_failed", err.Error())
		return
	}
	filtered := filterAssetsForDisplay(assets, r.URL.Query())
	writeJSON(w, http.StatusOK, map[string]any{
		"assets":       s.sanitizeAssetsForRequest(r, filtered),
		"summary":      buildAssetListSummary(filtered),
		"totalSummary": buildAssetListSummary(assets),
	})
}

func (s *Server) handleAssetSummary(w http.ResponseWriter, r *http.Request) {
	assets, err := s.loadAssetsForDisplay(s.tenantForRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_summary_failed", err.Error())
		return
	}
	filtered := filterAssetsForDisplay(assets, r.URL.Query())
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": buildAssetListSummary(filtered),
	})
}

func (s *Server) loadAssetsForDisplay(tenantID string) ([]*AssetEntry, error) {
	assets, err := s.store.ListAssets(tenantID)
	if err != nil {
		return nil, err
	}
	visible := make([]*AssetEntry, 0, len(assets))
	for _, a := range assets {
		s.enrichAssetForDisplay(a)
		if isLegacyZihanAggregateAsset(a) {
			continue
		}
		visible = append(visible, a)
	}
	return visible, nil
}

func (s *Server) ensureAssetLedgerFromRecords() error {
	// 启动时台账重建,无请求上下文;单租户过渡期按默认租户。
	records, err := s.store.ListRecords(defaultTenantID, 500)
	if err != nil {
		return err
	}
	latestByAssetID := map[string]*AssetEntry{}
	for _, rec := range records {
		if rec == nil || !rec.Submitted {
			continue
		}
		for _, asset := range buildAssets(rec, assetLedgerTime(rec)) {
			if _, exists := latestByAssetID[asset.ID]; exists {
				continue
			}
			latestByAssetID[asset.ID] = asset
		}
	}
	for id, asset := range latestByAssetID {
		// 启动时台账重建,无请求上下文;单租户过渡期按默认租户查存在性。
		if existing, err := s.store.GetAsset(defaultTenantID, id); err == nil && existing != nil {
			continue
		}
		if err := s.store.UpsertAsset(asset); err != nil {
			return fmt.Errorf("backfill asset %s: %w", id, err)
		}
	}
	return nil
}

func (s *Server) enrichAssetForDisplay(a *AssetEntry) {
	if a == nil {
		return
	}
	if a.ProjectCode == "" || a.TemplateID == "" || a.AssetKey == "" {
		a.ProjectCode, a.TemplateID, a.AssetKey = deriveAssetDisplayKeys(a)
	}
	if a.StatusLevel == "" {
		a.StatusLevel = statusLevel(a.LastStatus)
	}
	if a.StatusOrder == 0 {
		a.StatusOrder = statusOrder(a.LastStatus)
	}
	if a.CoverImagePath != "" {
		img := ImageInfo{
			ID:       "asset_cover_" + sanitizeAssetIdent(a.ID),
			FileName: filepath.Base(a.CoverImagePath),
			Path:     a.CoverImagePath,
		}
		a.CoverImage = &img
	}
	if a.LastRecordID == "" {
		return
	}
	rec, err := s.store.GetRecord(a.TenantID, a.LastRecordID)
	if err != nil || rec == nil {
		return
	}
	if a.LastInspector == "" {
		a.LastInspector = rec.Inspector
	}
	if len(rec.Images) > 0 {
		img := rec.Images[0]
		if a.CoverImage == nil {
			a.CoverImage = &img
		}
		if a.LastPhotoPath == "" {
			a.LastPhotoPath = img.Path
		}
	}
}

func (s *Server) sanitizeAssetsForRequest(r *http.Request, assets []*AssetEntry) []*AssetEntry {
	out := make([]*AssetEntry, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		clean := *asset
		if clean.LastRecordID != "" {
			if rec, err := s.store.GetRecord(s.tenantForRequest(r), clean.LastRecordID); err == nil && !s.canAccessRecord(r, rec, false) {
				if clean.CoverImagePath == "" {
					clean.CoverImage = nil
				}
				clean.LastPhotoPath = ""
			}
		}
		out = append(out, &clean)
	}
	return out
}

func filterAssetsForDisplay(assets []*AssetEntry, q url.Values) []*AssetEntry {
	project := strings.TrimSpace(q.Get("project"))
	assetType := strings.TrimSpace(q.Get("assetType"))
	status := strings.TrimSpace(q.Get("status"))
	level := strings.TrimSpace(q.Get("level"))
	pointID := strings.TrimSpace(q.Get("pointId"))
	templateID := strings.TrimSpace(q.Get("templateId"))
	keyword := strings.ToLower(strings.TrimSpace(q.Get("q")))

	out := make([]*AssetEntry, 0, len(assets))
	for _, a := range assets {
		if project != "" && a.Project != project && a.ProjectCode != project {
			continue
		}
		if assetType != "" && a.AssetType != assetType {
			continue
		}
		if status != "" && a.LastStatus != status {
			continue
		}
		if level != "" && a.StatusLevel != level {
			continue
		}
		if pointID != "" && a.PointID != pointID {
			continue
		}
		if templateID != "" && a.TemplateID != templateID {
			continue
		}
		if keyword != "" && !assetMatchesKeyword(a, keyword) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func assetMatchesKeyword(a *AssetEntry, keyword string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		a.ID,
		a.Project,
		a.ProjectCode,
		a.PointID,
		a.TemplateID,
		a.AssetType,
		a.AssetKey,
		a.AssetName,
		a.LastStatus,
		a.LastInspector,
		a.LastSummary,
	}, " "))
	return strings.Contains(haystack, keyword)
}

func buildAssetListSummary(assets []*AssetEntry) AssetListSummary {
	summary := AssetListSummary{
		ByStatus:    map[string]int{},
		ByProject:   []AssetGroupSummary{},
		ByAssetType: []AssetGroupSummary{},
	}
	projectGroups := map[string]*AssetGroupSummary{}
	typeGroups := map[string]*AssetGroupSummary{}
	recentCutoff := time.Now().Add(-24 * time.Hour)

	for _, a := range assets {
		if a == nil {
			continue
		}
		summary.Total++
		status := firstNonEmpty(a.LastStatus, "未巡检")
		level := firstNonEmpty(a.StatusLevel, statusLevel(status))
		summary.ByStatus[status]++
		switch level {
		case "normal":
			summary.Normal++
		case "warning":
			summary.Warning++
		case "danger":
			summary.Danger++
		case "repair":
			summary.Repair++
		default:
			summary.Unknown++
		}
		if !a.UpdatedAt.IsZero() && a.UpdatedAt.After(recentCutoff) {
			summary.RecentlyUpdated++
		}
		addAssetGroup(projectGroups, firstNonEmpty(a.ProjectCode, a.Project), firstNonEmpty(a.Project, "未分类项目"), status)
		addAssetGroup(typeGroups, firstNonEmpty(a.AssetType, "unknown"), firstNonEmpty(a.AssetType, "未分类资产"), status)
	}
	summary.ByProject = assetGroupValues(projectGroups)
	summary.ByAssetType = assetGroupValues(typeGroups)
	return summary
}

func addAssetGroup(groups map[string]*AssetGroupSummary, key, label, status string) {
	if key == "" {
		key = "unknown"
	}
	if label == "" {
		label = key
	}
	g, ok := groups[key]
	if !ok {
		g = &AssetGroupSummary{
			Key:      key,
			Label:    label,
			ByStatus: map[string]int{},
		}
		groups[key] = g
	}
	g.Total++
	g.ByStatus[status]++
}

func assetGroupValues(groups map[string]*AssetGroupSummary) []AssetGroupSummary {
	out := make([]AssetGroupSummary, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total == out[j].Total {
			return out[i].Label < out[j].Label
		}
		return out[i].Total > out[j].Total
	})
	return out
}

func (s *Server) handleAssetRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/assets/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	// 子资源：/api/assets/{id}/records、/api/assets/{id}/report（按已知后缀匹配，
	// 避免资产 id 里的分隔符干扰；assetID 形如 项目::模板::key，不含 /）
	if id := strings.TrimSuffix(rest, "/records"); id != rest {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
			return
		}
		s.handleAssetRecords(w, r, id)
		return
	}
	if id := strings.TrimSuffix(rest, "/report"); id != rest {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
			return
		}
		s.handleAssetReport(w, r, id)
		return
	}
	if id := strings.TrimSuffix(rest, "/cover"); id != rest {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
			return
		}
		s.handleAssetCoverUpload(w, r, id)
		return
	}
	if id := strings.TrimSuffix(rest, "/status-events"); id != rest {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
			return
		}
		s.handleAssetStatusEvents(w, r, id)
		return
	}
	if strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetAsset(w, r, rest)
	case http.MethodPatch:
		s.handlePatchAsset(w, r, rest)
	case http.MethodDelete:
		s.handleDeleteAsset(w, r, rest)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
	}
}

// handleCreateAsset 手工新增资产建档:主管权限。
// 巡检提交仍走 UpsertAsset 自动建档;此接口用于"设备先入台账、还没巡过"的场景,巡检数从 0 起。
func (s *Server) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	var req struct {
		Project    string `json:"project"`
		AssetType  string `json:"assetType"`
		AssetKey   string `json:"assetKey"`
		AssetName  string `json:"assetName"`
		TemplateID string `json:"templateId"`
		PointID    string `json:"pointId"`
		Summary    string `json:"summary"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Project = strings.TrimSpace(req.Project)
	req.AssetKey = strings.TrimSpace(req.AssetKey)
	req.AssetName = strings.TrimSpace(req.AssetName)
	req.AssetType = strings.TrimSpace(req.AssetType)
	if req.Project == "" || req.AssetKey == "" || req.AssetName == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "项目 / 编号 / 名称 均不能为空")
		return
	}
	tplPart := strings.TrimSpace(req.TemplateID)
	if tplPart == "" {
		tplPart = "manual"
	}
	asset := &AssetEntry{
		ID:          req.Project + "::" + tplPart + "::" + req.AssetKey,
		TenantID:    s.tenantForRequest(r), // 新资产打上创建者所属租户
		Project:     req.Project,
		ProjectCode: req.Project,
		PointID:     strings.TrimSpace(req.PointID),
		TemplateID:  strings.TrimSpace(req.TemplateID),
		AssetType:   req.AssetType,
		AssetKey:    req.AssetKey,
		AssetName:   req.AssetName,
		LastStatus:  "未巡检",
		StatusLevel: statusLevel("未巡检"),
		StatusOrder: statusOrder("未巡检"),
		LastSummary: strings.TrimSpace(req.Summary),
	}
	if err := s.store.CreateAsset(asset); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, "asset_exists", "同项目下已存在相同编号的资产")
			return
		}
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     "create_asset",
		TargetType: "asset",
		TargetID:   asset.ID,
		Detail:     map[string]any{"assetName": asset.AssetName, "project": asset.Project},
	})
	s.enrichAssetForDisplay(asset)
	writeJSON(w, http.StatusCreated, map[string]any{"asset": asset})
}

// handleDeleteAsset 删除资产:主管权限;有在途复查任务的先拦下(避免任务悬空);
// 巡检记录保留作历史证据,封面图文件一并清理。
func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "asset not found")
		return
	}
	tasks, err := s.store.ListEngineeringTasks(EngineeringTaskFilter{})
	if err == nil {
		for _, t := range tasks {
			if t.AssetID == id && t.Status != "已完成" && t.Status != "已取消" {
				writeError(w, http.StatusConflict, "asset_has_open_tasks",
					"该资产存在未完成的复查/整改任务,请先完成或取消任务后再删除")
				return
			}
		}
	}
	if err := s.store.DeleteAsset(s.tenantForRequest(r), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if asset.CoverImagePath != "" {
		_ = os.Remove(asset.CoverImagePath)
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     "delete_asset",
		TargetType: "asset",
		TargetID:   id,
		Detail:     map[string]any{"assetName": asset.AssetName, "project": asset.Project},
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleAssetCoverUpload(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	prev, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "asset not found")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("image")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing multipart file field: file")
		return
	}
	file.Close()
	img, err := saveMultipartFile(filepath.Join(s.storageDir, "assets", sanitizeAssetIdent(id)), header, 15<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}
	asset, err := s.store.UpdateAssetCover(s.tenantForRequest(r), id, img.Path)
	if err != nil {
		// DB 更新失败:回滚刚落盘的文件,避免留下孤儿
		_ = os.Remove(img.Path)
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	// 换图成功:删掉上一张封面文件(仅当确是旧封面且与新图不同,不动巡检记录图)
	if prev.CoverImagePath != "" && prev.CoverImagePath != img.Path {
		_ = os.Remove(prev.CoverImagePath)
	}
	s.enrichAssetForDisplay(asset)
	sanitized := s.sanitizeAssetsForRequest(r, []*AssetEntry{asset})
	if len(sanitized) == 0 {
		writeError(w, http.StatusInternalServerError, "sanitize_failed", "asset unavailable after update")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset":      sanitized[0],
		"coverImage": asset.CoverImage,
	})
}

// handleAssetRecords —— §3 按资产分页翻完整历史（查 asset_snapshots，不受记录列表窗口限制）
func (s *Server) handleAssetRecords(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	total, _ := s.store.CountAssetSnapshots(id)
	snaps, err := s.store.ListAssetSnapshots(id, pageSize, (page-1)*pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	if !s.hasSupervisorAccess(r) {
		all, listErr := s.store.ListAssetSnapshots(id, total, 0)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", listErr.Error())
			return
		}
		visible := make([]*AssetSnapshot, 0, len(all))
		for _, snap := range all {
			rec, recordErr := s.store.GetRecord(s.tenantForRequest(r), snap.RecordID)
			if recordErr == nil && s.canAccessRecord(r, rec, false) {
				visible = append(visible, snap)
			}
		}
		total = len(visible)
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		snaps = visible[start:end]
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset":      asset,
		"records":    snaps,
		"page":       page,
		"pageSize":   pageSize,
		"total":      total,
		"totalPages": totalPages,
	})
}

type fieldTrendPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

type fieldTrend struct {
	FieldKey      string            `json:"fieldKey"`
	FieldLabel    string            `json:"fieldLabel"`
	Points        []fieldTrendPoint `json:"points"`
	Current       *float64          `json:"current,omitempty"`
	Previous      *float64          `json:"previous,omitempty"`
	AvgRecent     *float64          `json:"avgRecent,omitempty"`
	ChangeRate    *float64          `json:"changeRate,omitempty"`
	OverThreshold bool              `json:"overThreshold"`
}

// handleAssetStatusEvents —— P1-2 电梯等状态类资产的"状态事件统计",
// 字段都是 choice 时数值趋势空白,这里返回 巡检次数/正常/待复核/异常/补拍/无判/未看图/重复异常字段 Top。
func (s *Server) handleAssetStatusEvents(w http.ResponseWriter, r *http.Request, id string) {
	if !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅管理角色可查看资产状态统计")
		return
	}
	rangeKey := firstNonEmpty(r.URL.Query().Get("range"), "30d")
	stat, err := s.toolGetStatusEvents(id, rangeKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stat)
}

// handleAssetReport —— §3/§5 设备健康报告：对数值字段算 本次/上次/近N均值/变化率/超阈值。
// 趋势由后端规则计算，不依赖大模型；AI 只负责把这些数字转成可读摘要。
func (s *Server) handleAssetReport(w http.ResponseWriter, r *http.Request, id string) {
	if !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅管理角色可查看资产健康报告")
		return
	}
	if _, err := s.store.GetAsset(s.tenantForRequest(r), id); err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	days := 30
	switch r.URL.Query().Get("range") {
	case "7d":
		days = 7
	case "90d":
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	obs, err := s.store.ListFieldObservations(id, "", 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	grouped := map[string]*fieldTrend{}
	order := []string{}
	for _, o := range obs {
		if o.ValueNumber == nil || o.CreatedAt.Before(since) {
			continue
		}
		ft, ok := grouped[o.FieldKey]
		if !ok {
			ft = &fieldTrend{FieldKey: o.FieldKey, FieldLabel: o.FieldLabel}
			grouped[o.FieldKey] = ft
			order = append(order, o.FieldKey)
		}
		ft.Points = append(ft.Points, fieldTrendPoint{Time: o.CreatedAt.Format("2006-01-02 15:04"), Value: *o.ValueNumber})
	}
	trends := []*fieldTrend{}
	for _, k := range order {
		ft := grouped[k]
		n := len(ft.Points)
		if n == 0 {
			continue
		}
		cur := ft.Points[n-1].Value
		ft.Current = &cur
		if n >= 2 {
			prev := ft.Points[n-2].Value
			ft.Previous = &prev
			if prev != 0 {
				cr := (cur - prev) / prev
				ft.ChangeRate = &cr
				abs := cr
				if abs < 0 {
					abs = -abs
				}
				ft.OverThreshold = abs > 0.1
			}
		}
		m := n
		if m > 7 {
			m = 7
		}
		sum := 0.0
		for i := n - m; i < n; i++ {
			sum += ft.Points[i].Value
		}
		avg := sum / float64(m)
		ft.AvgRecent = &avg
		trends = append(trends, ft)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"assetId": id,
		"range":   fmt.Sprintf("%dd", days),
		"fields":  trends,
	})
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	s.enrichAssetForDisplay(asset)
	// 顺带返回该资产历史巡检（最近 20 条）
	history := s.collectAssetHistory(r, asset, 20)
	writeJSON(w, http.StatusOK, map[string]any{
		"asset":   s.sanitizeAssetsForRequest(r, []*AssetEntry{asset})[0],
		"history": history,
	})
}

func (s *Server) handlePatchAsset(w http.ResponseWriter, r *http.Request, id string) {
	// 有 asset_manage 能力可直接 PATCH;其他角色必须走 /api/change-requests 审批流。
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	var req struct {
		AssetName   string `json:"assetName"`
		LastStatus  string `json:"lastStatus"`
		LastSummary string `json:"lastSummary"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.AssetName == "" && req.LastStatus == "" && req.LastSummary == "" {
		writeError(w, http.StatusBadRequest, "no_fields", "至少修改一个字段：assetName / lastStatus / lastSummary")
		return
	}
	if req.LastStatus != "" {
		valid := map[string]bool{"正常": true, "异常": true, "待复核": true, "待维修": true}
		if !valid[req.LastStatus] {
			writeError(w, http.StatusBadRequest, "bad_status",
				"lastStatus 必须是：正常 / 异常 / 待复核 / 待维修")
			return
		}
	}
	asset, err := s.store.UpdateAssetMeta(s.tenantForRequest(r), id, req.AssetName, req.LastStatus, req.LastSummary)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在或更新失败")
		return
	}
	s.recordOperation(r, "asset.patch", "asset", id, map[string]any{
		"assetName":   req.AssetName,
		"lastStatus":  req.LastStatus,
		"lastSummary": req.LastSummary,
	})
	// 资产被标记为"正常" → 异常闭环回写：清掉对应任务的待整改态、重算计划
	if asset != nil && asset.LastStatus == "正常" {
		s.onAssetResolvedNormal(id)
	}
	writeJSON(w, http.StatusOK, asset)
}

// collectAssetHistory 从所有 records 里筛出归到该资产的。
// 简单实现：遍历最近 200 条 record，过滤 + 排序。
func (s *Server) collectAssetHistory(r *http.Request, asset *AssetEntry, limit int) []*Record {
	all, err := s.store.ListRecords(s.tenantForRequest(r), 200)
	if err != nil {
		return nil
	}
	var matched []*Record
	for _, rec := range all {
		if !rec.Submitted {
			continue
		}
		if !s.canAccessRecord(r, rec, false) {
			continue
		}
		if recordTouchesAsset(rec, asset) {
			matched = append(matched, rec)
			if len(matched) >= limit {
				break
			}
		}
	}
	return matched
}

func sanitizeRecordsForCurrentTemplates(records []*Record) []*Record {
	out := make([]*Record, 0, len(records))
	for _, rec := range records {
		out = append(out, sanitizeRecordForCurrentTemplate(rec))
	}
	return out
}

func sanitizeRecordForCurrentTemplate(rec *Record) *Record {
	if rec == nil {
		return nil
	}
	tpl, ok := templateByID(rec.TemplateID)
	if !ok {
		return rec
	}
	allowed := map[string]bool{}
	allowedLabels := []string{}
	removedLabels := []string{}
	for _, f := range tpl.Fields {
		allowed[f.Code] = true
		allowedLabels = append(allowedLabels, f.Label)
	}
	clean := *rec
	clean.Fields = make([]FieldValue, 0, len(rec.Fields))
	for _, f := range rec.Fields {
		if allowed[f.Code] {
			clean.Fields = append(clean.Fields, f)
		} else {
			removedLabels = append(removedLabels, f.Label)
		}
	}
	// M41 · 防御：剔除推荐里提到已删字段 label 的条目
	if len(removedLabels) > 0 && len(rec.AIRecommendations) > 0 {
		filtered := make([]Recommendation, 0, len(rec.AIRecommendations))
		for _, r := range rec.AIRecommendations {
			hit := false
			full := r.Text + " " + r.Basis + " " + r.Category
			for _, lbl := range removedLabels {
				if lbl == "" {
					continue
				}
				if strings.Contains(full, lbl) {
					hit = true
					break
				}
			}
			if !hit {
				filtered = append(filtered, r)
			}
		}
		clean.AIRecommendations = filtered
	}
	return &clean
}

func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	var records []*Record
	var err error
	if s.hasSupervisorAccess(r) {
		records, err = s.store.ListRecords(s.tenantForRequest(r), 100)
	} else if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		records, err = s.store.ListRecordsByOwner(s.tenantForRequest(r), user.ID, user.DisplayName, user.Username, 100)
	} else if s.localNoAuthAllowed(r) {
		records, err = s.store.ListRecordsByOwner(s.tenantForRequest(r), "", userName(r), "", 100)
	} else {
		writeError(w, http.StatusForbidden, "forbidden", "请使用巡检员账号登录")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_records_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": sanitizeRecordsForCurrentTemplates(records)})
}

func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PointID    string   `json:"pointId"`
		TemplateID string   `json:"templateId"`
		Inspector  string   `json:"inspector"`
		TmpDir     string   `json:"tmpDir"`   // 来自场景分类后的临时目录，可选
		ImageIDs   []string `json:"imageIds"` // tmpDir 里要采纳的图片 ID
		EngTaskID  string   `json:"engineeringTaskId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	inspectorUserID := ""
	// 安全：如果有 session 用户登录，inspector 强制锁定到登录账号，
	// 不允许前端传别人名字伪造巡检记录归属。
	// 没 session（兜底 token 模式）仍允许 body 传 inspector。
	if sessionUser, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		req.Inspector = strings.TrimSpace(sessionUser.DisplayName)
		if req.Inspector == "" {
			req.Inspector = sessionUser.Username
		}
		inspectorUserID = sessionUser.ID
	} else if !s.localNoAuthAllowed(r) {
		writeError(w, http.StatusForbidden, "forbidden", "请使用巡检员账号登录后创建记录")
		return
	}
	if req.Inspector == "" {
		req.Inspector = "巡检员"
	}

	templateID := req.TemplateID
	pointID := req.PointID
	if pointID == "" && templateID != "" {
		// 自动找该模板对应的默认点位
		for _, p := range seedPoints() {
			if p.TemplateID == templateID {
				pointID = p.ID
				break
			}
		}
	}
	point, ok := pointByID(pointID)
	if !ok {
		writeError(w, http.StatusBadRequest, "point_not_found", "未找到对应巡检点位")
		return
	}
	if templateID == "" {
		templateID = point.TemplateID
	}
	tpl, ok := templateByID(templateID)
	if !ok {
		writeError(w, http.StatusBadRequest, "template_not_found", "未找到日报模板")
		return
	}
	req.EngTaskID = strings.TrimSpace(req.EngTaskID)
	if req.EngTaskID != "" {
		task, err := s.store.GetEngineeringTask(req.EngTaskID)
		if err != nil || task == nil {
			writeError(w, http.StatusBadRequest, "engineering_task_not_found", "关联的工程任务不存在")
			return
		}
		if task.Status == engTaskStatusDone || task.Status == engTaskStatusCanceled {
			writeError(w, http.StatusBadRequest, "engineering_task_closed", "关联的工程任务已关闭，不能继续填报")
			return
		}
	}

	now := time.Now()
	recordID := newID("rec")
	rec := &Record{
		ID:                recordID,
		RecordNo:          businessRecordNo(recordID, point.Project, point.ID, point.Name, now),
		Project:           point.Project,
		PointID:           point.ID,
		PointName:         point.Name,
		TemplateID:        tpl.ID,
		TemplateName:      tpl.Name,
		Type:              point.Type,
		Inspector:         req.Inspector,
		InspectorUserID:   inspectorUserID,
		EngineeringTaskID: req.EngTaskID,
		RecognitionStatus: "not_started",
		Images:            []ImageInfo{},
		Fields:            initialFieldValues(tpl, req.Inspector),
		AISummaryTags:     []string{},
		AIRecommendations: []Recommendation{},
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// 如果带了 tmpDir，把临时图片移到正式目录
	if req.TmpDir != "" {
		moved, err := s.adoptTmpImages(rec.ID, req.TmpDir, req.ImageIDs)
		if err != nil {
			writeError(w, http.StatusBadRequest, "adopt_images_failed", err.Error())
			return
		}
		rec.Images = moved
	}

	if err := s.store.CreateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "create_record_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleRecordRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/inspection/records/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	recordID := parts[0]
	write := r.Method != http.MethodGet
	if _, ok := s.requireRecordAccess(w, r, recordID, write); !ok {
		return
	}

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleGetRecord(w, r, recordID)
	case len(parts) == 2 && parts[1] == "images" && r.Method == http.MethodPost:
		s.handleUploadImages(w, r, recordID)
	case len(parts) == 2 && parts[1] == "ai-tasks" && r.Method == http.MethodPost:
		s.handleStartAnalysis(w, r, recordID)
	case len(parts) == 3 && parts[1] == "ai" && parts[2] == "latest" && r.Method == http.MethodGet:
		s.handleGetLatestTask(w, r, recordID)
	case len(parts) == 3 && parts[1] == "fields" && r.Method == http.MethodPatch:
		s.handlePatchField(w, r, recordID, parts[2])
	case len(parts) == 2 && parts[1] == "manual" && r.Method == http.MethodPost:
		s.handleEnableManual(w, r, recordID)
	case len(parts) == 2 && parts[1] == "submit" && r.Method == http.MethodPost:
		s.handleSubmit(w, r, recordID)
	case len(parts) == 2 && parts[1] == "confirm-logs" && r.Method == http.MethodGet:
		s.handleListConfirmLogs(w, r, recordID)
	default:
		writeError(w, http.StatusNotFound, "not_found", "")
	}
}

func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := s.store.GetRecord(s.tenantForRequest(r), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRecordForCurrentTemplate(rec))
}

func (s *Server) handleUploadImages(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	tpl, _ := templateByID(rec.TemplateID)
	maxImages := normalizedMaxImages(tpl.MaxImages)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "请先选择图片")
		return
	}
	// MaxImages 是单轮拍摄上限。识别失败后允许在同一条记录补拍，
	// 最多保留三轮现场证据，避免重建记录或无限写盘。
	if len(files) > maxImages {
		writeError(w, http.StatusBadRequest, "too_many_files",
			fmt.Sprintf("当前模板每轮最多上传 %d 张图片", maxImages))
		return
	}
	if len(rec.Images)+len(files) > maxImages*3 {
		writeError(w, http.StatusBadRequest, "too_many_retake_files",
			fmt.Sprintf("当前模板最多保留 3 轮照片，请转人工填写或重新开始巡检"))
		return
	}
	dir := filepath.Join(s.storageDir, "uploads", recordID)
	saved := []ImageInfo{}
	for _, header := range files {
		img, err := saveMultipartFile(dir, header, 15<<20)
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
			return
		}
		img.ContentHash = hashFile(img.Path)
		saved = append(saved, img)
	}
	rec.Images = append(rec.Images, saved...)
	if err := s.store.UpdateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"images": saved, "record": sanitizeRecordForCurrentTemplate(rec)})
}

func (s *Server) handleStartAnalysis(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	if len(rec.Images) == 0 {
		writeError(w, http.StatusBadRequest, "no_images", "请先上传图片")
		return
	}
	tpl, _ := templateByID(rec.TemplateID)
	if !tpl.HasAI {
		// 该模板第一版没接 AI，直接转人工
		rec.RecognitionStatus = "manual_required"
		rec.ManualRequired = true
		rec.RetakeReason = "该模板暂未启用 AI 识别，请直接人工填写"
		_ = s.store.UpdateRecord(rec)
		writeJSON(w, http.StatusOK, map[string]any{
			"action": "manual_fallback",
			"record": rec,
			"reason": rec.RetakeReason,
		})
		return
	}

	now := time.Now()
	task := &AITask{
		ID:        newID("task"),
		RecordID:  recordID,
		Status:    "queued",
		Progress:  Progress{Total: len(rec.Images)},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateTask(task); err != nil {
		writeError(w, http.StatusInternalServerError, "create_task_failed", err.Error())
		return
	}

	// 失败重拍计数：每次发起 analysis 算一次拍照尝试
	rec.CaptureAttempts++
	rec.RecognitionStatus = "processing"
	rec.RetakeReason = ""
	rec.TaskID = task.ID
	_ = s.store.UpdateRecord(rec)

	go s.runAnalysis(s.tenantForRequest(r), task.ID, recordID)
	writeJSON(w, http.StatusAccepted, task)
}

func normalizedMaxImages(maxImages int) int {
	if maxImages <= 0 {
		return 3
	}
	return maxImages
}

func recentImagesForAnalysis(images []ImageInfo, maxImages int) []ImageInfo {
	maxImages = normalizedMaxImages(maxImages)
	if len(images) <= maxImages {
		return images
	}
	return images[len(images)-maxImages:]
}

func (s *Server) runAnalysis(tenantID, taskID, recordID string) {
	// 并发闸:同时进行的视觉识别有上限(INSPECTAI_AI_CONCURRENCY,默认 3),
	// 超出的排队等待;免费档限流下无脑并发只会整体更慢。
	s.aiSem <- struct{}{}
	defer func() { <-s.aiSem }()

	_ = s.store.UpdateTask(taskID, func(t *AITask) {
		t.Status = "processing"
	})

	rec, err := s.store.GetRecord(tenantID, recordID)
	if err != nil {
		_ = s.store.UpdateTask(taskID, func(t *AITask) {
			t.Status = "failed"
			t.ErrorCode = "record_not_found"
			t.ErrorMessage = err.Error()
		})
		return
	}
	tpl, _ := templateByID(rec.TemplateID)

	// 同一记录会保留最多三轮现场证据，但重新分析优先使用最近一轮照片。
	// 这样补拍的特写不会被首轮旧图挤出视觉模型输入窗口。
	imagesForAnalysis := recentImagesForAnalysis(rec.Images, tpl.MaxImages)
	imagePayloads := make([]map[string]any, 0, len(imagesForAnalysis))
	for _, img := range imagesForAnalysis {
		imagePayloads = append(imagePayloads, map[string]any{
			"id":       img.ID,
			"fileName": img.FileName,
			"path":     img.Path,
		})
	}

	templatePayload := map[string]any{
		"id":     tpl.ID,
		"name":   tpl.Name,
		"prompt": tpl.AIPrompt,
		"fields": tpl.Fields,
	}
	// 模块化提示词:已迁移到结构化数据的模板,渲染成提示词文本随 payload 下发;
	// ai-service 优先用 promptText,没有则回退读 .md(灰度迁移、不破坏未迁移模板)
	if rendered, ok := renderPromptViaStore(s.store, tpl.ID); ok {
		templatePayload["promptText"] = rendered
	}
	payload := map[string]any{
		"recordId": rec.ID,
		"template": templatePayload,
		"images":   imagePayloads,
	}

	resp, err := s.aiClient.Analyze(payload)
	failed := false
	failReason := ""
	if err != nil {
		failed = true
		failReason = "AI 服务未响应：" + truncate(err.Error(), 60)
	} else {
		failed, failReason = recognitionFailed(resp, tpl)
	}

	rec, _ = s.store.GetRecord(tenantID, recordID)
	if rec == nil {
		return
	}

	if failed {
		rec.RetakeReason = failReason
		if rec.CaptureAttempts >= 3 {
			rec.ManualRequired = true
			rec.RecognitionStatus = "manual_required"
		} else {
			rec.RecognitionStatus = "retake_required"
		}
		_ = s.store.UpdateRecord(rec)
		_ = s.store.UpdateTask(taskID, func(t *AITask) {
			t.Status = "failed"
			t.ErrorCode = rec.RecognitionStatus
			t.ErrorMessage = failReason
			if resp != nil {
				t.Analysis = analysisToMap(resp)
			}
		})
		return
	}

	// 成功路径：把识别字段写回 fields
	applyRecognizedFields(rec, resp.RecognizedFields)
	rec.RecognitionStatus = "recognized"
	rec.ManualRequired = false
	rec.RetakeReason = ""
	rec.Report = buildDailyPreview(rec)
	_ = s.store.UpdateRecord(rec)
	_ = s.store.UpdateTask(taskID, func(t *AITask) {
		t.Status = "succeeded"
		t.Progress.Processed = t.Progress.Total
		t.Analysis = analysisToMap(resp)
	})
}

func (s *Server) handleGetLatestTask(w http.ResponseWriter, _ *http.Request, recordID string) {
	task, err := s.store.LatestTaskByRecord(recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task_not_found", "暂无 AI 任务")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/ai/tasks/")
	task, err := s.store.GetTask(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task_not_found", "AI 任务不存在")
		return
	}
	if _, ok := s.requireRecordAccess(w, r, task.RecordID, false); !ok {
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handlePatchField(w http.ResponseWriter, r *http.Request, recordID, code string) {
	var req struct {
		Value       string `json:"value"`
		Version     int    `json:"version"`
		Action      string `json:"action"`      // confirm / correct / uncertain（缺省按值是否变化推断）
		DurationMs  int    `json:"durationMs"`  // 该字段停留时长（移动端可选上报）
		ViewedPhoto bool   `json:"viewedPhoto"` // 是否看过原图（移动端可选上报）
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	field, _ := fieldByCode(rec.Fields, code)
	if field == nil {
		writeError(w, http.StatusNotFound, "field_not_found", "字段不存在")
		return
	}
	if req.Version != 0 && req.Version != field.Version {
		writeError(w, http.StatusConflict, "version_conflict", "字段已被更新，请刷新")
		return
	}

	// 留痕用：变更前先抓 AI 原值 / 改前值 / 置信度
	aiValue := field.AIValue
	originalValue := field.Value
	confidence := field.Confidence

	action := req.Action
	switch {
	case action == "uncertain":
		// 人工无法判定：保留待复核交主管抽查，不改值
		field.Source = "human-uncertain"
		field.NeedsReview = true
	case strings.TrimSpace(req.Value) == strings.TrimSpace(originalValue):
		field.Source = "human-confirmed"
		field.NeedsReview = false
		action = "confirm"
	default:
		field.Value = req.Value
		field.Source = "human-edited"
		field.NeedsReview = false
		action = "correct"
	}
	field.Version++
	rec.Report = buildDailyPreview(rec)
	if err := s.store.UpdateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	// §4 防惰性闭环：每次确认/修正/标疑都留痕（AI 原值 → 最终值、置信度、操作人、停留时长、是否看图）
	operator := rec.Inspector
	if u, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		if u.DisplayName != "" {
			operator = u.DisplayName
		} else {
			operator = u.Username
		}
	}
	_ = s.store.CreateFieldConfirmLog(&FieldConfirmLog{
		RecordID:      recordID,
		FieldKey:      field.Code,
		FieldLabel:    field.Label,
		AIValue:       aiValue,
		OriginalValue: originalValue,
		FinalValue:    field.Value,
		AIConfidence:  confidence,
		Action:        action,
		Operator:      operator,
		DurationMs:    req.DurationMs,
		ViewedPhoto:   req.ViewedPhoto,
	})
	writeJSON(w, http.StatusOK, field)
}

// handleListConfirmLogs —— §4 返回某条记录的字段确认留痕，供后台展示"谁确认了什么、AI原值→最终值"。
// 权限:主管/管理员读全部;巡检员只能读自己提交/经手的记录留痕(防止互相看)。
func (s *Server) handleListConfirmLogs(w http.ResponseWriter, r *http.Request, recordID string) {
	logs, err := s.store.ListFieldConfirmLogs(recordID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleEnableManual(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	rec.ManualRequired = true
	rec.RecognitionStatus = "manual_required"
	rec.RetakeReason = "已切换为人工填写"
	if err := s.store.UpdateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRecordForCurrentTemplate(rec))
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request, recordID string) {
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "提交需要 Idempotency-Key")
		return
	}
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}

	// P1-3 幂等：已提交的 record，直接返回旧结果，不重复 upsert 台账
	if rec.Submitted {
		writeJSON(w, http.StatusOK, sanitizeRecordForCurrentTemplate(rec))
		return
	}

	// P1-2 后端字段校验
	if rec.RecognitionStatus == "processing" || rec.RecognitionStatus == "queued" {
		writeError(w, http.StatusBadRequest, "ai_in_progress", "AI 识别尚未完成，请稍后再提交")
		return
	}
	if rec.RecognitionStatus == "retake_required" && !rec.ManualRequired {
		writeError(w, http.StatusBadRequest, "needs_retake", "请先重拍或转人工填写后再提交")
		return
	}
	var missing []string
	var pending []string
	for _, f := range rec.Fields {
		if f.Required && strings.TrimSpace(f.Value) == "" {
			missing = append(missing, f.Label)
		}
		if f.NeedsReview {
			pending = append(pending, f.Label)
		}
	}
	if len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "missing_required",
			"必填字段未填写："+strings.Join(missing, "、"))
		return
	}
	if len(pending) > 0 {
		writeError(w, http.StatusBadRequest, "needs_review",
			"以下字段需人工确认："+strings.Join(pending, "、"))
		return
	}

	// P0-4 防惰性闭环:任何 source 仍是「ai」的字段说明从未被人工 patch 过,
	// 必须先「确认全部 AI 识别字段」或逐项 tap,留下 confirm log 后才能提交。
	var unconfirmed []string
	for _, f := range rec.Fields {
		if f.Source == "ai" && strings.TrimSpace(f.Value) != "" && f.Confidence < 0.95 {
			unconfirmed = append(unconfirmed, f.Label)
		}
	}
	if len(unconfirmed) > 0 {
		writeError(w, http.StatusBadRequest, "needs_confirmation",
			"以下低置信字段需确认:"+strings.Join(unconfirmed, "、"))
		return
	}

	claim, err := s.store.ClaimSubmission(recordID, idemKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "idempotency_failed", err.Error())
		return
	}
	switch claim {
	case submissionClaimed:
	case submissionDuplicate:
		latest, latestErr := s.store.GetRecord(s.tenantForRequest(r), recordID)
		if latestErr == nil && latest.Submitted {
			writeJSON(w, http.StatusOK, sanitizeRecordForCurrentTemplate(latest))
			return
		}
		writeError(w, http.StatusConflict, "submission_in_progress", "相同提交正在处理中，请稍后刷新")
		return
	case submissionInProgress:
		writeError(w, http.StatusConflict, "submission_in_progress", "相同提交正在处理中，请稍后刷新")
		return
	case submissionBusy:
		writeError(w, http.StatusConflict, "duplicate_submission", "该日报已有另一个提交请求正在处理")
		return
	default:
		writeError(w, http.StatusConflict, "duplicate_submission", "该日报正在提交中")
		return
	}

	// 1. 调 ai-service /summarize 同步生成总结+建议
	// M41 · 只传当前模板还在的字段（防止 AI 引用已删除字段）
	cleanedRec := sanitizeRecordForCurrentTemplate(rec)
	historyPayload := s.lookupAssetHistory(cleanedRec)
	summaryPayload := map[string]any{
		"templateName":   rec.TemplateName,
		"project":        rec.Project,
		"pointName":      rec.PointName,
		"inspector":      rec.Inspector,
		"inspectionTime": rec.CreatedAt.Format("2006-01-02 15:04"),
		"fields":         simplifyFieldsForSummary(cleanedRec.Fields),
		"history":        historyPayload,
	}
	summary, sumErr := s.aiClient.Summarize(summaryPayload)
	now := time.Now()

	if sumErr != nil {
		rec.AISummary = buildFallbackSummary(rec)
		rec.AIRecommendations = []Recommendation{}
		rec.AISummaryError = truncate(sumErr.Error(), 120)
	} else {
		rec.AISummary = summary.Summary
		rec.AISummaryTags = summary.Tags
		rec.AIRecommendations = summary.Recommendations
		if strings.HasPrefix(summary.Model, "fallback") {
			rec.AISummaryError = "AI 总结降级：" + summary.Model
		} else {
			rec.AISummaryError = ""
		}
	}

	rec.Report = buildDailyPreview(rec)
	rec.Submitted = true
	rec.SubmittedAt = &now
	assets := buildAssets(rec, now)
	// P0-6 原子写入:日报/资产/快照/观测同事务,失败整体回滚,杜绝"日报已提交但快照漏写"
	snaps, obs := buildRecordObservations(rec, assets, now)
	if err := s.store.SubmitRecordWithAssets(rec, assets, snaps, obs); err != nil {
		_ = s.store.ReleaseSubmission(recordID, idemKey)
		writeError(w, http.StatusInternalServerError, "submit_failed",
			"日报提交与台账写入失败，已回滚："+err.Error())
		return
	}
	if err := s.closeEngineeringTaskFromRecord(rec, assets, now); err != nil {
		s.recordOperation(r, "engineering_task_close_failed", "record", rec.ID, map[string]any{
			"engineeringTaskId": rec.EngineeringTaskID,
			"error":             err.Error(),
		})
	} else if rec.EngineeringTaskID != "" {
		s.recordOperation(r, "engineering_task_auto_closed", "engineering_task", rec.EngineeringTaskID, map[string]any{
			"recordId": rec.ID,
		})
	}
	// 复检合格自动销账：本次提交使资产恢复"正常"时，闭环该资产遗留的"待整改"任务
	// （与「标记正常」「审批通过」共用 onAssetResolvedNormal，三条恢复路径行为一致）。
	for _, asset := range assets {
		if asset != nil && asset.LastStatus == "正常" {
			s.onAssetResolvedNormal(asset.ID)
		}
	}
	// 检出异常（需跟进）时，确保移动端「我的任务」有一条待整改任务可跟进——
	// 计划挂钩的巡检已由 closeEngineeringTaskFromRecord 置为待整改；这里兜底处理
	// 临时/非计划巡检：没有覆盖该资产的在途任务时，自动建一条待整改任务并同步移动端。
	s.ensureFollowupTaskForAnomalies(rec, assets, now)
	_ = s.store.CompleteSubmission(recordID, idemKey)
	s.notifyInspectionSubmitted(rec, assets)

	writeJSON(w, http.StatusOK, sanitizeRecordForCurrentTemplate(rec))
}

// ===== 场景分类 =====

func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string           `json:"message"`
		History []map[string]any `json:"history,omitempty"`
		Context map[string]any   `json:"context,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "请输入要询问的问题")
		return
	}
	// 自动补充平台实时快照供 AI 引用
	ctx := req.Context
	if ctx == nil {
		ctx = map[string]any{}
	}
	if assets, err := s.store.ListAssets(s.tenantForRequest(r)); err == nil {
		total, normal, warning, danger := 0, 0, 0, 0
		for _, a := range assets {
			total++
			switch a.LastStatus {
			case "正常":
				normal++
			case "异常":
				danger++
			case "待复核":
				warning++
			}
		}
		ctx["assetTotal"] = total
		ctx["assetNormal"] = normal
		ctx["assetWarning"] = warning
		ctx["assetDanger"] = danger
	}
	if recs, err := s.store.ListRecords(s.tenantForRequest(r), 1000); err == nil {
		ctx["recordTotal"] = len(recs)
	}
	if reqs, err := s.store.ListChangeRequests(ChangeRequestFilter{}); err == nil {
		pending := 0
		for _, c := range reqs {
			if c.Status == "pending" {
				pending++
			}
		}
		ctx["changeRequestPending"] = pending
	}
	payload := map[string]any{
		"message": req.Message,
		"history": req.History,
		"context": ctx,
	}
	resp, err := s.aiClient.Chat(payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, "ai_chat_failed", err.Error())
		return
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		UserID:     s.currentUserID(r),
		ActorName:  s.currentUserName(r),
		Action:     "ai_chat",
		TargetType: "ai",
		Detail:     map[string]any{"message": req.Message, "model": resp["model"]},
	})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) currentUserID(r *http.Request) string {
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		return user.ID
	}
	return "anonymous"
}

func (s *Server) handleClassifyScene(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "请先拍照")
		return
	}
	// 上限提到 20:全部存下供字段识别(analyze)使用,否则超 6 张的部位永远识别不到
	if len(files) > 20 {
		files = files[:20]
	}

	tmpDirID := newID("cls")
	tmpDir := filepath.Join(s.storageDir, "tmp_classify", tmpDirID)
	saved := []ImageInfo{}
	paths := []string{}
	// 前 6 张送场景分类(机房照可能排在第 4-6 张,只看前 3 张会漏判有机房)
	for _, header := range files[:min(len(files), 6)] {
		img, err := saveMultipartFile(tmpDir, header, 15<<20)
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
			return
		}
		paths = append(paths, img.Path)
		saved = append(saved, img)
	}
	// 其余的也存到 tmpDir 备用（用户确认模板后会带 imageIds 一起 createRecord）
	for _, header := range files[min(len(files), 6):] {
		img, err := saveMultipartFile(tmpDir, header, 15<<20)
		if err != nil {
			continue
		}
		saved = append(saved, img)
	}

	result, err := s.aiClient.Classify(paths)
	if err != nil {
		result := &SceneClassifyResult{
			TemplateID:      "unknown",
			TemplateName:    "无法识别",
			Confidence:      0,
			NeedsManualPick: true,
			TmpDir:          tmpDir,
			Error:           truncate(err.Error(), 120),
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"classify": result,
			"images":   saved,
		})
		return
	}
	result.TmpDir = tmpDir
	// 把候选模板名补全
	if tpl, ok := templateByID(result.TemplateID); ok {
		result.TemplateName = tpl.Name
	} else if result.TemplateID == "unknown" || result.TemplateID == "" {
		result.TemplateName = "无法识别"
		result.NeedsManualPick = true
	}
	// 把 saved images 也带回去（前端创建记录时 adopt）
	writeJSON(w, http.StatusOK, map[string]any{
		"classify": result,
		"images":   saved,
	})
}

// ===== 静态文件 =====

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/storage/") {
		// 提供上传图片访问（避免暴露任意文件，仅 storage 子树）
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/storage/"))
		if strings.Contains(clean, "..") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		parts := strings.Split(filepath.ToSlash(clean), "/")
		if len(parts) >= 2 && parts[0] == "uploads" {
			if _, ok := s.requireRecordAccess(w, r, parts[1], false); !ok {
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(s.storageDir, clean))
		return
	}
	if r.URL.Path == "/" || !strings.Contains(filepath.Base(r.URL.Path), ".") {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		http.ServeFile(w, r, filepath.Join(s.frontendDir, "index.html"))
		return
	}
	// 用 path.Clean（URL 路径包，跨平台一致）而不是 filepath.Clean
	// 后者在 Linux 会把 "/styles.css" 当作绝对路径，导致下面 IsAbs 误判返 403。
	cleanURL := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	rel := strings.TrimPrefix(cleanURL, "/")
	if rel == "" || rel == "." {
		http.ServeFile(w, r, filepath.Join(s.frontendDir, "index.html"))
		return
	}
	// 二次校验：阻止 .. 段、Windows 盘符等逃逸到 frontendDir 之外
	if strings.Contains(rel, "..") || filepath.IsAbs(rel) || (len(rel) >= 2 && rel[1] == ':') {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	fullPath := filepath.Join(s.frontendDir, filepath.FromSlash(rel))
	// 终态校验：解出的最终路径必须仍在 frontendDir 子树里（防符号链接 / Join 行为差异）
	if absRoot, err := filepath.Abs(s.frontendDir); err == nil {
		if absTarget, err := filepath.Abs(fullPath); err == nil {
			if !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) && absTarget != absRoot {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		// 静态资源（含 ?v= 查询串）允许浏览器缓存，但默认不强缓存
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, fullPath)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.frontendDir, "index.html"))
}

func (s *Server) cleanupTmpClassifyDirs() (int, error) {
	ttlHours := 24
	if raw := strings.TrimSpace(os.Getenv("TMP_IMAGE_TTL_HOURS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ttlHours = v
		}
	}
	root := filepath.Join(s.storageDir, "tmp_classify")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-time.Duration(ttlHours) * time.Hour)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

// ===== 辅助 =====

func (s *Server) adoptTmpImages(recordID, tmpDir string, imageIDs []string) ([]ImageInfo, error) {
	if !strings.HasPrefix(filepath.Clean(tmpDir), filepath.Clean(filepath.Join(s.storageDir, "tmp_classify"))) {
		return nil, errors.New("invalid tmpDir")
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, err
	}
	wantSet := map[string]bool{}
	for _, id := range imageIDs {
		wantSet[id] = true
	}

	dstDir := filepath.Join(s.storageDir, "uploads", recordID)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return nil, err
	}
	out := []ImageInfo{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// 文件名格式：img_{时间戳}_{随机}_{原文件名}，imageID 是前 3 段
		parts := strings.SplitN(name, "_", 4)
		if len(parts) < 4 || parts[0] != "img" {
			continue
		}
		id := parts[0] + "_" + parts[1] + "_" + parts[2]
		original := parts[3]
		if len(wantSet) > 0 && !wantSet[id] {
			continue
		}
		src := filepath.Join(tmpDir, name)
		dst := filepath.Join(dstDir, name)
		if err := os.Rename(src, dst); err != nil {
			// rename 失败时退化为 copy
			if data, e := os.ReadFile(src); e == nil {
				_ = os.WriteFile(dst, data, 0644)
				_ = os.Remove(src)
			} else {
				continue
			}
		}
		info, _ := os.Stat(dst)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, ImageInfo{
			ID:          id,
			FileName:    original,
			Path:        dst,
			Size:        size,
			ContentHash: hashFile(dst),
			CreatedAt:   time.Now(),
		})
	}
	// 清理 tmpDir
	_ = os.RemoveAll(tmpDir)
	return out, nil
}

func (s *Server) lookupAssetHistory(rec *Record) any {
	all, err := s.store.ListRecords(rec.TenantID, 100)
	if err != nil {
		return nil
	}
	for _, last := range all {
		if last == nil || last.ID == rec.ID || !last.Submitted {
			continue
		}
		if last.Project != rec.Project || last.TemplateID != rec.TemplateID || last.PointID != rec.PointID {
			continue
		}
		return map[string]any{
			"lastInspectionTime": last.CreatedAt.Format("2006-01-02 15:04"),
			"lastFields":         simplifyFieldsForSummary(last.Fields),
		}
	}
	return nil
}

func recognitionFailed(resp *AnalyzeResponse, tpl ReportTemplate) (bool, string) {
	if resp == nil {
		return true, "AI 未返回识别结果"
	}
	if resp.RecognitionStatus == "retake_required" || resp.RecognitionStatus == "failed" {
		return true, firstNonEmpty(resp.RetakeReason, "图片无法稳定识别，请重拍")
	}
	if len(resp.RecognizedFields) == 0 {
		return true, "未识别到日报字段，请重拍"
	}
	// 检查 ai+required 字段是否至少有一个识别到
	requiredAI := 0
	gotRequired := 0
	gotByCode := map[string]RecognizedField{}
	for _, f := range resp.RecognizedFields {
		gotByCode[f.Code] = f
	}
	for _, f := range tpl.Fields {
		if f.Required && f.Source == "ai" {
			requiredAI++
			if _, ok := gotByCode[f.Code]; ok {
				gotRequired++
			}
		}
	}
	if requiredAI > 0 && gotRequired == 0 {
		return true, "关键字段全部为空，请重拍"
	}
	return false, ""
}

func applyRecognizedFields(rec *Record, recognized []RecognizedField) {
	byCode := map[string]RecognizedField{}
	for _, f := range recognized {
		byCode[f.Code] = f
	}
	for i := range rec.Fields {
		// 已经被人工修改过的字段，AI 不覆盖
		if rec.Fields[i].Source == "human-confirmed" || rec.Fields[i].Source == "human-edited" {
			continue
		}
		got, ok := byCode[rec.Fields[i].Code]
		if !ok {
			continue
		}
		rawValue := got.Value
		normalized := rawValue
		// choice 字段：把 AI 自由文本映射到模板列出的选项
		if rec.Fields[i].Kind == "choice" {
			normalized = normalizeChoiceValue(rawValue, rec.Fields[i].Options)
			if !optionContains(rec.Fields[i].Options, normalized) {
				rec.Fields[i].AIValue = rawValue
				rec.Fields[i].Value = ""
				rec.Fields[i].Source = "ai"
				rec.Fields[i].Confidence = got.Confidence
				rec.Fields[i].Reason = got.Reason
				if strings.TrimSpace(rec.Fields[i].Reason) == "" {
					rec.Fields[i].Reason = "AI 返回值未匹配模板选项，需人工复核"
				}
				rec.Fields[i].NeedsReview = true
				rec.Fields[i].Version++
				continue
			}
		}
		rec.Fields[i].AIValue = normalized
		rec.Fields[i].Value = normalized
		rec.Fields[i].Source = "ai"
		rec.Fields[i].Confidence = got.Confidence
		rec.Fields[i].Reason = got.Reason
		// 高置信度的 ai 字段不再要求人工复核
		rec.Fields[i].NeedsReview = got.Confidence < 0.85
		rec.Fields[i].Version++
	}
}

func optionContains(options []string, value string) bool {
	for _, o := range options {
		if o == value {
			return true
		}
	}
	return false
}

// normalizeChoiceValue — 把 AI 返回的自由文本对齐到 options 里的某个值
// 1) 完全匹配 → 直接用
// 2) 包含同义词 → 映射到对应选项
// 3) 都不匹配 → 返回原始值（前端会显示但 select 选不中）
// 否定 / 异常词 —— 命中其中任何一个,直接归到「异常」(若有)或「待复核」,
// 不再进入下方同义词包含匹配。修这种 bug:"不正常" 不能因为包含"正常"二字而被判正常。
var normalizeNegativeAndAbnormalCues = []string{
	"不正常", "不合格", "不通过", "不良", "不行", "不可", "不好", "不正确",
	"异常", "故障", "损坏", "报警", "告警", "破损", "破裂", "破碎", "裂纹", "缺失", "丢失", "失踪",
	"漏水", "渗漏", "短路", "跳闸", "烧毁", "焦糊", "霉味", "燃气", "刺激性",
	"超标", "存在问题", "有问题", "存在隐患", "未通过",
}

// 模糊词 —— 命中视为「待复核」(若选项有),不要硬归正常/异常。
var normalizeUncertainCues = []string{
	"看不清", "无法判定", "不确定", "拿不准", "需复核", "需要复核", "建议复核",
	"模糊", "不清楚", "辨认不清",
}

// "否定异常"的短语 —— 字面上虽然带"异常/报警/故障"字样,但语义是「没有发生」=正常。
// 这一步必须先于第 2 步的异常词扫描,否则 "无异常" 会因为包含 "异常" 被误判。
var normalizeNegatedAbnormalPhrases = []string{
	"无异常", "无报警", "无告警", "无故障", "无破损", "无缺失", "无损坏",
	"无漏水", "无渗漏", "无烧毁", "无问题", "无隐患", "无超标",
	"未发现异常", "未发现报警", "未发现故障", "未发现破损", "未发现漏水", "未发现问题",
	"没有异常", "没有报警", "没有故障", "没有破损", "没有问题", "没问题",
	"不存在异常", "不存在报警", "不存在故障", "不存在问题",
}

func normalizeChoiceValue(raw string, options []string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	// 1) 精确匹配最优先
	for _, opt := range options {
		if opt == v {
			return opt
		}
	}
	vLower := strings.ToLower(v)

	// 2a) 否定异常的短语优先 —— "无异常/未发现报警/没有故障"等,语义是正常。
	for _, neg := range normalizeNegatedAbnormalPhrases {
		if strings.Contains(vLower, strings.ToLower(neg)) {
			if optionContains(options, "正常") {
				return "正常"
			}
			if optionContains(options, "完好") {
				return "完好"
			}
			if optionContains(options, "无") {
				return "无"
			}
			if optionContains(options, "否") {
				return "否"
			}
			return raw
		}
	}

	// 2b) 否定 / 异常词扫一遍。命中就强归"异常"(没"异常"选项就归"待复核")。
	//    这一步必须先于同义词 Contains,否则 "不正常" 会因为包含 "正常" 被判正常。
	for _, neg := range normalizeNegativeAndAbnormalCues {
		if strings.Contains(vLower, strings.ToLower(neg)) {
			if optionContains(options, "异常") {
				return "异常"
			}
			// 破损家族:破损/破裂/损坏/裂纹/破碎 都归"破损"(若选项有)
			breakage := neg == "破损" || neg == "破裂" || neg == "损坏" ||
				strings.Contains(vLower, "裂纹") || strings.Contains(vLower, "破碎")
			if optionContains(options, "破损") && breakage {
				return "破损"
			}
			if optionContains(options, "缺失") && (neg == "缺失" || neg == "丢失" || neg == "失踪") {
				return "缺失"
			}
			if optionContains(options, "待复核") {
				return "待复核"
			}
			return raw // 没合适选项,留原值交人工
		}
	}

	// 3) 模糊词归"待复核"(若选项有)。
	for _, u := range normalizeUncertainCues {
		if strings.Contains(vLower, strings.ToLower(u)) {
			if optionContains(options, "待复核") {
				return "待复核"
			}
			return raw
		}
	}

	// 4) 同义词库(精简到正向词,反向词已在第 2 步处理)
	positiveSynonyms := map[string][]string{
		"正常": {"无问题", "无异常", "良好", "ok", "通过", "合格", "完好", "运行正常", "运转正常"},
		"是":  {"yes", "有", "true"},
		"否":  {"no", "无", "没有", "未发现", "false"},
		"无":  {"未发现", "没有", "none"},
		"有":  {"存在", "发现"},
		"完好": {"良好", "无破损", "无损坏"},
		"良好": {"完好", "无问题"},
	}
	for _, opt := range options {
		aliases, ok := positiveSynonyms[opt]
		if !ok {
			continue
		}
		for _, alias := range aliases {
			a := strings.ToLower(alias)
			if a == vLower || strings.Contains(vLower, a) {
				return opt
			}
		}
	}
	// 5) 返回原始值,交人工复核
	return raw
}

func buildDailyPreview(rec *Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】\n", rec.TemplateName)
	fmt.Fprintf(&b, "项目：%s · 点位：%s · 巡检员：%s\n", rec.Project, rec.PointName, rec.Inspector)
	fmt.Fprintf(&b, "时间：%s\n\n", rec.CreatedAt.Format("2006-01-02 15:04"))
	for _, f := range rec.Fields {
		v := strings.TrimSpace(f.Value)
		if v == "" {
			v = "（待填写）"
		}
		fmt.Fprintf(&b, "%s：%s\n", f.Label, v)
	}
	if rec.AISummary != "" {
		fmt.Fprintf(&b, "\n【AI 总结】\n%s", rec.AISummary)
	}
	if len(rec.AIRecommendations) > 0 {
		b.WriteString("\n\n【AI 建议】")
		for _, r := range rec.AIRecommendations {
			fmt.Fprintf(&b, "\n[%s] %s（依据：%s）", r.Priority, r.Text, r.Basis)
		}
	}
	return strings.TrimSpace(b.String())
}

func buildFallbackSummary(rec *Record) string {
	abnormal := []string{}
	for _, f := range rec.Fields {
		v := strings.TrimSpace(f.Value)
		if v == "" {
			continue
		}
		bad := false
		switch {
		case v == "异常" || v == "缺失" || v == "破损" || v == "故障":
			bad = true
		case v == "否": // 正向合规问答"否"=不合格（负向问的"否"=正常）
			bad = !isOccurrenceLabel(f.Label)
		case v == "是" || v == "有": // 负向问（是否有异响/漏水/报警发生）答"是/有"=异常
			bad = isOccurrenceLabel(f.Label)
		}
		if bad {
			abnormal = append(abnormal, f.Label)
		}
	}
	status := "正常"
	if len(abnormal) > 0 {
		status = "存在异常字段：" + strings.Join(abnormal, "、")
	}
	return fmt.Sprintf("%s 在 %s 完成 %s 巡检（兜底总结，AI 服务暂不可用）。状态：%s。",
		rec.Inspector, rec.CreatedAt.Format("2006-01-02 15:04"),
		rec.TemplateName, status)
}

func simplifyFieldsForSummary(fields []FieldValue) []map[string]string {
	out := []map[string]string{}
	for _, f := range fields {
		v := strings.TrimSpace(f.Value)
		if v == "" {
			continue
		}
		out = append(out, map[string]string{
			"label": f.Label,
			"value": v,
		})
	}
	return out
}

type assetBuildSpec struct {
	Key       string
	Name      string
	AssetType string
	FieldCode string
}

func buildAssets(rec *Record, now time.Time) []*AssetEntry {
	switch rec.TemplateID {
	case "zihan_energy":
		return buildZihanEnergyAssets(rec, now)
	case "zihan_daily":
		return buildZihanDailyAssets(rec, now)
	default:
		return []*AssetEntry{buildAsset(rec, now)}
	}
}

func buildZihanEnergyAssets(rec *Record, now time.Time) []*AssetEntry {
	specs := []assetBuildSpec{
		{Key: "z1_energy_meter", Name: "Z1能耗表", AssetType: "电表", FieldCode: "z1_reading"},
		{Key: "z2_energy_meter", Name: "Z2能耗表", AssetType: "电表", FieldCode: "z2_reading"},
		{Key: "z3_energy_meter", Name: "Z3能耗表", AssetType: "电表", FieldCode: "z3_reading"},
		{Key: "z4_energy_meter", Name: "Z4能耗表", AssetType: "电表", FieldCode: "z4_reading"},
		{Key: "living_water_meter", Name: "生活水表", AssetType: "水表", FieldCode: "living_water_reading"},
		{Key: "fire_water_meter", Name: "消防水表", AssetType: "水表", FieldCode: "fire_water_reading"},
	}
	assets := make([]*AssetEntry, 0, len(specs))
	for _, spec := range specs {
		field, _ := fieldByCode(rec.Fields, spec.FieldCode)
		assets = append(assets, buildAssetEntry(
			rec,
			now,
			spec.Key,
			spec.Name,
			spec.AssetType,
			readingAssetStatus(field, rec, spec.Name),
			readingAssetSummary(spec.Name, fieldValue(rec.Fields, spec.FieldCode), field),
		))
	}
	return assets
}

func buildZihanDailyAssets(rec *Record, now time.Time) []*AssetEntry {
	specs := []assetBuildSpec{
		{Key: "strong_room", Name: "强电井", AssetType: "综合巡检对象", FieldCode: "strong_room_01"},
		{Key: "distribution_box", Name: "配电箱", AssetType: "综合巡检对象", FieldCode: "distribution_box"},
		{Key: "distribution_box_inside", Name: "配电箱内部", AssetType: "综合巡检对象", FieldCode: "distribution_box_inside"},
		{Key: "weak_room", Name: "弱电机房", AssetType: "综合巡检对象", FieldCode: "weak_room"},
		{Key: "fire_pump_room", Name: "消防泵房", AssetType: "综合巡检对象", FieldCode: "fire_pump_room"},
	}
	assets := make([]*AssetEntry, 0, len(specs)+1)
	for _, spec := range specs {
		field, _ := fieldByCode(rec.Fields, spec.FieldCode)
		assets = append(assets, buildAssetEntry(
			rec,
			now,
			spec.Key,
			spec.Name,
			spec.AssetType,
			choiceAssetStatus(field, rec, spec.Name),
			choiceAssetSummary(spec.Name, fieldValue(rec.Fields, spec.FieldCode), field),
		))
	}

	tempField, _ := fieldByCode(rec.Fields, "temperature")
	humField, _ := fieldByCode(rec.Fields, "humidity")
	temp := fieldValue(rec.Fields, "temperature")
	humidity := fieldValue(rec.Fields, "humidity")
	assets = append(assets, buildAssetEntry(
		rec,
		now,
		"environment",
		"环境温湿度",
		"环境监测",
		environmentAssetStatus(temp, humidity, tempField, humField, rec),
		environmentAssetSummary(temp, humidity),
	))
	return assets
}

func buildAssetEntry(rec *Record, now time.Time, key, name, assetType, status, summary string) *AssetEntry {
	return &AssetEntry{
		ID:              assetIDFor(rec, key),
		ProjectCode:     sanitizeAssetIdent(rec.Project),
		Project:         rec.Project,
		PointID:         rec.PointID,
		TemplateID:      rec.TemplateID,
		AssetType:       assetType,
		AssetKey:        sanitizeAssetIdent(key),
		AssetName:       name,
		LastRecordID:    rec.ID,
		LastStatus:      status,
		StatusLevel:     statusLevel(status),
		StatusOrder:     statusOrder(status),
		LastSummary:     summary,
		LastInspectedAt: now,
		LastInspector:   rec.Inspector,
		LastPhotoPath:   firstImagePath(rec),
	}
}

func buildAsset(rec *Record, now time.Time) *AssetEntry {
	tpl, _ := templateByID(rec.TemplateID)
	name := firstNonEmpty(
		fieldValue(rec.Fields, "asset_no"),
		fieldValue(rec.Fields, "site"),
		rec.PointName,
	)
	assetKey := sanitizeAssetIdent(assetIdentFromRecord(rec))
	status := inferOverallStatus(rec)
	lastPhotoPath := ""
	if len(rec.Images) > 0 {
		lastPhotoPath = rec.Images[0].Path
	}
	return &AssetEntry{
		ID:              assetIDFor(rec, assetKey),
		ProjectCode:     sanitizeAssetIdent(rec.Project),
		Project:         rec.Project,
		PointID:         rec.PointID,
		TemplateID:      rec.TemplateID,
		AssetType:       tpl.AssetType,
		AssetKey:        assetKey,
		AssetName:       name,
		LastRecordID:    rec.ID,
		LastStatus:      status,
		StatusLevel:     statusLevel(status),
		StatusOrder:     statusOrder(status),
		LastSummary:     rec.AISummary,
		LastInspectedAt: now,
		LastInspector:   rec.Inspector,
		LastPhotoPath:   lastPhotoPath,
	}
}

// assetFieldCodeMap 给多资产模板返回「资产Key(已 sanitize) → 该资产关联字段码」。
// 单资产模板返回 nil（调用方把整条记录字段都归到那台资产）。
// Key 必须与 buildAssetEntry 里的 AssetKey 一致（sanitizeAssetIdent(spec.Key)），否则归属对不上。
func assetFieldCodeMap(rec *Record) map[string][]string {
	switch rec.TemplateID {
	case "zihan_energy":
		return map[string][]string{
			sanitizeAssetIdent("z1_energy_meter"):    {"z1_reading"},
			sanitizeAssetIdent("z2_energy_meter"):    {"z2_reading"},
			sanitizeAssetIdent("z3_energy_meter"):    {"z3_reading"},
			sanitizeAssetIdent("z4_energy_meter"):    {"z4_reading"},
			sanitizeAssetIdent("living_water_meter"): {"living_water_reading"},
			sanitizeAssetIdent("fire_water_meter"):   {"fire_water_reading"},
		}
	case "zihan_daily":
		return map[string][]string{
			sanitizeAssetIdent("strong_room"):             {"strong_room_01"},
			sanitizeAssetIdent("distribution_box"):        {"distribution_box"},
			sanitizeAssetIdent("distribution_box_inside"): {"distribution_box_inside"},
			sanitizeAssetIdent("weak_room"):               {"weak_room"},
			sanitizeAssetIdent("fire_pump_room"):          {"fire_pump_room"},
			sanitizeAssetIdent("environment"):             {"temperature", "humidity"},
		}
	}
	return nil
}

// buildRecordObservations 把一条已提交记录拆成「资产快照 + 字段观测」，供 §3 长期台账/趋势使用。
// 数值字段额外解析出 ValueNumber，便于后端算变化率/均值/阈值。
func buildRecordObservations(rec *Record, assets []*AssetEntry, t time.Time) ([]*AssetSnapshot, []*FieldObservation) {
	fieldMap := assetFieldCodeMap(rec) // nil = 单资产模板
	snaps := make([]*AssetSnapshot, 0, len(assets))
	obs := make([]*FieldObservation, 0, len(assets)*2)
	for _, a := range assets {
		snaps = append(snaps, &AssetSnapshot{
			AssetID:     a.ID,
			RecordID:    rec.ID,
			Status:      a.LastStatus,
			StatusLevel: a.StatusLevel,
			Summary:     a.LastSummary,
			Inspector:   rec.Inspector,
			CreatedAt:   t,
		})
		var codes []string
		if fieldMap == nil {
			for i := range rec.Fields {
				codes = append(codes, rec.Fields[i].Code)
			}
		} else {
			codes = fieldMap[a.AssetKey]
		}
		for _, code := range codes {
			f, _ := fieldByCode(rec.Fields, code)
			if f == nil || strings.TrimSpace(f.Value) == "" {
				continue
			}
			ob := &FieldObservation{
				AssetID:    a.ID,
				RecordID:   rec.ID,
				FieldKey:   f.Code,
				FieldLabel: f.Label,
				ValueText:  f.Value,
				Source:     f.Source,
				Confidence: f.Confidence,
				CreatedAt:  t,
			}
			if num, err := strconv.ParseFloat(strings.TrimSpace(f.Value), 64); err == nil {
				ob.ValueNumber = &num
			}
			obs = append(obs, ob)
		}
	}
	return snaps, obs
}

// backfillAssetSnapshots 启动时把已有的已提交记录补成快照/观测（幂等，靠唯一索引去重）。
func (s *Server) backfillAssetSnapshots() error {
	// 快照回填是跨租户维护任务,单租户过渡期按默认租户。
	records, err := s.store.ListRecords(defaultTenantID, 2000)
	if err != nil {
		return err
	}
	var allSnaps []*AssetSnapshot
	var allObs []*FieldObservation
	for _, rec := range records {
		if rec == nil || !rec.Submitted {
			continue
		}
		t := assetLedgerTime(rec)
		assets := buildAssets(rec, t)
		snaps, obs := buildRecordObservations(rec, assets, t)
		allSnaps = append(allSnaps, snaps...)
		allObs = append(allObs, obs...)
	}
	return s.store.WriteAssetSnapshots(allSnaps, allObs)
}

func assetID(rec *Record) string {
	return assetIDFor(rec, assetIdentFromRecord(rec))
}

func assetIDFor(rec *Record, assetKey string) string {
	return rec.Project + "::" + rec.TemplateID + "::" + sanitizeAssetIdent(assetKey)
}

func firstImagePath(rec *Record) string {
	if len(rec.Images) == 0 {
		return ""
	}
	return rec.Images[0].Path
}

func assetLedgerTime(rec *Record) time.Time {
	if rec.SubmittedAt != nil && !rec.SubmittedAt.IsZero() {
		return *rec.SubmittedAt
	}
	if !rec.UpdatedAt.IsZero() {
		return rec.UpdatedAt
	}
	return rec.CreatedAt
}

func readingAssetStatus(field *FieldValue, rec *Record, assetName string) string {
	val := ""
	if field != nil {
		val = strings.TrimSpace(field.Value)
	}
	if val == "" {
		return "待复核"
	}
	// AI 在 reason / NeedsReview / record-level recommendation 里报告异常 → 状态升级
	if hasAbnormalSignal(field, rec, assetName) {
		return "待复核"
	}
	return "正常"
}

func readingAssetSummary(name, value string, field *FieldValue) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return name + "本次未取得有效读数，需人工补录或复核。"
	}
	base := fmt.Sprintf("%s本次读数：%s。", name, value)
	if field != nil && strings.TrimSpace(field.Reason) != "" && containsAnomalyKeyword(field.Reason) {
		base += "AI 提示：" + strings.TrimSpace(field.Reason)
	}
	return base
}

func choiceAssetStatus(field *FieldValue, rec *Record, assetName string) string {
	val := ""
	if field != nil {
		val = strings.TrimSpace(field.Value)
	}
	switch val {
	case "":
		return "待复核"
	case "异常", "缺失", "破损", "故障", "是", "有":
		return "异常"
	}
	if hasAbnormalSignal(field, rec, assetName) {
		return "待复核"
	}
	return "正常"
}

func choiceAssetSummary(name, value string, field *FieldValue) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return name + "本次未填写状态，需人工复核。"
	}
	base := fmt.Sprintf("%s本次状态：%s。", name, value)
	if field != nil && strings.TrimSpace(field.Reason) != "" && containsAnomalyKeyword(field.Reason) {
		base += "AI 提示：" + strings.TrimSpace(field.Reason)
	}
	return base
}

func environmentAssetStatus(temp, humidity string, tField, hField *FieldValue, rec *Record) string {
	t := strings.TrimSpace(temp)
	h := strings.TrimSpace(humidity)
	if t == "" && h == "" {
		return "待复核"
	}
	// 数值越界视为异常（机房环境合理范围：5–35℃ / 0–90%）
	if v, err := strconv.ParseFloat(t, 64); err == nil {
		if v < 5 || v > 35 {
			return "异常"
		}
	}
	if v, err := strconv.ParseFloat(h, 64); err == nil {
		if v < 0 || v > 90 {
			return "异常"
		}
	}
	if hasAbnormalSignal(tField, rec, "环境温湿度") || hasAbnormalSignal(hField, rec, "环境温湿度") {
		return "待复核"
	}
	return "正常"
}

// hasAbnormalSignal: 字段或 record 级 AI 信号是否提示异常
//  1. field.Reason 含异常关键词 (识别失败/模糊/倒退/报警/超限/未识别…)
//  2. field.NeedsReview = true 但 value 已填 (AI 不确信)
//  3. record.AISummaryError 非空 (AI 总结失败)
//  4. record.AIRecommendations 中有 priority=high 且文本提到该资产名 (针对性告警)
func hasAbnormalSignal(field *FieldValue, rec *Record, assetName string) bool {
	if field != nil {
		if containsAnomalyKeyword(field.Reason) {
			return true
		}
		if field.NeedsReview && strings.TrimSpace(field.Value) != "" {
			return true
		}
	}
	if rec == nil {
		return false
	}
	if strings.TrimSpace(rec.AISummaryError) != "" {
		return true
	}
	for _, r := range rec.AIRecommendations {
		if strings.EqualFold(r.Priority, "high") && (assetName == "" || strings.Contains(r.Text, assetName)) {
			return true
		}
	}
	return false
}

func containsAnomalyKeyword(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, kw := range []string{
		"异常", "报警", "倒退", "失败", "无法", "模糊", "不清", "未识别",
		"识别失败", "错误", "超限", "不正常", "缺失", "可疑",
	} {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

func environmentAssetSummary(temp, humidity string) string {
	temp = firstNonEmpty(strings.TrimSpace(temp), "未填写")
	humidity = firstNonEmpty(strings.TrimSpace(humidity), "未填写")
	return fmt.Sprintf("本次环境读数：温度 %s，湿度 %s。", temp, humidity)
}

func recordTouchesAsset(rec *Record, asset *AssetEntry) bool {
	if asset == nil {
		return false
	}
	if asset.ID == assetID(rec) {
		return true
	}
	for _, candidate := range buildAssets(rec, rec.UpdatedAt) {
		if candidate.ID == asset.ID {
			return true
		}
	}
	return false
}

func isLegacyZihanAggregateAsset(a *AssetEntry) bool {
	if a == nil || a.Project != "紫菡雅集" {
		return false
	}
	switch a.TemplateID {
	case "zihan_energy":
		return !isZihanEnergyAssetKey(a.AssetKey)
	case "zihan_daily":
		return !isZihanDailyAssetKey(a.AssetKey)
	default:
		return false
	}
}

func isZihanEnergyAssetKey(key string) bool {
	switch sanitizeAssetIdent(key) {
	case "z1_energy_meter", "z2_energy_meter", "z3_energy_meter", "z4_energy_meter",
		"living_water_meter", "fire_water_meter":
		return true
	default:
		return false
	}
}

func isZihanDailyAssetKey(key string) bool {
	switch sanitizeAssetIdent(key) {
	case "strong_room", "distribution_box", "distribution_box_inside", "weak_room",
		"fire_pump_room", "environment":
		return true
	default:
		return false
	}
}

func inferOverallStatus(rec *Record) string {
	hasAbnormal := false
	hasUnfilled := false
	for _, f := range rec.Fields {
		v := strings.TrimSpace(f.Value)
		if v == "" {
			if f.Required {
				hasUnfilled = true
			}
			continue
		}
		switch {
		case v == "异常" || v == "缺失" || v == "破损" || v == "故障":
			hasAbnormal = true
		case v == "否":
			// 正向合规问（……完好/正常/有效/齐全/牢固/未过期）答"否" = 不合格；
			// 负向问（是否有异响…）答"否" = 正常，不算异常
			if !isOccurrenceLabel(f.Label) {
				hasAbnormal = true
			}
		case v == "是" || v == "有":
			// 负向问（是否有异响/有无异味/是否漏水报警）答"是/有" = 发生异常；
			// 正向合规问答"是"(完好/有效) = 正常
			if isOccurrenceLabel(f.Label) {
				hasAbnormal = true
			}
		}
	}
	if hasAbnormal {
		return "异常"
	}
	if hasUnfilled {
		return "待复核"
	}
	return "正常"
}

// isOccurrenceLabel 判断字段是否为"发生即异常"的负向问（如"是否有异响""电箱内有无异味"
// "是否漏水/报警"），这类答"是/有"才是异常；返回 false 表示正向合规问（"……完好/正常/有效/
// 齐全"），这类答"否"才是异常。注意区分"设备无异响"(正向)与"有无异味"(负向)。
func isOccurrenceLabel(label string) bool {
	if strings.Contains(label, "有无") || strings.Contains(label, "是否有") {
		return true
	}
	if strings.Contains(label, "无异") || strings.Contains(label, "无报警") || strings.Contains(label, "无漏") || strings.Contains(label, "无故障") {
		return false
	}
	for _, kw := range []string{"异常", "是否漏水", "是否报警", "有异响", "有异味"} {
		if strings.Contains(label, kw) {
			return true
		}
	}
	return false
}

func analysisToMap(resp *AnalyzeResponse) map[string]any {
	if resp == nil {
		return nil
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ===== 修改申请（审批流） =====

// handleCreateChangeRequest 创建一条 pending 申请。
// 入参：{ targetType: 'asset'|'record', targetId, patch{...}, reason }
// patch 内允许的 key 由 target_type 限定：
//
//	asset:  assetName / lastStatus / lastSummary
//	record: fields[{code,value}] / inspector / aiSummary
func (s *Server) handleCreateChangeRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType string         `json:"targetType"`
		TargetID   string         `json:"targetId"`
		Patch      map[string]any `json:"patch"`
		Reason     string         `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "missing_reason", "请填写修改理由")
		return
	}
	if req.TargetType != "asset" && req.TargetType != "record" {
		writeError(w, http.StatusBadRequest, "bad_target_type", "targetType 必须是 asset 或 record")
		return
	}
	if strings.TrimSpace(req.TargetID) == "" {
		writeError(w, http.StatusBadRequest, "missing_target_id", "缺少 targetId")
		return
	}
	// 校验目标存在
	switch req.TargetType {
	case "asset":
		if _, err := s.store.GetAsset(s.tenantForRequest(r), req.TargetID); err != nil {
			writeError(w, http.StatusNotFound, "asset_not_found", "资产不存在")
			return
		}
	case "record":
		if _, ok := s.requireRecordAccess(w, r, req.TargetID, false); !ok {
			return
		}
	}
	if len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "empty_patch", "patch 不能为空")
		return
	}
	cr := &ChangeRequest{
		ID:          newID("cr"),
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
		Patch:       req.Patch,
		Reason:      req.Reason,
		Status:      "pending",
		RequestedBy: s.currentUserName(r),
		RequestedAt: time.Now(),
	}
	if err := s.store.CreateChangeRequest(cr); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	s.recordOperation(r, "change_request.create", req.TargetType, req.TargetID, map[string]any{
		"requestId": cr.ID,
		"reason":    cr.Reason,
		"patch":     cr.Patch,
	})
	s.notifyChangeRequestCreated(cr)
	writeJSON(w, http.StatusCreated, cr)
}

// handleListChangeRequests 列表查询。
// 巡检员只能看自己提的；主管能看全部。
// 参数：?status=pending&targetType=asset&mine=1
func (s *Server) handleListChangeRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := ChangeRequestFilter{
		Status:     q.Get("status"),
		TargetType: q.Get("targetType"),
	}
	if !s.hasSupervisorAccess(r) || q.Get("mine") == "1" {
		filter.RequestedBy = s.currentUserName(r)
	}
	list, err := s.store.ListChangeRequests(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

// handleChangeRequestRoutes 分发 /api/change-requests/{id}/{action}
func (s *Server) handleChangeRequestRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/change-requests/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		cr, err := s.store.GetChangeRequest(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "申请不存在")
			return
		}
		if cr.RequestedBy != s.currentUserName(r) && !s.hasSupervisorAccess(r) {
			writeError(w, http.StatusForbidden, "forbidden", "无权查看该修改申请")
			return
		}
		writeJSON(w, http.StatusOK, cr)
	case len(parts) == 2 && parts[1] == "approve" && r.Method == http.MethodPost:
		s.handleApproveChangeRequest(w, r, id)
	case len(parts) == 2 && parts[1] == "reject" && r.Method == http.MethodPost:
		s.handleRejectChangeRequest(w, r, id)
	case len(parts) == 2 && parts[1] == "withdraw" && r.Method == http.MethodPost:
		s.handleWithdrawChangeRequest(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "")
	}
}

var errChangeRequestNotPending = errors.New("change request is not pending")

func (s *Server) approveChangeRequest(id, reviewer, note string) (*ChangeRequest, error) {
	if store, ok := s.store.(*SQLiteStore); ok {
		return s.approveChangeRequestSQL(store, id, reviewer, note)
	}
	cr, err := s.store.GetChangeRequest(id)
	if err != nil {
		return nil, err
	}
	if cr.Status != "pending" {
		return nil, errChangeRequestNotPending
	}
	if err := s.applyChangeRequest(cr); err != nil {
		return nil, err
	}
	now := time.Now()
	cr.Status = "approved"
	cr.ReviewedBy = reviewer
	cr.ReviewedAt = &now
	cr.ReviewNote = note
	cr.AppliedAt = &now
	if err := s.store.UpdateChangeRequest(cr); err != nil {
		return nil, err
	}
	return cr, nil
}

type sqlReadWriter interface {
	sqlExecutor
	sqlQueryer
}

func (s *Server) approveChangeRequestSQL(store *SQLiteStore, id, reviewer, note string) (*ChangeRequest, error) {
	tx, err := store.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cr, err := getChangeRequestExec(tx, id)
	if err != nil {
		return nil, err
	}
	if cr.Status != "pending" {
		return nil, errChangeRequestNotPending
	}
	cleanupFiles, err := s.applyChangeRequestSQL(tx, cr)
	if err != nil {
		if cleanupFiles != nil {
			cleanupFiles()
		}
		return nil, err
	}
	now := time.Now()
	cr.Status = "approved"
	cr.ReviewedBy = reviewer
	cr.ReviewedAt = &now
	cr.ReviewNote = note
	cr.AppliedAt = &now
	if err := updateChangeRequestExec(tx, cr); err != nil {
		if cleanupFiles != nil {
			cleanupFiles()
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		if cleanupFiles != nil {
			cleanupFiles()
		}
		return nil, err
	}
	// 提交后再做异常闭环回写：onAssetResolvedNormal 走独立连接，须在事务提交后执行
	s.resolveNormalAfterChange(cr)
	return cr, nil
}

// resolveNormalAfterChange 审批通过、资产恢复正常后闭环待整改任务（事务路径提交后调用）。
func (s *Server) resolveNormalAfterChange(cr *ChangeRequest) {
	switch cr.TargetType {
	case "asset":
		// 变更审批流暂按默认租户;change_requests 域租户化时随 cr 串入真实租户。
		if a, err := s.store.GetAsset(defaultTenantID, cr.TargetID); err == nil && a != nil && a.LastStatus == "正常" {
			s.onAssetResolvedNormal(a.ID)
		}
	case "record":
		rec, err := s.store.GetRecord(defaultTenantID, cr.TargetID)
		if err != nil || rec == nil {
			return
		}
		for _, asset := range buildAssets(rec, time.Now()) {
			if a, err := s.store.GetAsset(defaultTenantID, asset.ID); err == nil && a != nil && a.LastRecordID == rec.ID && a.LastStatus == "正常" {
				s.onAssetResolvedNormal(a.ID)
			}
		}
	}
}

func (s *Server) applyChangeRequestSQL(exec sqlReadWriter, cr *ChangeRequest) (func(), error) {
	var cleanupFiles func()
	switch cr.TargetType {
	case "asset":
		name, _ := cr.Patch["assetName"].(string)
		status, _ := cr.Patch["lastStatus"].(string)
		summary, _ := cr.Patch["lastSummary"].(string)
		if name == "" && status == "" && summary == "" {
			return nil, fmt.Errorf("asset patch 为空")
		}
		return nil, updateAssetMetaExec(exec, defaultTenantID, cr.TargetID, name, status, summary)
	case "record":
		rec, err := getRecordByID(exec, cr.TargetID)
		if err != nil {
			return nil, err
		}
		changed := false
		if fields, ok := cr.Patch["fields"].([]any); ok {
			for _, item := range fields {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				code, _ := m["code"].(string)
				val, _ := m["value"].(string)
				if code == "" {
					continue
				}
				f, _ := fieldByCode(rec.Fields, code)
				if f == nil || f.Value == val {
					continue
				}
				f.Value = val
				f.Source = "human-edited"
				f.NeedsReview = false
				f.Version++
				changed = true
			}
		}
		if v, ok := cr.Patch["inspector"].(string); ok && strings.TrimSpace(v) != "" && v != rec.Inspector {
			rec.Inspector = strings.TrimSpace(v)
			rec.InspectorUserID = ""
			changed = true
		}
		if v, ok := cr.Patch["aiSummary"].(string); ok && v != rec.AISummary {
			rec.AISummary = v
			changed = true
		}
		if ai, ok := cr.Patch["addImages"].(map[string]any); ok {
			tmpDir, _ := ai["tmpDir"].(string)
			var ids []string
			if arr, ok := ai["imageIds"].([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						ids = append(ids, s)
					}
				}
			}
			if tmpDir != "" {
				moved, err := s.adoptTmpImages(rec.ID, tmpDir, ids)
				if err != nil {
					return nil, fmt.Errorf("补交照片失败: %w", err)
				}
				if len(moved) > 0 {
					rec.Images = append(rec.Images, moved...)
					cleanupFiles = func() {
						rollbackAdoptedImages(tmpDir, moved)
					}
					changed = true
				}
			}
		}
		if !changed {
			return cleanupFiles, nil
		}
		rec.Report = buildDailyPreview(rec)
		if err := updateRecordExec(exec, rec); err != nil {
			return cleanupFiles, err
		}
		for _, asset := range buildAssets(rec, time.Now()) {
			if a, err := getAssetByID(exec, asset.ID); err == nil && a != nil && a.LastRecordID == rec.ID {
				if err := updateAssetMetaExec(exec, defaultTenantID, asset.ID, "", asset.LastStatus, asset.LastSummary); err != nil {
					return cleanupFiles, err
				}
			}
		}
		return cleanupFiles, nil
	default:
		return nil, fmt.Errorf("不支持的 targetType: %s", cr.TargetType)
	}
}

func rollbackAdoptedImages(tmpDir string, moved []ImageInfo) {
	if tmpDir == "" || len(moved) == 0 {
		return
	}
	_ = os.MkdirAll(tmpDir, 0755)
	for _, img := range moved {
		if img.Path == "" {
			continue
		}
		_ = os.Rename(img.Path, filepath.Join(tmpDir, filepath.Base(img.Path)))
	}
}

func (s *Server) handleApproveChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "approval_review") {
		return
	}
	var req struct {
		ReviewNote string `json:"reviewNote"`
	}
	_ = decodeJSON(r, &req) // 备注可选
	cr, err := s.approveChangeRequest(id, s.currentUserName(r), req.ReviewNote)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "申请不存在")
		return
	}
	if errors.Is(err, errChangeRequestNotPending) {
		writeError(w, http.StatusConflict, "bad_status", "当前状态不可审批")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approve_failed", err.Error())
		return
	}
	s.recordOperation(r, "change_request.approve", cr.TargetType, cr.TargetID, map[string]any{
		"requestId": cr.ID,
		"note":      req.ReviewNote,
	})
	s.notifyChangeRequestReviewed(cr, "已通过")
	writeJSON(w, http.StatusOK, cr)
}

func (s *Server) handleRejectChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "approval_review") {
		return
	}
	var req struct {
		ReviewNote string `json:"reviewNote"`
	}
	_ = decodeJSON(r, &req)
	if strings.TrimSpace(req.ReviewNote) == "" {
		writeError(w, http.StatusBadRequest, "missing_note", "拒绝时请填写理由")
		return
	}
	cr, err := s.store.GetChangeRequest(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "申请不存在")
		return
	}
	if cr.Status != "pending" {
		writeError(w, http.StatusConflict, "bad_status", "当前状态不可审批："+cr.Status)
		return
	}
	now := time.Now()
	cr.Status = "rejected"
	cr.ReviewedBy = s.currentUserName(r)
	cr.ReviewedAt = &now
	cr.ReviewNote = req.ReviewNote
	if err := s.store.UpdateChangeRequest(cr); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	s.recordOperation(r, "change_request.reject", cr.TargetType, cr.TargetID, map[string]any{
		"requestId": cr.ID,
		"note":      req.ReviewNote,
	})
	s.notifyChangeRequestReviewed(cr, "已驳回")
	writeJSON(w, http.StatusOK, cr)
}

func (s *Server) handleWithdrawChangeRequest(w http.ResponseWriter, r *http.Request, id string) {
	cr, err := s.store.GetChangeRequest(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "申请不存在")
		return
	}
	if cr.Status != "pending" {
		writeError(w, http.StatusConflict, "bad_status", "仅 pending 状态可撤回")
		return
	}
	if cr.RequestedBy != s.currentUserName(r) && !s.hasSupervisorAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "只能撤回自己提的申请")
		return
	}
	cr.Status = "withdrawn"
	now := time.Now()
	cr.ReviewedAt = &now
	if err := s.store.UpdateChangeRequest(cr); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cr)
}

// handleUploadDraftPhotos 申请阶段的补交图片暂存接口。
// multipart：files=[]，最多 6 张。存到 storage/tmp_classify/cr_xxx/，
// 返回 {tmpDir, files: [{id, fileName, ...}]}；申请通过时再 adoptTmpImages 搬到 record。
func (s *Server) handleUploadDraftPhotos(w http.ResponseWriter, r *http.Request) {
	// 必须提供身份才能写盘（防匿名 DOS 写满磁盘）。
	// 优先 session 登录身份，其次 X-User-Name header。
	_, sessionOK := s.userFromSessionToken(s.tokenFromRequest(r))
	if !sessionOK && strings.TrimSpace(r.Header.Get("X-User-Name")) == "" {
		writeError(w, http.StatusUnauthorized, "missing_user", "请先登录后再上传")
		return
	}
	role := s.userRole(r)
	if role != roleInspector && role != roleSupervisor && role != roleManager && role != roleAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "仅巡检员/主管可上传申请图片")
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "请选择图片")
		return
	}
	if len(files) > 6 {
		files = files[:6]
	}
	tmpDirID := newID("cr")
	tmpDir := filepath.Join(s.storageDir, "tmp_classify", tmpDirID)
	saved := []ImageInfo{}
	for _, h := range files {
		img, err := saveMultipartFile(tmpDir, h, 15<<20)
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
			return
		}
		img.ContentHash = hashFile(img.Path)
		saved = append(saved, img)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"tmpDir": tmpDir,
		"files":  saved,
	})
}

// applyChangeRequest 把审批通过的 patch 落库。
// 当前存储层同时兼容 SQLite + MySQL，单条 UPDATE 由存储实现保证原子性。
func (s *Server) applyChangeRequest(cr *ChangeRequest) error {
	switch cr.TargetType {
	case "asset":
		name, _ := cr.Patch["assetName"].(string)
		status, _ := cr.Patch["lastStatus"].(string)
		summary, _ := cr.Patch["lastSummary"].(string)
		if name == "" && status == "" && summary == "" {
			return fmt.Errorf("asset patch 为空")
		}
		_, err := s.store.UpdateAssetMeta(defaultTenantID, cr.TargetID, name, status, summary)
		if err == nil && status == "正常" {
			// 修改审批通过、资产改回正常 → 异常闭环回写
			s.onAssetResolvedNormal(cr.TargetID)
		}
		return err
	case "record":
		rec, err := s.store.GetRecord(defaultTenantID, cr.TargetID)
		if err != nil {
			return err
		}
		changed := false
		if fields, ok := cr.Patch["fields"].([]any); ok {
			for _, item := range fields {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				code, _ := m["code"].(string)
				val, _ := m["value"].(string)
				if code == "" {
					continue
				}
				f, _ := fieldByCode(rec.Fields, code)
				if f == nil {
					continue
				}
				if f.Value == val {
					continue
				}
				f.Value = val
				f.Source = "human-edited"
				f.NeedsReview = false
				f.Version++
				changed = true
			}
		}
		if v, ok := cr.Patch["inspector"].(string); ok && strings.TrimSpace(v) != "" && v != rec.Inspector {
			rec.Inspector = strings.TrimSpace(v)
			rec.InspectorUserID = ""
			changed = true
		}
		if v, ok := cr.Patch["aiSummary"].(string); ok && v != rec.AISummary {
			rec.AISummary = v
			changed = true
		}
		// 补交照片：addImages = { tmpDir, imageIds }
		if ai, ok := cr.Patch["addImages"].(map[string]any); ok {
			tmpDir, _ := ai["tmpDir"].(string)
			var ids []string
			if arr, ok := ai["imageIds"].([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						ids = append(ids, s)
					}
				}
			}
			if tmpDir != "" {
				moved, err := s.adoptTmpImages(rec.ID, tmpDir, ids)
				if err != nil {
					return fmt.Errorf("补交照片失败: %w", err)
				}
				if len(moved) > 0 {
					rec.Images = append(rec.Images, moved...)
					changed = true
				}
			}
		}
		if !changed {
			return nil
		}
		rec.Report = buildDailyPreview(rec)
		if err := s.store.UpdateRecord(rec); err != nil {
			return err
		}
		// 同步资产 last_status / last_summary。
		for _, asset := range buildAssets(rec, time.Now()) {
			if a, err := s.store.GetAsset(defaultTenantID, asset.ID); err == nil && a != nil && a.LastRecordID == rec.ID {
				_, _ = s.store.UpdateAssetMeta(defaultTenantID, asset.ID, "", asset.LastStatus, asset.LastSummary)
				// 字段级审批通过后资产重算为正常 → 异常闭环回写（与「标记正常」「资产级审批」「复检」一致）
				if asset.LastStatus == "正常" {
					s.onAssetResolvedNormal(asset.ID)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("不支持的 targetType: %s", cr.TargetType)
	}
}
