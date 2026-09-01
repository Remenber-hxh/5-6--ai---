package main

import (
	"encoding/json"
	"log"
	"strings"
)

// ===== 提示词版本留痕 =====
//
// 【为什么编辑能力必须连着这个一起做】改提示词是全系统里最容易"改坏了还
// 看不出来"的操作:改完不报错、界面照常、下一张照片开始悄悄误判,而且是
// 对所有人、所有项目立刻生效。没有回滚的话,唯一的补救办法是凭记忆把原文
// 敲回去 —— 而人恰恰记不住自己删掉的那一句是什么。
//
// 【每一条版本 = 那一刻的完整快照】不存 diff。提示词是一整段互相牵制的
// 文字,拿 diff 拼回去容易拼出一份从来没存在过的中间态,而它跑出来什么
// 结果谁也不知道。整份存,回滚就是原样贴回去。

type PromptVersion struct {
	ID         string `json:"id"`
	TemplateID string `json:"templateId"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	// Data 完整模板 JSON。列表接口不返回它(一份提示词几千字,
	// 二十条版本就是十几万字,列表页不需要)。
	Data      string `json:"-"`
	Note      string `json:"note"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// Template 把快照解回模板。
func (v PromptVersion) Template() (PromptTemplate, error) {
	var t PromptTemplate
	err := json.Unmarshal([]byte(v.Data), &t)
	return t, err
}

// snapshotPromptTemplate 把一份模板存成一条版本。
//
// note 是给人看的一句话("改了机房温度判定"/"回滚到 8-20 那版")。
// 【不强制填】强制填的话,人会填"1"、"改了一下"、"." —— 拿到的不是信息,
// 是噪音,还多一次点击。
func snapshotPromptTemplate(store Store, t PromptTemplate, author, note string) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	mode := t.Mode
	if mode == "" {
		mode = PromptModeStructured
	}
	return store.AddPromptVersion(PromptVersion{
		ID:         newID("pv"),
		TemplateID: t.ID,
		Name:       t.Name,
		Mode:       mode,
		Data:       string(data),
		Note:       strings.TrimSpace(note),
		Author:     author,
		CreatedAt:  nowStamp(),
	})
}

// ensureBaselineVersion 保证"改之前的样子"一定有一条版本。
//
// 【为什么不能只在保存后留痕】版本功能是后加的,库里那些早就存在的模板
// 一条版本都没有。只在保存后留痕的话,第一次编辑写下的是【改完】的样子,
// 而改之前长什么样再也拿不回来 —— 恰恰是第一次改最容易改坏,也最想退回去。
//
// 所以:保存前发现这个模板还没有任何版本,先把【现在的】样子存成基线。
func ensureBaselineVersion(store Store, id string) {
	vs, err := store.ListPromptVersions(id, 1)
	if err != nil || len(vs) > 0 {
		return
	}
	cur, ok, err := store.GetPromptTemplate(id)
	if err != nil || !ok {
		return // 本来就没有,没有"改之前"可留
	}
	if err := snapshotPromptTemplate(store, cur, "系统", "初始版本(启用版本留痕前的内容)"); err != nil {
		log.Printf("WARN: 写入初始版本失败 template=%s: %v", id, err)
	}
}

// promptVersionLimit 列表默认拉多少条。
//
// 【留够但不无限】提示词是慢变量,一个模板一年也改不了几十次;
// 拉太多只是把有用的那几条压到屏幕外面。
const promptVersionLimit = 50
