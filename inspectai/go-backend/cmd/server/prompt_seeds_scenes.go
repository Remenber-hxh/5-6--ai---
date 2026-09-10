package main

// ===== 其余 8 个场景的判定规则种子 =====
//
// 这些模板此前【一条判定规则都没配】,渲染出来是空,于是运行时一直回退到
// ai-service 里那份写死的 .md —— 后台怎么改都不影响 AI。
//
// 【两类来源,不要混为一谈】
//
//  A. 有专用 .md 的三个(zihan_energy / power_room / escalator):
//     逐条从 .md 搬过来,尽量一字不改。搬迁是有损的 —— 灭火器那条判定
//     就是把三个条件压成两个,结果实测漏判了一次真实的超期未检(迁移 029)。
//     所以这里宁可啰嗦,也不做"归纳提炼"。
//
//  B. 只共用通用 screen_reading.md 的五个(zihan_daily / hot_water_room /
//     fire_pump / ups_room / water_pump):那份 .md 只覆盖到"读屏"和其中
//     3 个字段,其余字段【本来就没有判定标准】。下面这些是【按字段名起草的】,
//     不是从哪里搬来的 —— 现场口径需要业务确认后再调。
//     宁可写得保守(拿不准就不返回),也不让 AI 替现场做没依据的判断。
//
// 【场景级的话进 ExtraNotes】"哪些字段 AI 绝对不能填""有异响要判是不是否"
// 这类不属于任何单个字段,放在补充说明里,渲染时拼在总则后面。

// sceneTemplateSeeds 除两个电梯之外的 8 个场景。
func sceneTemplateSeeds() []PromptTemplate {
	return []PromptTemplate{
		energyMeterSeed(),
		substationSeed(),
		escalatorSeed(),
		zihanDailySeed(),
		hotWaterRoomSeed(),
		firePumpSeed(),
		upsRoomSeed(),
		waterPumpSeed(),
	}
}

// ---------- A 类:从专用 .md 逐条搬 ----------

// 能耗抄表 —— 搬自 prompts/energy_meter.md
//
// 这个场景几乎全部是"怎么读数"的规矩,落不进单个字段格子,所以大头在
// ExtraNotes。字段本身只需说清"这个 code 对应现场哪块表"。
func energyMeterSeed() PromptTemplate {
	readMeter := func(code, label, where string) PromptField {
		return PromptField{
			Code: code, Label: label, Group: "抄表", Mode: ModeReadText,
			YesWhen: where,
		}
	}
	return PromptTemplate{
		ID:    "zihan_energy",
		Name:  "能耗抄表",
		Scene: "紫菡雅集能耗抄表:读 Z1~Z4 能耗表 + 生活水表 + 消防水表的累计读数。",
		ExpectedPhotos: []string{
			"Z1/Z2/Z3/Z4 能耗表 LCD 屏(需显示 EP / ΣEP 主读数)",
			"生活水表(机械字轮,黑色整数窗口)",
			"消防水表(机械字轮,黑色整数窗口)",
		},
		ExtraNotes: `先判表型,再读数字 —— 不要直接把图里最大/最清晰的数字当读数。LCD 电表(蓝底,标识 EP/ΣEP/kWh)和机械字轮水表(黑底白字窗口 + 红色小盘)读法完全不同。
电表只读 EP / ΣEP 正向有功电量;当前屏显示 EQ / Ia / Ub / 电流 / 电压等非 EP 数据时不要填读数,在 warnings 提示等屏切到 EP 后重拍。
**上下两行不是小数点**:LCD 电表常把一个读数拆成上下两行显示,禁止把换行当小数点(上排 1505、下排 2777 不等于 1505.2777)。看不到真实小数点时 confidence 不得高于 0.65 并提示人工复核,不要自行决定小数位。下排以 0 开头时这个 0 要保留。
水表只取黑色字轮窗口的整数位,**包括最后一位黑色数字**;红色字轮/红色指针盘是小数位,日报不需要,直接忽略。前导 0 可去掉(000722 读作 722,不是 72.2)。
用户不会标注这是 Z1 还是 Z2:优先看现场标牌(Z1/Z2/Z3/Z4、1#/2#变压器),其次按上传顺序(第 1 张通常 Z1);按顺序假设时要在 reason 里写明并把 confidence 降到 0.7。
reason 里必须说明表型,例如"图1为机械水表,黑色字轮 000722 去前导 0 读作 722"。
这些一律不是读数,不要填:表盖设备编号、二维码、Q1/Q2/Q3、检验编号、厂家编号、屏上时间戳、通讯指示灯、别的表的反光投影。看到红色报警灯时在 warnings 提示"该表显示报警",但读数照读。`,
		Fields: []PromptField{
			readMeter("z1_reading", "Z1 能耗表读数", "Z1 电表 LCD 屏显示 EP/ΣEP 主读数且数字清晰完整(单位 kWh)"),
			readMeter("z2_reading", "Z2 能耗表读数", "Z2 电表 LCD 屏显示 EP/ΣEP 主读数且数字清晰完整(单位 kWh)"),
			readMeter("z3_reading", "Z3 能耗表读数", "Z3 电表 LCD 屏显示 EP/ΣEP 主读数且数字清晰完整(单位 kWh)"),
			readMeter("z4_reading", "Z4 能耗表读数", "Z4 电表 LCD 屏显示 EP/ΣEP 主读数且数字清晰完整(单位 kWh)"),
			readMeter("living_water_reading", "生活水表读数", "生活水表黑色字轮窗口整数位清晰可读(单位 m³,红色小数位忽略)"),
			readMeter("fire_water_reading", "消防水表读数", "消防水表黑色字轮窗口整数位清晰可读(单位 m³,红色小数位忽略)"),
		},
	}
}

