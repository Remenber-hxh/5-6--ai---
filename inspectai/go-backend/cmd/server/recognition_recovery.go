package main

import (
	"log"
	"time"
)

// ===== 被打断的识别:启动时收尾 =====
//
// 识别是后台 goroutine 跑的(runAnalysis)。进程在识别途中重启 —— 每次部署都会 ——
// 那条记录就停在"识别中":提交接口拒绝它("AI 识别尚未完成"),手机上轮询 80 秒后
// 只提示"可手动填写",而记录本身永远不会自己好。要靠人发现、再点一次重新识别。
//
// 【启动时一次性收尾】新进程起来的那一刻,上一个进程的识别 goroutine 已经不存在了,
// 所以此刻所有"识别中"的记录都是被打断的,不存在"还在跑、被误伤"的情况 ——
// 前提是只有一个后端实例(现在的部署就是一个)。将来多实例时,这里要改成
// 只处理超过识别预算很久的记录。
//
// 【改成"需重拍",不是"失败"】现场看到的是熟悉的那条路:重拍、或者转人工填写。

const interruptedRecognitionReason = "上次识别被中断(服务重启),请重新识别或转人工填写"

func (s *Server) recoverInterruptedRecognitions() {
	n, err := s.store.ResetInterruptedRecognitions(interruptedRecognitionReason)
	if err != nil {
		log.Printf("WARN: 收尾被打断的识别失败: %v", err)
		return
	}
	if n > 0 {
		log.Printf("收尾了 %d 条被重启打断的识别,已改为需重拍", n)
	}
}

func (s *SQLiteStore) ResetInterruptedRecognitions(reason string) (int, error) {
	res, err := s.db.Exec(`
		UPDATE records SET recognition_status='retake_required', retake_reason=?, updated_at=?
		WHERE recognition_status IN ('processing','queued') AND submitted=0`,
		reason, fmtStamp(time.Now()))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := s.db.Exec(`
		UPDATE ai_tasks SET status='failed', error_code='interrupted', error_message=?, updated_at=?
		WHERE status IN ('queued','processing')`,
		reason, nowStamp()); err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (s *MemStore) ResetInterruptedRecognitions(reason string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rec := range s.records {
		if rec.Submitted || (rec.RecognitionStatus != "processing" && rec.RecognitionStatus != "queued") {
			continue
		}
		rec.RecognitionStatus = "retake_required"
		rec.RetakeReason = reason
		rec.UpdatedAt = time.Now()
		n++
	}
	for _, t := range s.tasks {
		if t.Status == "queued" || t.Status == "processing" {
			t.Status = "failed"
			t.ErrorCode = "interrupted"
			t.ErrorMessage = reason
			t.UpdatedAt = time.Now()
		}
	}
	return n, nil
}
