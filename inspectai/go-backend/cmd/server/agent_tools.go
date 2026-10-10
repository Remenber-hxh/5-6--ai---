package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===== 管理 AI 的工具层 =====
//
// 在这之前,聊天是"后端不管你问什么,都跑同一组统计塞进提示词"。后果是:
// 问题一旦超出那几组聚合(比如"K07 上个月异常几次"),模型没有数据,
// 而提示词又要求它别拒答 —— 只能编。
//
// 这里把决定权交给模型:告诉它有哪些工具可用,它自己决定调哪个、传什么参数,
// 后端执行完把真实数据还给它,再让它作答。**数字从此有出处。**
//
// 【只放只读工具】派单仍然走 /act + 人点确认,不进这个清单。让模型能读全库
// 是一回事,让它能写是另一回事 —— 这个规模下,读放开、写守住是划算的。
//
// 【工具本身早就写好了】management_ai.go 里那几个 toolXxx 函数是为这一步准备的,
// 其中三个此前一次都没被调用过。这里做的是把它们接上,不是从零实现。

const (
	// 最多几轮工具调用。DeepSeek 官方文档自己写着 function calling
	// "may result in looped calls" —— 必须有硬上限,不能指望模型自己收敛。
	agentMaxToolRounds = 3
	// 单个工具结果喂回模型时的字符上限。一条巡检记录展开有几十个字段,
	// 三轮下来能把上下文撑爆,成本和延迟都不可控。
	agentToolResultMaxRune = 3000

	// 【时间预算】整条聊天请求要在 nginx 的 60 秒之内回来,否则浏览器拿到的是 504,
	// 设计好的兜底回答一次都用不上。工具路径每一轮最多等 agentRoundBudget,
	// 并且始终给不带工具的老路留出 agentFallbackReserve —— 工具路径失败时还来得及退回去。
	managementChatBudget = 50 * time.Second
	agentRoundBudget     = 25 * time.Second
	agentFallbackReserve = 15 * time.Second
	agentRoundMin        = 6 * time.Second // 剩这么点就别再开一轮了

	// 模型一轮里最多并行调几个工具。不设上限的话,一轮能要 20 次查询。
	agentMaxCallsPerRound = 4
)

// agentEvidence 模型这一轮实际查过的东西 —— 回答下面的"依据"只从这里来。
//
// 【为什么不再按关键词猜】原来的依据是另外拼的:问句带"异常/风险"、回答又没点名
// 设备时,就挂上风险最高的那台。工具路径下模型答的是它自己查回来的 A 设备,
// 依据却指向 B 设备 —— "数字有出处"这件事在最后一步被打破了。
type agentEvidence struct {
	AssetID  string
	RecordID string
}