// 变电所 —— 搬自 prompts/substation.md
//
// 【这个场景最要紧的不是判什么,是"哪些绝对不能判"】.md 里点名 7 个字段
// 图片无法可靠判断,AI 一律不能填(柜面红灯当报警、远景读电度表这类误判,
// 后果是现场以为查过了)。这 7 条必须进 ExtraNotes ——
// 光是不写进「字段映射」挡不住:字段清单是无条件附在提示词后面的,
// 模型照样看得见这些 code。
func substationSeed() PromptTemplate {
	return PromptTemplate{
		ID:    "power_room",
		Name:  "变电所巡检",
		Scene: "变电所巡检:以【手写巡检记录表】为主要数据源,设备照片仅用于辅助确认外观/照明/卫生。",
		ExpectedPhotos: []string{
			"手写巡检记录表(核心数据源,需拍清当次时间列)",
			"高压柜 / 低压配电柜柜体",
			"变压器 / 机房环境(照明、地面、绝缘垫)",
		},
		ExtraNotes: `**以下 7 个字段一律不要填,留给巡检员人工**:变电所编号(asset_no,柜体铭牌易糊易拍错柜)、变压器声音是否异常(transformer_noise,图片无法判断声音)、高压柜是否有报警(hv_alarm,柜面红灯多为运行指示灯,极易误判)、高压柜电度表是否显示异常(meter_abnormal,需现场逐表核对)、高压柜温湿度是否异常(temperature_humidity_abnormal,无数据源)、高压柜有无异常气味(hv_smell,图片无法识别气味)、机房温湿度(room_temperature_humidity,巡检表上无此项)。这几个 code 不填、不猜,在 warnings 里统一提示一次"上述字段按规则留人工填写"。
数据以手写巡检表为准:表格有多个时间列(9:00/13:00/18:00/21:00/0:00/6:00),**只读最早一列已填写的那一栏**,其余空列忽略。
手写数字容易混:4/9、0/6、1/7、5/3。三相电压正常在 380~420V,读出明显离谱的值(如 40 或 4000)要把 confidence 降到 0.6 并在 warnings 提示可能误读。
勾选符号:✓/√ → 是/正常;✗/×/横线 → 否/异常;空白 → 不填该字段。
2# 进线柜 / 2# 变压器那几行全是 0 表示该路未投运或备用,**不报异常**,但在 observations 注明"2#路未运行"。
只拍了设备照片、没拍手写表时:电压/温度等数值类一律不填,只填照片能判的外观/照明/卫生,warnings 提示"未拍到巡检记录表,电压/变压器温度需人工补填"。
当次时间列整列空白(还没抄录就来拍)→ recognitionStatus 置为 retake_required,retakeReason 写"巡检表本次时段未填写,请先现场抄录数据再拍照"。
不要把电流值(A)误填到电压字段(V),不要把时间戳填进任何业务字段。`,
		Fields: []PromptField{
			{Code: "inspection_time", Label: "日期+时间", Group: "头部", Mode: ModeReadText,
				YesWhen: "手写表表头日期 + 已填那一列的时间清晰可读(输出 YYYY-MM-DD HH:MM)"},
			{Code: "voltage_normal", Label: "高压柜:电压是否正常", Group: "高压柜", Mode: ModeVisual,
				YesWhen:  "手写表上 1# 进线柜三相电压(AB/BC/AC)全部在 342~418V 之间(380V±10%)",
				NoWhen:   "任一相低于 342V 或高于 418V(reason 注明是哪一相、多少 V)",
				SkipWhen: "三相中任一相读不出、或没拍到手写表"},
			{Code: "transformer_temperature", Label: "变压器:温度及三相温差", Group: "变压器", Mode: ModeReadText,
				YesWhen: `手写表上变压器 A/B/C 三相温度清晰可读(按 "A48 B51 C49℃,三相温差3℃" 格式填,温差=最大值−最小值;正常运行 <85℃、温差宜 <15℃,超出在 reason 里提示但值照填)`},
			{Code: "hv_appearance", Label: "高压柜:外观检查是否良好", Group: "高压柜", Mode: ModeVisual,
				YesWhen:  "手写表「开关柜、变压器外观是否完好」打勾;或照片里柜门无变形、无破损、无漏油漏液",
				NoWhen:   "该项打叉;或照片里柜体明显变形、破损、漏油漏液",
				SkipWhen: "手写表该项空白且照片没拍到柜体"},
			{Code: "transformer_appearance", Label: "变压器:外观检查是否良好", Group: "变压器", Mode: ModeVisual,
				YesWhen:  "手写表「外观完好」打勾;或照片里变压器无破损、无漏油",
				NoWhen:   "该项打叉;或照片里变压器明显破损、漏油",
				SkipWhen: "手写表该项空白且照片没拍到变压器"},
			{Code: "room_lighting", Label: "机房照明是否正常", Group: "机房环境", Mode: ModeVisual,
				YesWhen:  "手写表「照明是否正常」打勾;或照片里灯具点亮、照度正常",
				NoWhen:   "该项打叉;或照片里明显无照明、大面积不亮",
				SkipWhen: "手写表该项空白且照片没拍到机房"},
			{Code: "room_clean", Label: "机房卫生", Group: "机房环境", Mode: ModeVisualLenient,
				YesWhen:  "手写表「室内设施是否良好」打勾;或照片里地面整洁、绝缘垫铺设规范、无杂物",
				NoWhen:   "该项打叉;或照片里明显堆放杂物、积水、大面积脏乱",
				SkipWhen: "手写表该项空白且照片没拍到机房地面",
				Note:     "少量灰尘、零星碎屑、脚印、线缆、反光都不算异常"},
		},
	}
}

