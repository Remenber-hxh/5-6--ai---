package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ===== 项目管理接口 =====
//
// 读口给管理角色(派任务、看台账都要选项目);写口只给系统管理员 ——
// 项目归属直接决定谁能看到哪些数据,是权限动作。

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProjects(s.tenantForRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	// 【名单也按项目范围裁】和台账同一套口径,见 limitProjectsToVisible。
	list = s.limitProjectsToVisible(r, list)
	if list == nil {
		list = []*Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": list})
}

// errUnknownProject 项目名不在项目表里。
var errUnknownProject = errors.New("unknown_project")

// errProjectDisabled 项目存在但已停用。
var errProjectDisabled = errors.New("project_disabled")

// checkProjectRegistered 这个项目名是不是台账里真实存在的、启用中的项目。
//
// 【为什么必须在后端拦】项目名是业务表的关联键 —— assets.project 存的就是
// 这个字符串,没有外键。打错一个字("紫涵"vs"紫菡")建出来的设备会落进一个
// 不存在的项目,然后【谁都看不见它】:
//   - 项目管理页按 projects 表列,列不出这个名字
//   - 台账按人的项目范围裁(limitAssetsToVisibleProjects),裁掉它
//   - 项目的"设备数"按名字聚合,也数不到它
//
// 全程没有任何报错,只表现成"我明明建了一台设备,它不见了"。
// 前端把输入框换成下拉能挡住大部分,但挡不住直接调接口的,
// 所以真正的闸口在这里。
//
// 【停用的也拦】停用项目不参与可见范围计算(ListUserProjectNames 只返回启用中的),
// 往里建设备是同一种"建完就看不见"。
func (s *Server) checkProjectRegistered(tenantID, name string) error {
	name = strings.TrimSpace(name)
	list, err := s.store.ListProjects(tenantID)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p == nil || strings.TrimSpace(p.Name) != name {
			continue
		}
		if p.Disabled {
			return errProjectDisabled
		}
		return nil
	}
	return errUnknownProject
}

type projectUpsertRequest struct {
	Name     string `json:"name"`
	Note     string `json:"note"`
	Disabled bool   `json:"disabled"`
	// BotIndex 这个项目的提醒发到第几个企微群。
	//
	// 【指针:不传 = 这次不改】前端有两处会 PUT 这条接口(改备注/停用、以及选群),
	// 用零值的话"改个备注"会把已经选好的群一起清成 0,而界面上什么都不显示。
	BotIndex *int `json:"botIndex"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req projectUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "项目名称不能为空")
		return
	}
	tenantID := s.tenantForRequest(r)
	// 【先查重名】项目名是业务表的关联键,建了同名的第二条,成员挂到哪一条
	// 都对不上台账。数据库唯一约束会拦,但那里报出来的是一句英文。
	existing, err := s.store.ListProjects(tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	for _, p := range existing {
		if p.Name == name {
			writeError(w, http.StatusConflict, "project_exists", "同名项目已存在")
			return
		}
	}
	p := &Project{TenantID: tenantID, Name: name, Note: strings.TrimSpace(req.Note)}
	if err := s.store.CreateProject(p); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	s.recordOperation(r, "project.create", "project", p.ID, map[string]any{"name": p.Name})
	writeJSON(w, http.StatusOK, map[string]any{"project": p})
}

// handleProjectRoutes 处理 /api/projects/<id>
func (s *Server) handleProjectRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理项目")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "未匹配的项目路由")
		return
	}
	if r.Method == http.MethodDelete {
		s.handleDeleteProject(w, r, id)
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 PUT / DELETE")
		return
	}
	var req projectUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// 刻意不接受改名:业务表按名字关联,改了名台账就认不出来了。
	err := s.store.UpdateProjectMeta(s.tenantForRequest(r), id, strings.TrimSpace(req.Note), req.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project_not_found", "项目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	if req.BotIndex != nil {
		if err := s.setProjectBot(w, r, id, *req.BotIndex); err != nil {
			return // setProjectBot 已经写过响应
		}
	}
	s.recordOperation(r, "project.update", "project", id, map[string]any{"disabled": req.Disabled})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setProjectBot 保存"这个项目的提醒发到第几个群"。
//
// 【只认真实存在的群】随手填一个 9,提醒就会静默地发不出去 —— 而后台显示
// 保存成功。所以在这里就拦住,让人当场看到。0 是合法值:交回环境变量决定。
//
// 返回 error 非空 = 已经写过响应,调用方直接 return。
func (s *Server) setProjectBot(w http.ResponseWriter, r *http.Request, id string, botIndex int) error {
	if botIndex < 0 {
		writeError(w, http.StatusBadRequest, "bad_bot_index", "群序号不能是负数")
		return errBadBotIndex
	}
	if botIndex > 0 && !s.knownBotIndex(botIndex) {
		writeError(w, http.StatusBadRequest, "unknown_bot",
			fmt.Sprintf("服务器上没有配置第 %d 个群 —— 先在 secrets 里配好它的地址", botIndex))
		return errBadBotIndex
	}
	if err := s.store.SetProjectBotIndex(s.tenantForRequest(r), id, botIndex); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project_not_found", "项目不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		}
		return err
	}
	s.recordOperation(r, "project.set_bot", "project", id, map[string]any{"botIndex": botIndex})
	return nil
}

var errBadBotIndex = errors.New("bad bot index")

// handleListWeWorkBots —— GET /api/wework/bots
//
// 后台"给项目选群"那个下拉要用。
//
// 【绝不返回 webhook】这个接口是给浏览器的,返回值会进控制台、进截图、
// 进任何一次"帮我看看"的粘贴 —— 而 webhook 等价于往那个群发消息的权限。
// 只给序号、名称(名称里是项目名,不是地址)和"地址配了没有"。
func (s *Server) handleListWeWorkBots(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]any, 0, len(s.weworkBots))
	for _, b := range s.weworkBots {
		out = append(out, map[string]any{
			"index": b.Index,
			"name":  b.Name,
			// envProjects:服务器上 WEWORK_BOT_[N]_PROJECTS 写的那几个项目。
			// 后台没给项目选群时按它走,所以要显示出来让人知道默认是什么。
			"envProjects": b.Projects,
			"ready":       b.Client != nil && b.Client.Enabled(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

// ===== 某人的项目归属:GET / PUT /api/users/<id>/projects =====

func (s *Server) handleUserProjects(w http.ResponseWriter, r *http.Request, userID string) {
	tenantID := s.tenantForRequest(r)
	switch r.Method {
	case http.MethodGet:
		ids, err := s.store.ListUserProjectIDs(tenantID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projectIds": ids})
	case http.MethodPut:
		var req struct {
			ProjectIDs []string `json:"projectIds"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		// 【确认这个人在本租户】否则跨租户传个 userID 就能改别家的人员归属。
		target, err := s.store.GetUser(userID)
		if err != nil || target == nil || tenantOrDefault(target.TenantID) != tenantID {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		if err := s.store.SetUserProjects(tenantID, userID, req.ProjectIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		s.recordOperation(r, "user.projects", "user", userID, map[string]any{
			"count": len(req.ProjectIDs),
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET / PUT")
	}
}
