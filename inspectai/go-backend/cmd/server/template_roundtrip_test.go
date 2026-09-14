package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getTpl(t *testing.T, srv *Server, path, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 校验器不该拒收它自己已经存着的数据。
//
// 【这是上面那条不变式测试抓出来的】库里 10 个模板有 5 个没有 asset_no,
// 而豁免名单只写了紫菡那两个。fire_pump / hot_water_room / water_pump
// 读得出来、一点保存就被拒 —— 而且没有任何办法修好:想加 asset_no?
// 那是给有记录的模板加字段,又被另一条规则挡住。三个模板就这么锁死了,
// 界面上只有一句"模板必须有设备编号字段",人只会以为是自己填错了。
func TestEditDoesNotBlockOnPreExistingProblems(t *testing.T) {
	noAssetNo := ReportTemplate{
		ID: "hot_water_room", Name: "热水机房巡检", Project: "会议中心", AssetType: "热水机房",
		Fields: []TemplateField{
			{Code: "cabinet_temperature", Label: "控制柜温度℃", Kind: "number", Source: "ai"},
		},
	}
	// 新建一份这样的模板要拦 —— 新东西不该带着这个毛病出生
	if err := validateReportTemplate(noAssetNo); err == nil {
		t.Error("新建一个没有设备编号的模板应该被拦下")
	}
	// 但改一份【本来就没有】的,不该拦
	if err := validateTemplateEdit(noAssetNo, noAssetNo); err != nil {
		t.Errorf("本来就没有 asset_no,这次改动没让它变差,不该拦:%v", err)
	}

	// 本来有、改完没了 —— 这才是真的在删归属依据,必须拦
	hadAssetNo := noAssetNo
	hadAssetNo.Fields = append([]TemplateField{
		{Code: "asset_no", Label: "设备编号", Kind: "text", Source: "manual", ManualOnly: true},
	}, noAssetNo.Fields...)
	if err := validateTemplateEdit(hadAssetNo, noAssetNo); err == nil {
		t.Error("把已有的设备编号字段删掉必须被拦下")
	}

	// 别的毛病照拦不误 —— 放行的只有 asset_no 这一条存量豁免
	broken := hadAssetNo
	broken.Fields = append(append([]TemplateField{}, hadAssetNo.Fields...),
		TemplateField{Code: "bad_choice", Label: "只有一个选项", Kind: "choice",
			Options: []string{"只有这个"}, Source: "ai"})
	if err := validateTemplateEdit(hadAssetNo, broken); err == nil {
		t.Error("只有一个选项的单选还是要拦 —— 存量豁免只针对 asset_no")
	}
}

// 读出来、原样写回去,什么都不该变。
//
// 【为什么值得单独守这一条】编辑器就是这么工作的:GET 一份、改其中一两处、
// PUT 回去。凡是"读回来的表示"和"能写回去的表示"对不齐的地方,
// 都会变成一次静默的数据丢失 —— 人改了 A,B 被顺手清成零值,返回 200,
// 要到很久以后才被发现。
//
// 这条测试不关心具体哪个字段,它关心的是那个不变式本身。
// 往模板上加新列时,如果只加了读没加写(或者反过来),这里会先炸。
func TestTemplateRoundTripChangesNothing(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"hot_water_room", "elevator_machine_room", "zihan_energy"} {
		t.Run(id, func(t *testing.T) {
			before, ok := templateByID(id)
			if !ok {
				t.Fatalf("找不到模板 %s", id)
			}

			// 走真实的 GET —— 编辑器拿到的就是这份
			got := getTpl(t, server, "/api/report/templates/"+id, tokens["admin"])
			if got.Code != http.StatusOK {
				t.Fatalf("读取失败 code=%d body=%s", got.Code, got.Body.String())
			}
			var read struct {
				Template json.RawMessage `json:"template"`
			}
			if err := json.Unmarshal(got.Body.Bytes(), &read); err != nil {
				t.Fatal(err)
			}

			// 原样写回去
			put := putTpl(t, server, "/api/report/templates/"+id, tokens["admin"], string(read.Template))
			if put.Code != http.StatusOK {
				t.Fatalf("写回失败 code=%d body=%s", put.Code, put.Body.String())
			}

			after, _ := templateByID(id)
			if after.MinImages != before.MinImages || after.MaxImages != before.MaxImages {
				t.Errorf("照片张数变了:%d/%d → %d/%d",
					before.MinImages, before.MaxImages, after.MinImages, after.MaxImages)
			}
			// 场景头那几列:新编辑器要在同一个界面里编它们,一次存盘写完。
			// 只加了读没加写的话,人填了场景描述、存完就没了,而且返回 200。
			if after.Scene != before.Scene {
				t.Errorf("场景描述变了:%q → %q", before.Scene, after.Scene)
			}
			if after.SceneFeatures != before.SceneFeatures {
				t.Errorf("识别特征变了:%q → %q", before.SceneFeatures, after.SceneFeatures)
			}
			if after.ExtraNotes != before.ExtraNotes {
				t.Errorf("补充说明变了:%d 字 → %d 字", len(before.ExtraNotes), len(after.ExtraNotes))
			}
			if len(after.ExpectedPhotos) != len(before.ExpectedPhotos) {
				t.Errorf("期望照片变了:%v → %v", before.ExpectedPhotos, after.ExpectedPhotos)
			}
			if after.PromptMode != before.PromptMode || after.RawText != before.RawText {
				t.Errorf("提示词维护方式变了:%q/%d字 → %q/%d字",
					before.PromptMode, len(before.RawText), after.PromptMode, len(after.RawText))
			}
			if after.AIPrompt != before.AIPrompt || after.HasAI != before.HasAI {
				t.Errorf("AI 设置变了:prompt %q→%q hasAI %v→%v",
					before.AIPrompt, after.AIPrompt, before.HasAI, after.HasAI)
			}
			if len(after.Fields) != len(before.Fields) {
				t.Fatalf("字段数变了:%d → %d", len(before.Fields), len(after.Fields))
			}
			for i, bf := range before.Fields {
				af := after.Fields[i]
				if af.Code != bf.Code {
					t.Errorf("第 %d 个字段的标识变了:%q → %q", i, bf.Code, af.Code)
					continue
				}
				if af.Required != bf.Required {
					t.Errorf("%s 的必填变了:%v → %v", bf.Code, bf.Required, af.Required)
				}
				if af.Label != bf.Label || af.Kind != bf.Kind {
					t.Errorf("%s 的表单定义变了:%q/%q → %q/%q", bf.Code, bf.Label, bf.Kind, af.Label, af.Kind)
				}
				if af.JudgeMode != bf.JudgeMode || af.YesWhen != bf.YesWhen ||
					af.NoWhen != bf.NoWhen || af.SkipWhen != bf.SkipWhen {
					t.Errorf("%s 的判定规则变了:mode %q→%q yes %q→%q",
						bf.Code, bf.JudgeMode, af.JudgeMode, bf.YesWhen, af.YesWhen)
				}
				if len(af.Options) != len(bf.Options) {
					t.Errorf("%s 的选项变了:%v → %v", bf.Code, bf.Options, af.Options)
				}
			}
		})
	}
}