// agentToolSpecs 返回给模型的工具清单(OpenAI 兼容的 JSON Schema)。
//
// 描述写得具体,是因为模型靠它判断"该不该调这个" —— 写"查询资产"它会到处乱调,
// 写清楚"问某一台设备的逐次巡检历史时用"它才知道边界。
func agentToolSpecs() []map[string]any {
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	tool := func(name, desc string, props map[string]any, required []string) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters": map[string]any{
					"type":       "object",
					"properties": props,
					"required":   required,
				},
			},
		}
	}
	return []map[string]any{
		// 【这个工具必须放第一个,而且描述要写"先用它"】
		// 用户说的是"K07""kt-7"这种简写,而其它工具要的是完整 id
		// (项目::模板::编号)。没有这个工具时,模型只能从看板数据的
		// topRiskAssets 里找 —— 而那份【只有高风险设备】。正常设备压根不在里面,
		// 于是模型要么从手上那几台里硬挑一个(答错设备),要么去猜完整 id
		// (碰巧猜对就答出来、没猜就说"查不到")—— 同一台设备问两次两个结果。
		// 线上实测踩到的正是这个。
		tool("find_asset",
			"按名字或编号找设备,拿到它的完整 id。"+
				"【问到任何具体设备时都先调这个】用户说的是'K07''kt-7''3号电梯'这种简写,"+
				"而其它工具需要完整 id。支持模糊匹配,大小写和连字符都能对上。"+
				"返回多台时,挑名字最接近的那台继续,或者反问用户是哪一台。",
			map[string]any{
				"keyword": strProp("设备名或编号的片段,如 K07 / kt-7 / DT01"),
			},
			[]string{"keyword"}),

		tool("get_asset_history",
			"查某一台设备的逐次巡检历史(时间、状态、巡检人、结论)。"+
				"问'这台设备上个月异常几次''最近几次什么情况''什么时候出的问题'时用。"+
				"assetId 用 find_asset 返回的完整 id,不要自己拼、也不要从看板数据里猜。",
			map[string]any{
				"assetId": strProp("设备的完整 id,形如 项目::模板::编号"),
				"limit":   map[string]any{"type": "integer", "description": "取最近几条,默认 20"},
			},
			[]string{"assetId"}),

		tool("get_record_detail",
			"查某一次巡检的完整明细:每个检查项填了什么、谁确认的、有没有照片。"+
				"问'那次具体哪项不合格''这条记录是谁填的'时用。",
			map[string]any{
				"recordId": strProp("巡检记录 id"),
			},
			[]string{"recordId"}),

		tool("compare_asset_periods",
			"把一台设备的两个时间段做对比,看是好转还是恶化。"+
				"问'这台比上个月怎么样''有没有改善'时用。",
			map[string]any{
				"assetId":  strProp("设备的完整 id"),
				"current":  strProp("当前时间段,如 30d / 7d"),
				"previous": strProp("对比时间段,如 30d"),
			},
			[]string{"assetId"}),

		// 【描述必须和返回值严格一致】这个工具名叫 status_events,但它返回的
		// 【只有统计】:总次数、正常/异常计数、哪些字段反复异常、最近一次时间。
		// 它没有逐条事件、没有每次的日期。
		//
		// 第一版我按名字想当然写成"什么时候从正常变异常" —— 模型信了,
		// 于是拿这里给的字段名配上自己编的日期,答出"7月1日、7月4日因某项异常",
		// 而真实数据是 7/2、7/5。工具描述许诺了它给不出的东西,模型就会把缺口补上。
		tool("get_status_events",
			"查一台设备在某个时间范围内的【统计】:巡检总次数、正常/异常各几次、"+
				"哪几个检查项反复出问题、最近一次巡检时间。"+
				"【注意:这个工具不返回逐次的日期】要具体哪一天什么状态,用 get_asset_history。"+
				"问'这台一共出过几次问题''老是哪一项不合格'时用。",
			map[string]any{
				"assetId":  strProp("设备的完整 id"),
				"rangeKey": strProp("时间范围,如 30d / 90d,默认 30d"),
			},
			[]string{"assetId"}),
	}
}