// 扶梯 —— 搬自 prompts/escalator.md
//
// 【有一条方向相反的字段】"扶梯运行是否有异响":有异响要判「是」,
// 而其他所有字段都是"完好 → 是"。不把这条单独说明的话,模型会按
// 惯例判反 —— 而判反的结果是"有异响"被记成正常。
func escalatorSeed() PromptTemplate {
	visual := func(code, label, yes, no string) PromptField {
		return PromptField{Code: code, Label: label, Group: "扶梯本体", Mode: ModeVisual,
			YesWhen: yes, NoWhen: no, SkipWhen: "未拍到该部位"}
	}
	return PromptTemplate{
		ID:    "escalator",
		Name:  "扶梯巡检",
		Scene: "自动扶梯 / 自动人行道巡检:查出入口标识、梯级梳齿、扶手带、护壁板、急停与检验标志。",
		ExpectedPhotos: []string{
			"出入口与警示标识、防攀爬装置",
			"梳齿板 / 梯级 / 安全毛刷",
			"扶手带、护壁板(裙板)、上下基坑盖板",
			"紧急制动开关、《安全检验合格》标志 / 登记牌",
		},
		ExtraNotes: `选项字段一律**按字段的问句字面回答**:"……是否完好/正常/牢固/完整/有效" → 完好/正常判「是」,破损/异常/缺失判「否」。
**唯一例外:「扶梯运行是否有异响」方向相反** —— 有异响才判「是」(异常),无异响判「否」(正常)。这一条不要按"完好=是"的惯例套。
直接可见项要主动判断,不要为求稳留空:警示标识、梳齿板/梯级、扶手带、护壁板、安全毛刷、基坑盖板、急停按钮、《安全检验合格》标志 —— 相关部位拍清楚就给是/否,明显完好就大胆判「是」。
照片无法感知声音、运行速度、装置是否真正有效 —— 这类没有可见证据时一律不返回,不要拿"是/否"代替"没拍到"。`,
		Fields: []PromptField{
			{Code: "inspection_time", Label: "检查时间", Group: "头部", Mode: ModeSystem},
			{Code: "asset_no", Label: "扶梯编号", Group: "头部", Mode: ModeReadText,
				YesWhen: "编号牌或《安全检验合格》标志上的编号清晰可读"},
			{Code: "inspector", Label: "巡检人员", Group: "头部", Mode: ModeSystem},
			visual("entrance_sign", "出入口警示标识是否完好", "出入口警示标识齐全、完好", "缺失、破损、污损不可读"),
			visual("anti_climb", "防档防攀爬是否完好", "防攀爬挡板/装置在位且完好", "缺失、变形、损坏"),
			{Code: "emergency_stop", Label: "紧急制动开关是否正常", Group: "扶梯本体", Mode: ModeVisual,
				YesWhen: "急停按钮在位、完好、无遮挡", NoWhen: "缺失、损坏、被遮挡", SkipWhen: "未拍到急停按钮",
				Note: "「是否正常」里需实测的部分照片给不出依据,只按可见状态判"},
			visual("comb_plate", "疏齿板是否牢固、齿是否完整", "梳齿板齿形完整、无缺齿断齿、安装牢固", "缺齿、断裂、翘起"),
			visual("steps", "梯级是否完好", "梯级踏板完好,无破损/塌陷/缺失,无异物卡阻", "破损、缺失、明显磨损"),
			visual("pit_cover", "上、下基坑盖板是否牢固", "上下基坑盖板齐全、平整、牢固", "缺失、翘起、松动"),
			visual("handrail", "扶手带是否完好", "扶手带表面完好、无破损裂口", "破损、老化开裂"),
			visual("skirt_panel", "扶梯护壁板是否牢固", "护壁板/裙板完整、无变形、安装牢固", "破损、缺失、松动"),
			visual("safety_brush", "安全毛刷是否完好", "裙板安全毛刷齐全、完好", "缺失、脱落、损坏"),
			{Code: "running_speed", Label: "扶梯运行速度是否正常", Group: "扶梯本体", Mode: ModeVisual,
				NoWhen:   "照片明确显示扶梯停运或运行异常",
				SkipWhen: "速度需现场观测,照片通常判不了 —— 没有明确证据时一律不返回"},
			{Code: "running_noise", Label: "扶梯运行是否有异响", Group: "扶梯本体", Mode: ModeSensory,
				NoWhen: "看见可能引起异响的可见故障(部件松脱、卡阻)",
				Note:   "这一项方向相反:有异响=是(异常)。照片判不了声音,除非看见明确可见故障,否则不返回"},
			{Code: "inspection_cert", Label: "《安全检验合格》标志是否完好", Group: "扶梯本体", Mode: ModeObjectiveDate,
				YesWhen:  "《安全检验合格》标志在位、完好,且标注的下次检验日期晚于 current_date",
				NoWhen:   "标志缺失、破损,或下次检验日期早于 current_date(已过期)",
				SkipWhen: "未拍到标志、或日期看不清"},
			{Code: "nonconformity", Label: "不符合项说明", Group: "汇总", Mode: ModeSummary},
		},
	}
}

