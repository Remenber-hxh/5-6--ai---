package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 密码下限。管理员给别人重置密码那条路(handleResetUserPassword)用的是 6 位,
// 这里跟着它 —— 同一个系统里两套长度要求,只会让人以为哪边坏了。
const minPasswordLen = 6

// ===== 自助改密码:POST /api/auth/me/password =====

func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromSessionToken(s.tokenFromRequest(r))
	if !ok {
		// 本地免鉴权那条路进得来但没有"自己"可言,拒掉比改错人好
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.OldPassword = strings.TrimSpace(req.OldPassword)
	req.NewPassword = strings.TrimSpace(req.NewPassword)

	if len(req.NewPassword) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "weak_password",
			"新密码至少 "+strconv.Itoa(minPasswordLen)+" 位")
		return
	}
	if req.NewPassword == req.OldPassword {
		writeError(w, http.StatusBadRequest, "same_password", "新密码不能和当前密码相同")
		return
	}
	if req.NewPassword == defaultAdminPass {
		// 默认密码是公开写在代码里的,改成它等于没改
		writeError(w, http.StatusBadRequest, "weak_password", "不能使用系统默认密码")
		return
	}

	// 【必须验旧密码】否则任何拿到会话的人(比如借用了没锁屏的手机)
	// 都能把账号密码改掉、把本人锁在外面。
	// 复用登录的失败计数:同一 IP 反复猜旧密码会被锁,和登录一个口径。
	guardKey := loginGuardKey("chpwd:"+user.Username, r)
	if locked, retryAfter := s.loginGuard.locked(guardKey); locked {
		writeError(w, http.StatusTooManyRequests, "locked",
			"尝试次数过多,请 "+strconv.Itoa((retryAfter+59)/60)+" 分钟后再试")
		return
	}
	if err := s.store.VerifyUserPassword(user.ID, req.OldPassword); err != nil {
		if errors.Is(err, errInvalidCredentials) {
			s.loginGuard.fail(guardKey)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "当前密码不正确")
			return
		}
		writeError(w, http.StatusInternalServerError, "verify_failed", err.Error())
		return
	}
	s.loginGuard.success(guardKey)

	if err := s.store.SetUserPassword(user.ID, req.NewPassword); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	// 改完踢掉【所有】会话,包括当前这个。
	//
	// 改密码的常见动机就是"密码可能泄露了" —— 只留当前会话有效、别处照旧
	// 登着,等于没解决问题。代价是本人要重新登一次,这个代价值得付。
	if err := s.store.DeleteUserSessions(user.ID); err != nil {
		// 密码【已经改成功了】,这一步失败不能回报成"改密码失败" ——
		// 那会让用户拿旧密码反复重试。记日志,继续走成功响应。
		log.Printf("WARN: 用户 %s 改密码后清理会话失败: %v", user.Username, err)
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		UserID: user.ID, ActorName: user.DisplayName, Action: "change_password",
		TargetType: "user", TargetID: user.ID,
		Detail: map[string]any{"username": user.Username},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		// 前端据此把本地登录态清掉并回登录页,而不是等下一个请求 401
		"reauth": true,
	})
}