// execAgentTool 执行一个工具调用。
//
// 【白名单 switch,不做反射派发】多一个工具就在这里多一行 —— 看得见、审得动。
// 反射或 map 派发写起来短,但"模型能调到什么"就散在各处了,这是安全边界,
// 不该为了少写几行把它藏起来。
func (s *Server) execAgentTool(r *http.Request, project, name, rawArgs string) (any, *agentEvidence, error) {
	var args map[string]any
	if strings.TrimSpace(rawArgs) != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			// 模型偶尔会吐出不合法的 JSON。把错误【原样告诉它】而不是中断整轮 ——
			// 它下一轮通常能自己改对,比直接失败给用户看强。
			return nil, nil, fmt.Errorf("参数不是合法 JSON: %v", err)
		}
	}
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	num := func(k string, def, max int) int {
		if f, ok := args[k].(float64); ok && f > 0 {
			if int(f) > max {
				return max // 模型要 10000 条也只给这么多 —— 结果本来就会被截断
			}
			return int(f)
		}
		return def
	}
	tenant := s.tenantForRequest(r)
	vis := s.visibilityFor(r)

	// 【项目范围对 AI 同样生效】页面上过滤掉了但一问 AI 就说出来,
	// 等于开了一扇后门,而且是最容易被发现、最难解释的那种。
	if vis.Blocked {
		return nil, nil, fmt.Errorf("当前账号未分配项目,查不到数据")
	}
	// 单台设备的三个工具:先确认这台设备在他能看的项目里。
	assetInScope := func(id string) error {
		asset, err := s.store.GetAsset(tenant, id)
		if err != nil || asset == nil {
			return fmt.Errorf("设备不存在: %s", id)
		}
		if !vis.allowsProject(asset.Project) {
			// 【和"不存在"给同样的话】说"没权限"就等于确认了这台设备存在,
			// 模型会把这句转述给用户,反而把别的项目有什么透出去。
			return fmt.Errorf("设备不存在: %s", id)
		}
		return nil
	}

	switch name {
	case "find_asset":
		kw := str("keyword")
		if kw == "" {
			return nil, nil, fmt.Errorf("缺少 keyword")
		}
		if len(vis.Projects) == 1 && project == "" {
			// 只属于一个项目:直接锁定,他问"K7"就只在这个项目里找
			project = vis.Projects[0]
		}
		if project != "" && !vis.allowsProject(project) {
			return nil, nil, fmt.Errorf("查不到项目: %s", project)
		}
		// 【先按范围筛、再计数】原来是全租户找完、截成 10 台、再按项目裁 ——
		// 裁剪时把 count 改成了截断后的条数,旁边的说明却还写着"匹配到 15 台",
		// 模型照着 count 答"一共 10 台"。
		found, err := s.findAssetsForAgent(tenant, project, kw, vis.allowsProject)
		if err != nil {
			return nil, nil, err
		}
		return limitAgentAssetsToProjects(found, vis), nil, nil

	case "get_asset_history":
		id := str("assetId")
		if id == "" {
			return nil, nil, fmt.Errorf("缺少 assetId")
		}
		// 先确认这台设备属于当前租户【且在他的项目范围内】—— 工具是模型驱动的,
		// 它可能拿到任何字符串。不校验的话,一个猜对的 id 就能读到别家的数据。
		if err := assetInScope(id); err != nil {
			return nil, nil, err
		}
		snaps, hErr := s.toolGetAssetHistory(id, num("limit", 20, 50))
		if hErr != nil {
			return nil, nil, hErr
		}
		return normalizeHistoryForModel(snaps), &agentEvidence{AssetID: id}, nil

	case "get_record_detail":
		id := str("recordId")
		if id == "" {
			return nil, nil, fmt.Errorf("缺少 recordId")
		}
		if len(vis.Projects) > 0 {
			rec, err := s.store.GetRecord(tenant, id)
			if err != nil || rec == nil || !vis.allowsProject(rec.Project) {
				return nil, nil, fmt.Errorf("记录不存在: %s", id)
			}
		}
		out, err := s.toolGetRecordDetail(tenant, id)
		if err != nil {
			return nil, nil, err
		}
		return out, &agentEvidence{RecordID: id}, nil

	case "compare_asset_periods":
		id := str("assetId")
		if id == "" {
			return nil, nil, fmt.Errorf("缺少 assetId")
		}
		if err := assetInScope(id); err != nil {
			return nil, nil, err
		}
		cur := str("current")
		if cur == "" {
			cur = "30d"
		}
		prev := str("previous")
		if prev == "" {
			prev = "30d"
		}
		out, err := s.toolCompareAssetPeriods(id, cur, prev)
		if err != nil {
			return nil, nil, err
		}
		return out, &agentEvidence{AssetID: id}, nil

	case "get_status_events":
		id := str("assetId")
		if id == "" {
			return nil, nil, fmt.Errorf("缺少 assetId")
		}
		if err := assetInScope(id); err != nil {
			return nil, nil, err
		}
		rk := str("rangeKey")
		if rk == "" {
			rk = "30d"
		}
		out, err := s.toolGetStatusEvents(id, rk)
		if err != nil {
			return nil, nil, err
		}
		return out, &agentEvidence{AssetID: id}, nil
	}
	return nil, nil, fmt.Errorf("未知工具: %s", name)
}

// agentToolResultJSON 把工具结果压成给模型看的字符串。
// 超长就截断并【明说截断了】—— 不说的话模型会把残缺数据当成全部,
// 得出"只有 3 次异常"这种基于半截数据的结论。
func agentToolResultJSON(v any, err error) string {
	if err != nil {
		b, _ := json.Marshal(map[string]any{"error": err.Error()})
		return string(b)
	}
	b, mErr := json.Marshal(v)
	if mErr != nil {
		return `{"error":"结果无法序列化"}`
	}
	out := string(b)
	if len([]rune(out)) > agentToolResultMaxRune {
		return truncate(out, agentToolResultMaxRune) +
			`  ...(结果过长已截断,如需完整数据请缩小范围再查)`
	}
	return out
}