// ---------- B 类:没有专用 .md,以下为【起草稿】,需业务确认 ----------

// 通用机房环境三件套(卫生 / 照明 / 漏水报警)。
//
// 这三项在热水机房、消防泵房、生活水泵房、UPS 机房里重复出现,判定口径
// 应当一致 —— 各写一份的话,同一件事在不同模板里判得不一样,
// 而现场看到的是"同样的地面,这个模板说正常那个说异常"。
func roomEnvFields(group string) []PromptField {
	return []PromptField{
		{Code: "leak_alarm", Label: "是否漏水/报警", Group: group, Mode: ModeVisual,
			YesWhen:  "地面/管道/设备周边有明显积水、水渍、滴漏,或控制柜显示报警、报警灯点亮",
			NoWhen:   "地面干燥、无水渍,控制面板无报警显示",
			SkipWhen: "未拍到地面与控制面板",
			Note:     "这一项方向相反:发现漏水或报警才判「是」,一切正常判「否」"},
		{Code: "room_clean", Label: "机房卫生", Group: group, Mode: ModeVisualLenient,
			YesWhen:  "地面基本整洁,无明显杂物堆放、积水、油污",
			NoWhen:   "明显堆放杂物/纸箱、积水漏油、大面积脏乱",
			SkipWhen: "未拍到机房地面",
			Note:     "少量灰尘、零星碎屑、脚印、地面线缆、反光、墙面陈旧都不算异常"},
	}
}