// ===== 自助注册:POST /api/auth/register(免鉴权)=====

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		Code        string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Password = strings.TrimSpace(req.Password)
	code := normalizeRegistrationCode(req.Code)

	if req.Username == "" || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "账号和姓名都不能为空")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "weak_password",
			"密码至少 "+strconv.Itoa(minPasswordLen)+" 位")
		return
	}
	if code == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "请填写注册码")
		return
	}

	// 猜码限流。码本身是 8 位 31 进制(约 8.5×10^11 种),暴力破解不现实,
	// 但没有限流的话,一个脚本可以一直试到日志被刷爆。按 IP 计。
	guardKey := loginGuardKey("register", r)
	if locked, retryAfter := s.loginGuard.locked(guardKey); locked {
		writeError(w, http.StatusTooManyRequests, "locked",
			"尝试次数过多,请 "+strconv.Itoa((retryAfter+59)/60)+" 分钟后再试")
		return
	}

	rc, err := s.store.GetRegistrationCode(code)
	if err != nil {
		// 【"查不到"和"查不了"必须分开】原来这里把任何错误都说成"注册码不存在":
		// 数据库连不上时,巡检员看到的是"你的码不对",于是去找管理员;管理员
		// 只会反复重发新码,而问题根本不在码上。而且这种情况还会白白计入猜码限流。
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("ERROR: 注册时查注册码失败: %v", err)
			writeError(w, http.StatusInternalServerError, "lookup_failed",
				"服务暂时不可用,请稍后重试")
			return
		}
		s.loginGuard.fail(guardKey)
		writeError(w, http.StatusBadRequest, "invalid_code", "注册码不存在,请检查后重试")
		return
	}
	if err := rc.Usable(time.Now()); err != nil {
		// 这类失败是"码本身的问题",不是在猜码,不计入限流 ——
		// 否则一个班组拿着同一个过期码来试,会把整栋楼的 IP 锁掉
		writeError(w, http.StatusBadRequest, "invalid_code", err.Error())
		return
	}
	s.loginGuard.success(guardKey)

	role, ok, err := s.store.GetRoleByCode(rc.RoleCode)
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "bad_code",
			"注册码上的角色已不存在,请找管理员换一个")
		return
	}

	user := &User{
		ID:           newID("user"),
		Username:     req.Username,
		DisplayName:  req.DisplayName,
		RoleID:       role.ID,
		RoleCode:     role.Code,
		DepartmentID: rc.DepartmentID,
		TenantID:     rc.TenantID,
		Status:       "active",
	}
	if err := s.store.CreateUser(user, req.Password); err != nil {
		// 用户名唯一冲突是最常见的失败,单独说清楚 ——
		// 回一句笼统的"注册失败"会让人以为是系统坏了,然后反复点
		if isDuplicateKeyErr(err) {
			writeError(w, http.StatusConflict, "username_taken", "这个账号已被使用,换一个")
			return
		}
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}

	// 【建完用户才扣次数】反过来的话,建用户失败(比如重名)会白白吃掉一次名额,
	// 一个 5 次的码被三个人打错字就废了。
	if err := s.store.ConsumeRegistrationCode(rc.ID); err != nil {
		// 极端并发下这里可能刚好被别人用完。用户已经建好了,不回滚 ——
		// 把人建出来又删掉更糟。记日志,管理员能看到超发了一个。
		_ = s.store.CreateOperationLog(&OperationLog{
			ActorName: req.Username, Action: "register_code_overrun",
			TargetType: "registration_code", TargetID: rc.ID,
			Detail: map[string]any{"code": rc.Code, "reason": err.Error()},
		})
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		UserID: user.ID, ActorName: user.DisplayName, Action: "register",
		TargetType: "user", TargetID: user.ID,
		Detail: map[string]any{"username": user.Username, "code": rc.Code, "role": role.Code},
	})

	// 注册完直接给会话,不用再手动登一次
	_, session, err := s.store.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needLogin": true})
		return
	}
	s.setAuthCookie(w, r, session.Token, int(sessionTTL.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      user,
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
		"perms":     s.permsForRole(user.RoleCode),
	})
}

// ===== 注册码管理(仅管理员)=====

func (s *Server) handleListRegistrationCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := s.store.ListRegistrationCodes(s.tenantForRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) handleCreateRegistrationCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RoleCode      string `json:"roleCode"`
		DepartmentID  string `json:"departmentId"`
		Note          string `json:"note"`
		MaxUses       int    `json:"maxUses"`
		ExpiresInDays int    `json:"expiresInDays"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.RoleCode) == "" {
		req.RoleCode = roleInspector
	}
	if _, ok, _ := s.store.GetRoleByCode(req.RoleCode); !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "角色无效")
		return
	}
	// 管理员角色不发注册码。管理权限必须由管理员一个一个建,
	// 一张能自助注册出管理员的码要是流出去,整个租户的数据就没门槛了。
	if req.RoleCode == roleAdmin {
		writeError(w, http.StatusBadRequest, "role_not_allowed",
			"管理员账号不能用注册码创建,请在「用户与权限」里手动新建")
		return
	}
	if req.MaxUses < 0 {
		req.MaxUses = 0
	}
	expiresAt := ""
	if req.ExpiresInDays > 0 {
		expiresAt = time.Now().AddDate(0, 0, req.ExpiresInDays).Format(time.RFC3339)
	}
	rc := &RegistrationCode{
		ID:           newID("regcode"),
		Code:         newRegistrationCode(),
		TenantID:     s.tenantForRequest(r),
		RoleCode:     req.RoleCode,
		DepartmentID: strings.TrimSpace(req.DepartmentID),
		Note:         strings.TrimSpace(req.Note),
		MaxUses:      req.MaxUses,
		ExpiresAt:    expiresAt,
		CreatedBy:    s.currentUserName(r),
		CreatedAt:    nowStamp(),
	}
	if err := s.store.CreateRegistrationCode(rc); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": rc})
}

func (s *Server) handleDisableRegistrationCode(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Disabled *bool `json:"disabled"`
	}
	_ = decodeJSON(r, &req)
	disabled := true // 不带 body 就是停用
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	if err := s.store.SetRegistrationCodeDisabled(s.tenantForRequest(r), id, disabled); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "注册码不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "disabled": disabled})
}

// /api/registration-codes/<id>/... —— 动态段走这里(路由表只登记精确路径)
func (s *Server) handleRegistrationCodeRoutes(w http.ResponseWriter, r *http.Request) {
	// 【这一层必须自己查权限】路由表里的 guardAdmin 只作用于精确路径,
	// 前缀路由是在这里分权的 —— 漏了就等于把停用/启用对所有登录用户开放。
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理注册码")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/registration-codes/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "缺少注册码 ID")
		return
	}
	id := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch {
	case sub == "disable" && r.Method == http.MethodPost:
		s.handleDisableRegistrationCode(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	}
}