// ===== 工具调用循环 =====
//
// agentChat 跑一轮完整的"问 → 模型决定查什么 → 执行 → 再问 → 作答"。
//
// 【三道兜底,缺一不可】DeepSeek 官方文档写着 function calling 当前版本不稳定,
// 可能陷入循环调用或返回空响应。所以:
//
//	一、最多 agentMaxToolRounds 轮,超了就要求它用现有信息直接作答;
//	二、任何一步出错(包括模型返回空),整轮放弃,由调用方回退到不带工具的老路 ——
//	    那条路一直是好的,不能因为新功能不稳就把聊天整个搞挂;
//	三、工具执行的错误【不中断】,原样作为结果喂回去 —— 模型下一轮通常能自己改对
//	    (比如 assetId 拼错了),比直接失败给用户看强。
//
// 【时间】deadline 是整条聊天请求的截止时刻。每开一轮前先看剩多少:
// 不够一轮、或者开了这一轮就没时间退回老路了,就直接放弃工具路径。
//
// 返回 (回复文本, 用过的工具名, 查过的东西, 是否成功)。不成功时调用方走老路。
func (s *Server) agentChat(
	r *http.Request, systemPrompt, contextJSON, question, project string,
	history []map[string]any, deadline time.Time,
) (string, []string, []agentEvidence, bool) {
	if s.analyticsClient == nil {
		return "", nil, nil, false
	}
	messages := []map[string]any{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": "[当前看板数据 JSON]\n" + contextJSON},
	}
	for _, h := range history {
		role, _ := h["role"].(string)
		if role != "user" {
			role = "assistant" // 发给模型时用标准角色名;前端那套 user/ai 只在前端用
		}
		text, _ := h["text"].(string)
		if strings.TrimSpace(text) == "" {
			continue
		}
		messages = append(messages, map[string]any{"role": role, "content": text})
	}
	messages = append(messages, map[string]any{"role": "user", "content": question})

	used := []string{}
	evidence := []agentEvidence{}
	tools := agentToolSpecs()

	for round := 0; round <= agentMaxToolRounds; round++ {
		budget := time.Until(deadline) - agentFallbackReserve
		if budget < agentRoundMin {
			log.Printf("WARN: agent 第 %d 轮前剩余时间不足(%s),回退无工具路径", round, budget.Round(time.Second))
			return "", used, evidence, false
		}
		if budget > agentRoundBudget {
			budget = agentRoundBudget
		}
		payload := map[string]any{"messages": messages}
		if round < agentMaxToolRounds {
			payload["tools"] = tools // 最后一轮不再给工具:逼它用手上的信息作答
		}
		resp, err := s.analyticsClient.ChatTools(payload, budget)
		if err != nil {
			log.Printf("WARN: agent 工具轮次 %d 调用失败,回退无工具路径: %v", round, err)
			return "", used, evidence, false
		}
		switch resp["finish"] {
		case "stop":
			reply, _ := resp["reply"].(string)
			if strings.TrimSpace(reply) == "" {
				// 空响应是官方点名的失败模式之一,别把空白交给用户
				log.Printf("WARN: agent 第 %d 轮返回空回复,回退", round)
				return "", used, evidence, false
			}
			return reply, used, evidence, true

		case "tool_calls":
			calls, _ := resp["toolCalls"].([]any)
			if len(calls) == 0 {
				return "", used, evidence, false
			}
			// 【assistant 那条要原样带回去】OpenAI 协议要求 tool 结果必须
			// 跟在发起它的 assistant 消息之后,且 tool_call_id 对得上。
			if am, ok := resp["assistantMessage"].(map[string]any); ok {
				messages = append(messages, am)
			}
			for i, c := range calls {
				call, _ := c.(map[string]any)
				name, _ := call["name"].(string)
				args, _ := call["arguments"].(string)
				id, _ := call["id"].(string)
				// 【超出上限的也要回一条】协议要求每个 tool_call_id 都有结果,
				// 少一条模型那边就对不上;回一句"这一轮查太多了"让它下一轮收着点。
				if i >= agentMaxCallsPerRound {
					messages = append(messages, map[string]any{
						"role": "tool", "tool_call_id": id,
						"content": agentToolResultJSON(nil, fmt.Errorf("这一轮查询太多,已跳过;请先用已有结果作答或少查几项")),
					})
					continue
				}
				out, ev, execErr := s.execAgentTool(r, project, name, args)
				if execErr != nil {
					log.Printf("INFO: agent 工具 %s 执行失败(将把错误回给模型): %v", name, execErr)
				} else {
					used = append(used, name)
					if ev != nil {
						evidence = append(evidence, *ev)
					}
				}
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": id,
					"content":      agentToolResultJSON(out, execErr),
				})
			}

		default:
			// finish == "error" 或未知
			if msg, _ := resp["message"].(string); msg != "" {
				log.Printf("WARN: agent 轮次 %d 返回错误,回退: %s", round, msg)
			}
			return "", used, evidence, false
		}
	}
	// 轮数用尽还没给出答案 —— 正是官方说的"循环调用"那种情况
	log.Printf("WARN: agent 工具轮次用尽(%d 轮)仍未作答,回退", agentMaxToolRounds)
	return "", used, evidence, false
}