func roomLightingField(group, code string) PromptField {
	return PromptField{Code: code, Label: "机房照明", Group: group, Mode: ModeVisual,
		YesWhen:  "机房照明点亮、照度足够看清设备",
		NoWhen:   "明显无照明、灯具大面积不亮",
		SkipWhen: "未拍到机房整体环境"}
}

// 热水机房(起草)
func hotWaterRoomSeed() PromptTemplate {
	f := []PromptField{
		{Code: "inspection_time", Label: "日期+时间", Group: "头部", Mode: ModeSystem},
		{Code: "cabinet_temperature", Label: "控制柜显示温度℃", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "控制柜 LCD 上的温度数值清晰可读(只读数,不做正常/异常判定)"},
		{Code: "tank_level", Label: "水箱水位(米)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "水位计/液位显示数值清晰可读"},
		{Code: "water_pressure", Label: "供水压力(MPa)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "压力表指针或数显读数清晰可读(注意单位 MPa)"},
	}
	f = append(f, roomEnvFields("机房环境")...)
	f = append(f, roomLightingField("机房环境", "room_lighting"))
	return PromptTemplate{
		ID:    "hot_water_room",
		Name:  "热水机房巡检",
		Scene: "热水机房:读控制柜温度、水箱水位、供水压力,并查机房环境。",
		ExpectedPhotos: []string{
			"控制柜 LCD 屏(显示温度)",
			"水箱水位计 / 液位显示",
			"供水压力表",
			"机房地面与照明",
		},
		ExtraNotes: `控制柜屏可能多页循环,只读当前帧能确认的数值;屏在切换、拍糊、反光看不清时一律不填该字段,不要凭印象猜。
读数只填数值,不要带单位符号以外的文字;分不清单位(米 / MPa / ℃)时不填并在 warnings 提示。
温度、水位、压力这三项【只读数,不判正常异常】—— 阈值因季节和设备而异,现场比照片更清楚。`,
		Fields: f,
	}
}

// 消防泵房(起草)
func firePumpSeed() PromptTemplate {
	f := []PromptField{
		{Code: "inspection_time", Label: "日期+时间", Group: "头部", Mode: ModeSystem},
		{Code: "tank_level", Label: "水箱水位(米)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "水位计/液位显示数值清晰可读"},
		{Code: "sewage_auto", Label: "污水泵是否自动", Group: "运行状态", Mode: ModeVisual,
			YesWhen:  "控制箱旋钮/切换开关明确指向「自动」档位,或面板「自动」指示灯点亮",
			NoWhen:   "旋钮指向「手动」或「停止」,或「自动」灯未亮",
			SkipWhen: "未拍到控制箱面板、或档位标识看不清"},
	}
	f = append(f, roomEnvFields("机房环境")...)
	f = append(f, roomLightingField("机房环境", "room_lighting"))
	return PromptTemplate{
		ID:    "fire_pump",
		Name:  "消防泵房巡检",
		Scene: "消防泵房:查水箱水位、污水泵自动档位、漏水报警与机房环境。",
		ExpectedPhotos: []string{
			"水箱水位计 / 液位显示",
			"污水泵控制箱面板(自动/手动档位)",
			"泵体与管路(有无渗漏)",
			"机房地面与照明",
		},
		ExtraNotes: `消防泵房的核心是**处于备用状态**:档位停在「手动」意味着火警时不会自动启泵,属于要立刻处理的问题 —— 拍到面板就要判,不要留空。
泵体、法兰、管路接口的水渍要和地面冲洗残留区分开:接口处持续渗水才算漏水,地面一片潮湿但接口干燥的不下结论,留人工。
拿不准的一律不返回。这个场景判错的代价是"以为消防没问题"。`,
		Fields: f,
	}
}

// 生活水泵房(起草)
func waterPumpSeed() PromptTemplate {
	f := []PromptField{
		{Code: "inspection_time", Label: "日期+时间", Group: "头部", Mode: ModeSystem},
		{Code: "tank_level", Label: "水箱水位(米)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "水位计/液位显示数值清晰可读"},
		{Code: "water_pressure", Label: "供水压力(MPa)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "压力表指针或数显读数清晰可读(注意单位 MPa)"},
	}
	f = append(f, roomEnvFields("机房环境")...)
	f = append(f, roomLightingField("机房环境", "room_lighting"))
	return PromptTemplate{
		ID:    "water_pump",
		Name:  "生活水泵房巡检",
		Scene: "生活水泵房:读水箱水位与供水压力,查漏水报警与机房环境。",
		ExpectedPhotos: []string{
			"水箱水位计 / 液位显示",
			"供水压力表(蓝色压力罐上)",
			"泵体与管路(有无渗漏)",
			"机房地面与照明",
		},
		ExtraNotes: `压力表读数注意量程和单位:表盘上同时印 MPa 和 kgf/cm² 时以 MPa 为准;指针在两格之间时读到最近的刻度并在 reason 说明。
只读数,不判压力正常与否 —— 正常区间因楼层和泵组而异,现场比照片清楚。
泵体、法兰、管路接口的水渍要和地面冲洗残留区分开:接口处持续渗水才算漏水。`,
		Fields: f,
	}
}

// UPS 机房(起草)
func upsRoomSeed() PromptTemplate {
	f := []PromptField{
		{Code: "inspection_time", Label: "日期+时间", Group: "头部", Mode: ModeSystem},
		{Code: "asset_no", Label: "UPS 主机编号", Group: "头部", Mode: ModeReadText,
			YesWhen: "主机柜铭牌/编号牌上的编号清晰完整可读"},
		{Code: "battery_appearance", Label: "蓄电池组:外观", Group: "蓄电池组", Mode: ModeVisual,
			YesWhen:  "电池外壳完好无鼓胀变形、极柱无腐蚀漏液、连接排紧固",
			NoWhen:   "电池鼓胀变形、极柱氧化腐蚀、漏液流痕、连接排松脱",
			SkipWhen: "未拍到电池组"},
		{Code: "battery_voltage", Label: "电池电压(V)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "UPS 面板或电池监控屏上的电池电压数值清晰可读(单位 V)"},
		{Code: "input_voltage", Label: "输入电压(V)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "UPS 面板显示的输入电压数值清晰可读(单位 V)"},
		{Code: "output_voltage", Label: "输出电压(V)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "UPS 面板显示的输出电压数值清晰可读(单位 V)"},
		{Code: "output_current", Label: "输出电流(A)", Group: "运行参数", Mode: ModeReadText,
			YesWhen: "UPS 面板显示的输出电流数值清晰可读(单位 A)"},
		{Code: "fire_equipment", Label: "机房空间:消防器材是否完好", Group: "机房环境", Mode: ModeVisual,
			YesWhen:  "灭火器在位、压力表指针在绿区、瓶体无破损",
			NoWhen:   "灭火器缺失、压力表指针在红区、瓶体破损",
			SkipWhen: "未拍到消防器材、或压力表看不清"},
	}
	f = append(f, roomEnvFields("机房环境")...)
	f = append(f, roomLightingField("机房环境", "lighting"))
	return PromptTemplate{
		ID:    "ups_room",
		Name:  "UPS 机房巡检",
		Scene: "UPS 机房:读主机面板的电压电流,查蓄电池组外观、配电箱与机房环境。",
		ExpectedPhotos: []string{
			"UPS 主机柜面板(输入/输出电压、电流、电池电压)",
			"蓄电池组(外壳、极柱、连接排)",
			"配电箱内部与指示灯",
			"机房消防器材、地面与照明",
		},
		ExtraNotes: `**以下几项一律不要填,留给巡检员人工**:蓄电池组是否有报警(battery_alarm,面板灯多为运行指示,照片极易误判)、电池状态(battery_status,需接入监控或实测才知道)、配电箱内有无异味(box_smell,照片闻不到)、配电箱指示灯(indicator_status,灯色含义因设备而异,远景判不准)、温湿度(temperature_humidity,无数据源)。这几个 code 不填不猜,在 warnings 统一提示一次。
UPS 面板一屏可能轮显多个参数,只填当前帧能明确对上标签的数值;分不清是输入还是输出电压时**一律不填**,填错比不填糟得多。
电池鼓胀、极柱腐蚀、漏液这三样是要立刻处理的,拍到电池组就要判,不要留空。`,
		Fields: f,
	}
}

// 紫菡综合巡检(起草;强电井那部分搬自 screen_reading.md)
// zihanDailyNotes — 紫菡「综合巡检」的本场景补充说明。
//
// 【单独提出来当常量,是给迁移 032 用的】迁移要判断库里这一格是不是还等于
// 上一版种子(等于才补新内容,不等于说明人改过、不能盖)。两边各写一份的话,
// 改了这里忘了改那边,迁移就永远认为"人改过",于是永远不补 —— 而且不报错。
const zihanDailyNotes = `强电井除湿机屏(湿博 SHITENG 等品牌)可能多页循环,只读当前帧能确认的温度与湿度;屏在切换或反光看不清时不填,不要凭印象猜。
**strong_room_01 的判定阈值:温度 < 40℃ 且 湿度 < 70% → 「正常」;任一超出 → 「异常」**。两个数都读不到时不判,留人工。
分区检查(配电箱/弱电机房/消防泵房)按"拍到就判"处理:明显堆放杂物、积水、设备报警才算异常,少量灰尘和线缆走线不算。
除湿机典型屏是左右两个圆环数字:标「湿度%」的那个填 humidity,标「温度℃」的填 temperature;顶部时间戳(如 04/29 15:13)不是读数,任何字段都别填它。
七段数码管容易混 B/8、6/8、0/D,置信度按字符可读性给;数字正在切换(半个数字和下一个叠加)时 confidence 降到 0.5 并在 warnings 里说明。
屏上显示 "--" 或 "ER" / "Err" → 该字段不填,warnings 提示"屏幕显示错误码 XX"。
strong_room_01 的 reason 要把算式写出来,例:"温度21℃<40, 湿度34%<70, 判定正常"。
屏完全熄灭/黑屏、只拍到外壳没拍到显示区、反光把数字全挡住、或拍到的是空白墙 → retake_required,不要硬猜。
不要看到红色 LED 就判异常 —— 红色 LED 在很多设备上只是普通运行指示灯。`

// zihanDailyNotesV1 — 迁移 031 种进库的那一版(前三行)。迁移 032 只在库里
// 还等于它时才升级到上面那份。
const zihanDailyNotesV1 = `强电井除湿机屏(湿博 SHITENG 等品牌)可能多页循环,只读当前帧能确认的温度与湿度;屏在切换或反光看不清时不填,不要凭印象猜。
**strong_room_01 的判定阈值:温度 < 40℃ 且 湿度 < 70% → 「正常」;任一超出 → 「异常」**。两个数都读不到时不判,留人工。
分区检查(配电箱/弱电机房/消防泵房)按"拍到就判"处理:明显堆放杂物、积水、设备报警才算异常,少量灰尘和线缆走线不算。`

func zihanDailySeed() PromptTemplate {
	area := func(code, label, what string) PromptField {
		return PromptField{Code: code, Label: label, Group: "分区检查", Mode: ModeVisualLenient,
			YesWhen:  what + "整洁有序、设备无明显异常、无杂物堆放",
			NoWhen:   "明显堆放杂物、积水漏水、设备破损或报警、门锁损坏",
			SkipWhen: "未拍到该区域",
			Note:     "少量灰尘、零星碎屑、线缆走线、墙面陈旧都不算异常"}
	}
	return PromptTemplate{
		ID:    "zihan_daily",
		Name:  "综合巡检",
		Scene: "紫菡雅集综合巡检:强电井除湿机读数 + 配电箱、弱电机房、消防泵房分区检查。",
		ExpectedPhotos: []string{
			"强电井除湿机 LCD 屏(温度、湿度)",
			"配电箱外部与内部",
			"弱电机房",
			"消防泵房",
		},
		// 【这一段是从 screen_reading.md 搬过来的,不能省】那份内置 .md 原来
		// 整段接管着这个模板。切回字段表就等于把它整份丢掉 —— 字段表能表达
		// "每个字段怎么判",表达不了"这块屏长什么样、什么时候该重拍"。
		// 少了这几条,换来的是多认 4 个分区字段、却把 3 个读数项读糊。
		ExtraNotes: zihanDailyNotes,
		Fields: []PromptField{
			{Code: "temperature", Label: "温度℃", Group: "强电井", Mode: ModeReadText,
				YesWhen: "除湿机 LCD 屏上的温度数值清晰可读(单位 ℃)"},
			{Code: "humidity", Label: "湿度%", Group: "强电井", Mode: ModeReadText,
				YesWhen: "除湿机 LCD 屏上的湿度数值清晰可读(单位 %)"},
			{Code: "strong_room_01", Label: "强电井室内情况", Group: "强电井", Mode: ModeVisual,
				YesWhen:  "读到的温度 < 40℃ 且 湿度 < 70%,且室内无积水、无明显杂物堆放",
				NoWhen:   "温度 ≥ 40℃ 或 湿度 ≥ 70%,或室内明显积水、堆放杂物、设备报警",
				SkipWhen: "温湿度两个数都读不到,且室内情况也没拍清"},
			area("distribution_box", "配电箱情况", "配电箱柜体外观完好、门锁正常,箱周"),
			area("distribution_box_inside", "配电箱内部情况", "箱内元件排列整齐、无烧蚀痕迹、无异物,箱内"),
			area("weak_room", "弱电机房情况", "弱电机房"),
			area("fire_pump_room", "消防泵房情况", "消防泵房"),
		},
	}
}
