package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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
)

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
				"assetId 用上下文里 topRiskAssets 给出的完整 id,不要自己拼。",
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
func (s *Server) execAgentTool(r *http.Request, name, rawArgs string) (any, error) {
	var args map[string]any
	if strings.TrimSpace(rawArgs) != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			// 模型偶尔会吐出不合法的 JSON。把错误【原样告诉它】而不是中断整轮 ——
			// 它下一轮通常能自己改对,比直接失败给用户看强。
			return nil, fmt.Errorf("参数不是合法 JSON: %v", err)
		}
	}
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	num := func(k string, def int) int {
		if f, ok := args[k].(float64); ok && f > 0 {
			return int(f)
		}
		return def
	}
	tenant := s.tenantForRequest(r)

	switch name {
	case "find_asset":
		kw := str("keyword")
		if kw == "" {
			return nil, fmt.Errorf("缺少 keyword")
		}
		return s.findAssetsForAgent(tenant, kw)

	case "get_asset_history":
		id := str("assetId")
		if id == "" {
			return nil, fmt.Errorf("缺少 assetId")
		}
		// 先确认这台设备属于当前租户 —— 工具是模型驱动的,它可能拿到
		// 任何字符串。不校验的话,一个猜对的 id 就能读到别家的数据。
		if _, err := s.store.GetAsset(tenant, id); err != nil {
			return nil, fmt.Errorf("设备不存在: %s", id)
		}
		snaps, hErr := s.toolGetAssetHistory(id, num("limit", 20))
		if hErr != nil {
			return nil, hErr
		}
		return normalizeHistoryForModel(snaps), nil

	case "get_record_detail":
		id := str("recordId")
		if id == "" {
			return nil, fmt.Errorf("缺少 recordId")
		}
		return s.toolGetRecordDetail(tenant, id)

	case "compare_asset_periods":
		id := str("assetId")
		if id == "" {
			return nil, fmt.Errorf("缺少 assetId")
		}
		if _, err := s.store.GetAsset(tenant, id); err != nil {
			return nil, fmt.Errorf("设备不存在: %s", id)
		}
		cur := str("current")
		if cur == "" {
			cur = "30d"
		}
		prev := str("previous")
		if prev == "" {
			prev = "30d"
		}
		return s.toolCompareAssetPeriods(id, cur, prev)

	case "get_status_events":
		id := str("assetId")
		if id == "" {
			return nil, fmt.Errorf("缺少 assetId")
		}
		if _, err := s.store.GetAsset(tenant, id); err != nil {
			return nil, fmt.Errorf("设备不存在: %s", id)
		}
		rk := str("rangeKey")
		if rk == "" {
			rk = "30d"
		}
		return s.toolGetStatusEvents(id, rk)
	}
	return nil, fmt.Errorf("未知工具: %s", name)
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
// 返回 (回复文本, 用过的工具名, 是否成功)。不成功时调用方走老路。
func (s *Server) agentChat(
	r *http.Request, systemPrompt, contextJSON, question string,
	history []map[string]any,
) (string, []string, bool) {
	if s.analyticsClient == nil {
		return "", nil, false
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
	tools := agentToolSpecs()

	for round := 0; round <= agentMaxToolRounds; round++ {
		payload := map[string]any{"messages": messages}
		if round < agentMaxToolRounds {
			payload["tools"] = tools // 最后一轮不再给工具:逼它用手上的信息作答
		}
		resp, err := s.analyticsClient.ChatTools(payload)
		if err != nil {
			log.Printf("WARN: agent 工具轮次 %d 调用失败,回退无工具路径: %v", round, err)
			return "", used, false
		}
		switch resp["finish"] {
		case "stop":
			reply, _ := resp["reply"].(string)
			if strings.TrimSpace(reply) == "" {
				// 空响应是官方点名的失败模式之一,别把空白交给用户
				log.Printf("WARN: agent 第 %d 轮返回空回复,回退", round)
				return "", used, false
			}
			return reply, used, true

		case "tool_calls":
			calls, _ := resp["toolCalls"].([]any)
			if len(calls) == 0 {
				return "", used, false
			}
			// 【assistant 那条要原样带回去】OpenAI 协议要求 tool 结果必须
			// 跟在发起它的 assistant 消息之后,且 tool_call_id 对得上。
			if am, ok := resp["assistantMessage"].(map[string]any); ok {
				messages = append(messages, am)
			}
			for _, c := range calls {
				call, _ := c.(map[string]any)
				name, _ := call["name"].(string)
				args, _ := call["arguments"].(string)
				id, _ := call["id"].(string)
				out, execErr := s.execAgentTool(r, name, args)
				if execErr != nil {
					log.Printf("INFO: agent 工具 %s 执行失败(将把错误回给模型): %v", name, execErr)
				} else {
					used = append(used, name)
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
			return "", used, false
		}
	}
	// 轮数用尽还没给出答案 —— 正是官方说的"循环调用"那种情况
	log.Printf("WARN: agent 工具轮次用尽(%d 轮)仍未作答,回退", agentMaxToolRounds)
	return "", used, false
}

// MANAGEMENT_CHAT_SYSTEM_HINT — 工具路径的系统提示。
//
// 【为什么不复用 Python 那份】那份是为"一次性给你全部数据"写的,里面有大量
// "上下文 JSON 各字段含义"的说明。工具路径下模型是【自己去取】数据的,
// 那些说明反而会让它以为手上已经有全部东西、懒得调工具。
//
// 这份只讲三件事:你是谁、有工具就去查、数字必须有出处。
const MANAGEMENT_CHAT_SYSTEM_HINT = `你是「智巡」设施巡检系统管理后台的 AI 助手,服务于巡检主管。

你手上有一份看板概览数据(整体计数、高风险资产、重复问题、待复核、巡检员质量、
数值漂移、任务概览),另外还有几个工具可以查更细的东西。

【最重要的一条】凡是具体的数字、次数、日期、人名,要么来自看板数据,要么来自工具
返回的结果,**绝不能凭印象给**。问到单台设备的逐次历史、某条记录的明细、
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

【回答里不许出现内部字段名和 id】这些是给程序看的,不是给人看的:
  · 不要写 topRiskAssets / repeatedIssues / numericDrift 这类英文字段名
  · 不要写 会议中心::elevator_no_room::KT-5 这种完整 id,只说"KT-5"
  · 不要写 elevator_no_room 这种模板代号,要说"无机房电梯"
说人话:"看板里没有它的记录" → "这台设备最近没有异常记录";
"repeatedIssues 为空" → "最近 30 天没有反复出问题的设备"。

【领域知识可以答】"灭火器多久检查""电梯防夹装置的标准"这类通用维保规范问题,
用专业知识正常回答,可以点明对应国标/规程名称 —— 这不算编造,
编造指的是编本系统里的台账数字、设备名、人员。

输出:
1. 第一句直接回答问题,最关键处用 **…** 加粗(全文只加粗一处)。
2. 另起一行写「依据:」,跟 1-2 条短句,每条一个关键数字或事实。
   数据来自工具的,点明是哪台设备/哪次巡检。
3. 全文 50-120 字,除那一处加粗外不用其它 markdown。`

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
func (s *Server) findAssetsForAgent(tenantID, keyword string) (map[string]any, error) {
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
	exact, partial, near := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for _, a := range all {
		if a == nil {
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
			partial = append(partial, item(a))
		case looseAssetMatch(kw, nName) || looseAssetMatch(kw, nKey):
			near = append(near, item(a))
		}
	}
	// 精确 > 包含:用户打"KT-7"时,"KT-7"要压过"KT-70"
	out := append(append([]map[string]any{}, exact...), partial...)
	if len(out) > 0 {
		if len(out) > 10 {
			out = out[:10]
		}
		return map[string]any{"count": len(out), "assets": out}, nil
	}
	// 【只有在完全没有把握的匹配时才给相近候选】
	// 这一层是为"用户打 K7、设备实际叫 KT-7"准备的。它有猜的成分,
	// 所以必须标出来让模型确认,不能当成命中直接用 —— 答错设备比查不到更糟。
	if len(near) > 0 {
		note := "没有完全匹配的设备,以下是编号相近的候选(有猜测成分)。" +
			"只有一台候选时,可以按它回答,但【必须在回答里写出完整编号】," +
			"让用户能发现认错了;多台候选时列出来反问用户指的是哪一台。"
		if len(near) > 10 {
			near = near[:10]
		}
		return map[string]any{"count": 0, "nearMatches": near, "_note": note}, nil
	}
	return map[string]any{
		"count": 0,
		"_note": "没有找到匹配的设备,连编号相近的也没有。台账里确实没有这台,可以如实告诉用户。",
	}, nil
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
	kd, td := digits(kw), digits(target)
	// 两边都必须有数字且完全相同 —— 数字才是编号的身份
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
func isSubsequence(short, long string) bool {
	i := 0
	for _, r := range long {
		if i < len(short) && rune(short[i]) == r {
			i++
		}
	}
	return i == len(short)
}