// MANAGEMENT_CHAT_SYSTEM_HINT — 工具路径的系统提示。
//
// 【为什么不复用 Python 那份】那份是为"一次性给你全部数据"写的,里面有大量
// "上下文 JSON 各字段含义"的说明。工具路径下模型是【自己去取】数据的,
// 那些说明反而会让它以为手上已经有全部东西、懒得调工具。
//
// 这份只讲工具路径特有的:有工具就去查、怎么查、查到的东西怎么用。
// 两条路都要守的规矩在 managementChatSharedRules,拼在最后。
const MANAGEMENT_CHAT_SYSTEM_HINT = `你是「智巡」设施巡检系统管理后台的 AI 助手,服务于巡检主管。

你手上有一份看板概览数据(整体计数、高风险资产、重复问题、待复核、巡检员质量、
数值漂移、任务概览),另外还有几个工具可以查更细的东西。

【有工具就去查】问到单台设备的逐次历史、某条记录的明细、
某台设备的时间段对比、状态变化时间时,**先调对应的工具**,拿到真实数据再答。
工具报错或查不到,就如实说"这台设备我这儿查不到",并指出该去后台哪一页看。

【日期只认结构化字段】工具返回的每条历史都带 date / time 字段,那才是准的。
结果里的叙述文字(summaryText)可能自带日期,那些日期【不可信】,不要引用。

【不许把两个工具的结果凑成一条事实】比如统计工具给了"某项反复异常",
历史工具给了"某几天异常" —— 除非同一条数据里同时有日期和字段,
否则不要写成"X 月 X 日因某项异常"。宁可分开说两句。

【设备 id 从哪来】用户说的是"K07""kt-7"这种简写。**先调 find_asset 拿到完整 id**,
再用那个 id 调其它工具。【绝对不要自己拼 id、也不要只在看板数据里找】——
看板里的 topRiskAssets 只有高风险设备,正常设备不在里面,在那儿找不到不等于设备不存在。
find_asset 返回空才说明真的没有这台设备。
find_asset 有时会返回 nearMatches(编号相近的候选,有猜测成分):只有一台时可以按它
回答,但【必须写出完整编号】让用户能发现认错;多台时列出来反问是哪一台。
数据来自工具的,「依据」里点明是哪台设备/哪次巡检。

` + managementChatSharedRules

// managementChatSharedRules 管理聊天两条路(工具主路、不带工具的备用路)都要守的规矩,只在这里写一份。
//
// 【为什么只能有一份】原来两条路各写一份,写着写着就不一样了:一键派单的动作提议规则
// 只加进了其中一份,走另一条路时派单按钮就没了;字数要求一份 50-120、一份 50-90。
// 备用路由 ai-service 拼提示词,这份随请求一起发过去(payload.sharedRules),
// ai-service 那边只留"看板数据各字段是什么意思"这类它自己特有的说明。
const managementChatSharedRules = `【数字不能编,这条压过一切】凡是具体的数字、次数、日期、人名、设备编号,
只能来自给你的数据(看板数据,以及工具返回的结果),**绝不能凭印象给**。
数据里没有的,一律不给具体值,如实说"这里查不到",并指出该去后台哪一页看 ——
指路不是拒答,它比编一个数有用得多。一个编出来的数字会让主管不再相信系统里的所有数字。

【回答里不许出现内部字段名和 id】这些是给程序看的,不是给人看的,「依据」里也一样:
  · 不要写 topRiskAssets / repeatedIssues / numericDrift / pendingApprovals / riskLevel 这类英文字段名
  · 不要写 会议中心::elevator_no_room::KT-5 这种完整 id,只说"KT-5";不要写 rec_xxx、user_xxx
  · 不要写 elevator_no_room 这种模板代号,要说"无机房电梯";字段说中文名("按钮显示")
  · 风险等级翻成中文:danger=高风险、warning=需关注、normal=正常、repair=维修中
说人话:"repeatedIssues 为空" → "最近 30 天没有反复出问题的设备"。

【本系统的话题都答,领域知识也可以答】巡检计划、记录、台账、审批、设备健康、用户权限、
操作日志、系统配置都算相关,别回"请问与巡检相关的问题"。"灭火器多久检查""电梯防夹装置的标准"
这类通用维保规范问题,用专业知识正常回答,可以点明对应国标/规程名称 —— 这不算编造,
编造指的是编本系统里的台账数字、设备名、人员。只有和设施巡检完全无关的(天气、闲聊、写代码)
才回"请问与巡检管理相关的问题"。

输出:
1. 第一句直接回答问题,最关键处用 **…** 加粗(全文只加粗一处)。
2. 另起一行写「依据:」,跟 1-2 条短句,每条一个关键数字或事实,不复述结论里已说过的数字。
3. 全文 50-120 字(不含下面的动作提议块),除那一处加粗外不用其它 markdown。

动作提议(仅在确有必要时附,其余情况绝不附):
- 你不能直接改数据、审批或派单;但当某台设备反复异常、尚未闭环、适合派一次现场复查时
  (包括回答"今天优先处理什么""重点关注哪些设备"),在正文之后另起一行附【一个】提议块,
  主管点确认后系统才会执行:
<<ACTION>>
{"type":"create_recheck_task","assetId":"设备完整 id","asset":"设备可读编号","assignee":"责任人(不确定就省略此项)","dueAt":"YYYY-MM-DD(不确定就省略)","reason":"一句话复查理由"}
<<END>>
- assetId 原样照抄数据里那台设备的完整 id(工具 find_asset 返回的 id,或看板数据里高风险设备的 assetId)。
  【动作块是给程序读的,不显示给人】这里必须写完整 id —— 台账里同名设备不少,只写编号会派错设备;
  正文里仍然只说可读编号。
- assignee / dueAt 不知道就省略那一项,绝不编造;reason 用中文一句话。
- 问复核率、趋势、谁没看图这类问题时,不附动作块。`

// normalizeHistoryForModel 把快照整理成"日期无歧义"的形状再交给模型。
//
// 【为什么必须做这一步】快照的 summary 是一整段 AI 生成的话,里面【自带日期】,
// 而那个日期和快照本身的 createdAt 可能对不上 —— 实测七月那批数据里,
// createdAt 是 7月5日 12:49、正文却写着"07月04日21:49",整整差 15 小时,
// 正好是太平洋时区与东八区的时差(那批是早期模拟数据,生成时用了本机时区)。
//
// 模型读到两个日期时会挑正文里那个(它更像"事件发生时间"),于是答出来的日期
// 整体偏一天。这不是模型在编 —— 它读的就是数据里写着的东西。
//
// 所以:给每条加一个明确的 date 字段(东八区,和数据库口径一致),
// 并把 summary 改名成 summaryText 且明说其中日期不可信。
// 与其叮嘱模型"别信正文",不如让正确答案更容易拿到。
func normalizeHistoryForModel(snaps []*AssetSnapshot) map[string]any {
	items := make([]map[string]any, 0, len(snaps))
	for _, sn := range snaps {
		if sn == nil {
			continue
		}
		items = append(items, map[string]any{
			"date":      dayStamp(sn.CreatedAt),
			"time":      sn.CreatedAt.In(cnLoc).Format("2006-01-02 15:04"),
			"status":    sn.Status,
			"inspector": sn.Inspector,
			"recordId":  sn.RecordID,
			// 保留正文(里面有"哪一项不合格"这种有用信息),但改个名字并警告
			"summaryText": sn.Summary,
		})
	}
	return map[string]any{
		"count": len(items),
		"items": items,
		"_note": "日期一律以每条的 date / time 字段为准。" +
			"summaryText 是历史生成的叙述文字,其中提到的日期可能与实际记录时间不符,不要引用它里面的日期。",
	}
}

// findAssetsForAgent 按关键词模糊找设备,返回给模型用的精简结果。
//
// 【为什么在 Go 里筛而不是写 SQL LIKE】ListAssets 没有 LIMIT,拿回来的是这个
// 租户的全部设备(几十台量级),不存在"窗口截断导致漏匹配"的问题 ——
// 而那正是这个项目里反复出现的 bug 形状。全量在手,匹配规则也能写得宽容些。
//
// 匹配做了归一化:去掉连字符/空格、统一大写。用户打"kt-7""KT7""kt 7"
// 都要能对上"KT-7" —— 现场用手机打字,不该因为一个连字符查不到。
// findAssetsForAgent 按关键词找设备。project 非空时【只在该项目内找】。
//
// 【为什么必须按项目过滤】聊天的上下文数据是按项目筛过的,而这个工具原来搜的是
// 整个租户 —— 于是在"会议中心"的对话里问 K01,可能答出另一栋楼的 K01。
// 各楼的编号是各自排的,重名很常见(asset_identity.go 里记着这件事),
// 而"挑名字最接近的那台"这条规则对两台同名设备完全无效。
//
// allow 是这个账号的项目范围(nil = 不限)。【必须在计数之前筛】—— 见 execAgentTool。
func (s *Server) findAssetsForAgent(tenantID, project, keyword string, allow func(string) bool) (map[string]any, error) {
	norm := func(x string) string {
		return strings.ToUpper(strings.NewReplacer("-", "", "_", "", " ", "", "－", "").Replace(x))
	}
	kw := norm(keyword)
	if kw == "" {
		return nil, fmt.Errorf("关键词为空")
	}
	all, err := s.store.ListAssets(tenantID)
	if err != nil {
		return nil, err
	}
	item := func(a *AssetEntry) map[string]any {
		return map[string]any{
			// id 是给后续工具用的,不该出现在回答里(提示词里点明了)
			"id":            a.ID,
			"name":          a.AssetName,
			"type":          a.AssetType, // 中文类型名,不是模板代号
			"project":       a.Project,
			"currentStatus": a.LastStatus,
		}
	}
	kwHasDigits := onlyDigits(kw) != ""
	exact, partial, near := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for _, a := range all {
		if a == nil {
			continue
		}
		if project != "" && a.Project != project {
			continue
		}
		if allow != nil && !allow(a.Project) {
			continue
		}
		nName, nKey := norm(a.AssetName), norm(a.AssetKey)
		if nName == "" && nKey == "" {
			continue
		}
		switch {
		case nName == kw || nKey == kw:
			exact = append(exact, item(a))
		case strings.Contains(nName, kw) || strings.Contains(nKey, kw):
			// 【子串命中也可能是别的设备】"KT7" 是 "KT70" 的子串,但那是两台。
			// 带了数字就要求数字对上,否则降级成候选让模型确认 ——
			// 原来子串直接进 assets,模型当成确定答案,问 KT-7 答的是 KT-70。
			//
			// 但关键词【没有数字】时(搜 "DT" 这种前缀、或按类型搜)是正常的
			// 宽泛检索,本来就该返回多台,不该降级 —— 第一版漏了这个条件,
			// 搜 "DT" 一台都进不了确定命中。
			if !kwHasDigits || digitsEqual(kw, nName) || digitsEqual(kw, nKey) {
				partial = append(partial, item(a))
			} else {
				near = append(near, item(a))
			}
		case looseAssetMatch(kw, nName) || looseAssetMatch(kw, nKey):
			near = append(near, item(a))
		}
	}
	// 精确 > 包含:用户打"KT-7"时,"KT-7"要压过"KT-70"
	out := append(append([]map[string]any{}, exact...), partial...)
	if len(out) > 0 {
		// 【count 要在截断【前】算】否则 40 台里返回 10 台,count 报 10,
		// 模型就会理直气壮地说"一共 10 台"。截断了必须说出来 ——
		// agentToolResultJSON 那边就是这么做的,这里当初漏了。
		total := len(out)
		res := map[string]any{"count": total}
		if total > 10 {
			out = out[:10]
			res["truncated"] = true
			res["_note"] = "匹配到 " + strconv.Itoa(total) + " 台,这里只列出前 10 台。" +
				"回答涉及总数时用 count,不要数下面这个列表的条数。"
		}
		res["assets"] = out
		return res, nil
	}
	// 【只有在完全没有把握的匹配时才给相近候选】
	// 这一层是为"用户打 K7、设备实际叫 KT-7"准备的。它有猜的成分,
	// 所以必须标出来让模型确认,不能当成命中直接用 —— 答错设备比查不到更糟。
	if len(near) > 0 {
		note := "没有完全匹配的设备,以下是编号相近的候选(有猜测成分)。" +
			"只有一台候选时,可以按它回答,但【必须在回答里写出完整编号】," +
			"让用户能发现认错了;多台候选时列出来反问用户指的是哪一台。"
		nearTotal := len(near)
		if nearTotal > 10 {
			near = near[:10]
			note += " 共有 " + strconv.Itoa(nearTotal) + " 台相近,这里只列前 10 台。"
		}
		return map[string]any{
			"count": 0, "nearMatchCount": nearTotal,
			"nearMatches": near, "_note": note,
		}, nil
	}
	return map[string]any{
		"count": 0,
		"_note": "没有找到匹配的设备,连编号相近的也没有。台账里确实没有这台,可以如实告诉用户。",
	}, nil
}

// digitsEqual 两个编号的数字部分是否相同(忽略前导零)。
// 编号里的数字是设备身份 —— KT-7 和 KT-70 是两台设备,不是同一台的两种写法。
func digitsEqual(a, b string) bool {
	da, db := trimLeadingZeros(onlyDigits(a)), trimLeadingZeros(onlyDigits(b))
	return da != "" && da == db
}

func onlyDigits(x string) string {
	var sb strings.Builder
	for _, r := range x {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// looseAssetMatch 判断"用户打的简写"是不是指这台设备。
//
// 真实场景:用户打「K7」,设备实际叫「KT-7」。归一化后是 K7 vs KT7 ——
// K7 不是 KT7 的子串(K 后面是 T 不是 7),所以严格匹配对不上。线上实测踩到的。
//
// 【不能简单放松成"字符都出现过"】那样 K7 会把 KT-17、KX-7 全匹上。
// 用【数字部分完全相同】作锚点:设备编号里的数字是它的身份,字母是分类前缀。
//
//	K7  vs KT-7  → 数字 7 == 7、字母 K 是 KT 的子序列  → 相近 ✓
//	K7  vs KT-17 → 数字 7 != 17                        → 不匹配 ✓
//	K7  vs KX-7  → 数字相同、字母 K 是 KX 的子序列      → 相近(确实可能是它,交给模型确认)
func looseAssetMatch(kw, target string) bool {
	digits := func(x string) string {
		var b strings.Builder
		for _, r := range x {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	letters := func(x string) string {
		var b strings.Builder
		for _, r := range x {
			if r < '0' || r > '9' {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	// 【必须去掉前导零再比】K07 的数字是 "07",KT-7 的是 "7",按字符串比对不上 ——
	// 而 K07 正是工具描述里举的例子。现场编号补不补零全看当初谁录的。
	kd, td := trimLeadingZeros(digits(kw)), trimLeadingZeros(digits(target))
	// 两边都必须有数字且相同 —— 数字才是编号的身份
	if kd == "" || kd != td {
		return false
	}
	// 字母部分:两个方向都算 —— 用户可能打少了(K7 → KT-7),也可能打多了
	// (KT-7 → K7)。【单向会漏】第一版只判前者,于是本机那台真名叫 K7 的设备
	// 在用户打 KT-7 时匹配不上。
	// KX 对 KT 两个方向都不成立,所以放松成双向不会把不同前缀的设备混起来。
	kl, tl := letters(kw), letters(target)
	if kl == "" {
		return true // 用户只打了数字("7 号电梯"),数字对上就算候选
	}
	return isSubsequence(kl, tl) || isSubsequence(tl, kl)
}

// isSubsequence 判断 short 是否为 long 的子序列(按顺序出现,不必连续)。
//
// 【必须按 rune 比】第一版写的是 rune(short[i]) —— 那取的是【字节】,
// 和解码出来的 rune 比。ASCII 编号(KT-7)碰巧能过,中文名("3号客梯")一律失败,
// 相近候选那条路对中文设备名等于不存在。
func isSubsequence(short, long string) bool {
	sr, lr := []rune(short), []rune(long)
	i := 0
	for _, r := range lr {
		if i < len(sr) && sr[i] == r {
			i++
		}
	}
	return i == len(sr)
}

// trimLeadingZeros 去掉数字串的前导零,全零则保留一个 "0"。
func trimLeadingZeros(d string) string {
	t := strings.TrimLeft(d, "0")
	if t == "" && d != "" {
		return "0"
	}
	return t
}

// limitAgentAssetsToProjects 把 find_asset 的结果裁到可见项目内。
//
// findAssetsForAgent 的 project 参数是"模型说要在哪个项目找",不是权限 ——
// 模型不传或传错,结果里就会混进别的项目。所以出口再筛一道。
func limitAgentAssetsToProjects(found map[string]any, vis dataVisibility) map[string]any {
	if found == nil || vis.AllData || len(vis.Projects) == 0 {
		return found
	}
	keep := func(key string) int {
		list, ok := found[key].([]map[string]any)
		if !ok {
			return 0
		}
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			name, _ := item["project"].(string)
			if vis.allowsProject(name) {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			delete(found, key)
		} else {
			found[key] = out
		}
		return len(list) - len(out)
	}
	removed := keep("assets")
	keep("nearMatches")
	// count 是"确定命中多少台"(截断之前的总数)。裁掉几台就减几台 ——
	// 【不能直接改成列表长度】列表可能已经截成 10 台,count 是 15;
	// 改成 10 的话旁边的说明还写着"匹配到 15 台",模型会照 count 答"一共 10 台"。
	// (findAssetsForAgent 已经先按范围筛过,正常情况下这里一台都不会裁,
	// 这一道是防将来有人漏传范围。)
	if removed > 0 {
		if n, ok := found["count"].(int); ok {
			found["count"] = max(n-removed, 0)
		}
	}
	if _, ok := found["assets"]; !ok {
		found["count"] = 0
	}
	return found
}
