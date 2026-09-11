package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"JuanNiang-Neo/internal/api/dto"
	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/core/ragtag"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
)

// ---------- 群管理 ----------

// 词库导入限制：单文件 ≤ 1MB、行数 ≤ 20000（防超大上传内存 DoS）。
const (
	maxWordImportSize  = 1 << 20
	maxWordImportLines = 20000
)

// u32str uint → 十进制字符串（RAG tag 派生用）。
func u32str(v uint) string { return strconv.FormatUint(uint64(v), 10) }

// GetGroupMgrConfig 读取群管理配置（未初始化则写入默认配置）。
func (s *Service) GetGroupMgrConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.GroupMgr.GetConfig(ctx)
	if err != nil {
		if initErr := s.DAO.GroupMgr.InitConfig(ctx); initErr != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: initErr.Error()}))
			return
		}
		cfg, err = s.DAO.GroupMgr.GetConfig(ctx)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, groupMgrConfigResp(cfg)))
}

// UpdateGroupMgrConfig 更新群管理配置并热重载 Manager。
func (s *Service) UpdateGroupMgrConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateGroupMgrConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	cfg, err := s.DAO.GroupMgr.GetConfig(ctx)
	if err != nil {
		_ = s.DAO.GroupMgr.InitConfig(ctx)
		cfg, _ = s.DAO.GroupMgr.GetConfig(ctx)
	}
	if cfg == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "配置不存在"}))
		return
	}
	cfg.Enabled = data.Enabled
	cfg.LLMReview = data.LLMReview
	if data.BlackMinScore > 0 && data.BlackMinScore <= 1 {
		cfg.BlackMinScore = data.BlackMinScore
	}
	if data.LLMBatchWindow > 0 && data.LLMBatchWindow <= 60 {
		cfg.LLMBatchWindow = data.LLMBatchWindow
	}
	// 检测参数（图片刷屏 / 复读 / 惩罚时长），非法值忽略保留原值
	if data.ImgSpamWindow > 0 {
		cfg.ImgSpamWindow = data.ImgSpamWindow
	}
	if data.ImgSpamThreshold > 0 {
		cfg.ImgSpamThreshold = data.ImgSpamThreshold
	}
	if data.ImgMuteDuration > 0 {
		cfg.ImgMuteDuration = data.ImgMuteDuration
	}
	cfg.EnableCopyCheck = data.EnableCopyCheck
	if data.CopyThreshold > 0 {
		cfg.CopyThreshold = data.CopyThreshold
	}
	if data.ViolationMuteSeconds > 0 {
		cfg.ViolationMuteSeconds = data.ViolationMuteSeconds
	}
	cfg.ExcludeGroups = models.JSONSlice(data.ExcludeGroups)
	cfg.LLMPrompt = data.LLMPrompt
	cfg.LLMCriteria = data.LLMCriteria
	cfg.LLMGrayPrompt = data.LLMGrayPrompt
	cfg.LLMHighRiskPrompt = data.LLMHighRiskPrompt
	if err := s.DAO.GroupMgr.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.GroupMgr != nil {
		_ = s.GroupMgr.Reload(ctx)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, groupMgrConfigResp(cfg)))
}

// ListGroupMgrWords 词条列表（已废弃：关键词词库改为只读内存兜底，从 txt 加载，不入 DB）。
// 返回空列表，兼容旧客户端调用。
func (s *Service) ListGroupMgrWords(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, []dto.GroupMgrWordResp{}))
}

// AddGroupMgrWord 新增词条（已废弃：关键词词库改为只读内存兜底，从 txt 加载，不入 DB/RAG）。
func (s *Service) AddGroupMgrWord(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "关键词词库已改为只读内存兜底（从 txt 加载），不再支持新增"}))
}

// DeleteGroupMgrWord 删除词条（已废弃：关键词词库改为只读内存兜底，从 txt 加载，不入 DB/RAG）。
func (s *Service) DeleteGroupMgrWord(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "关键词词库已改为只读内存兜底（从 txt 加载），不再支持删除"}))
}

// ImportGroupMgrWords txt 导入（已废弃：关键词词库改为只读内存兜底，从 txt 加载，不入 DB/RAG）。
func (s *Service) ImportGroupMgrWords(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "关键词词库已改为只读内存兜底（从 txt 加载），不再支持导入"}))
}

// SyncGroupMgrRAG 手动全量同步向量库（仅违禁语录；关键词词库不入 RAG）。
func (s *Service) SyncGroupMgrRAG(ctx context.Context, c *app.RequestContext) {
	if s.GroupMgr == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "群管理未初始化"}))
		return
	}
	total, failed, err := s.GroupMgr.SyncRAG(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.GroupMgrSyncResp{Total: total, Failed: failed}))
}

// SyncGroupMgrRAGStream 全量同步向量库（SSE 流式）：每批同步后推送 progress 事件，
// 完成后推送 done（词条量大时避免单次 HTTP 请求超时，Web 端 EventSource 消费实时进度）。
// GET /group-mgr/sync-rag/stream
func (s *Service) SyncGroupMgrRAGStream(ctx context.Context, c *app.RequestContext) {
	if s.GroupMgr == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "群管理未初始化"}))
		return
	}
	w := sse.NewWriter(c)
	defer func() { _ = w.Close() }()
	push := func(event string, data any) bool {
		b, err := json.Marshal(data)
		if err != nil {
			log.Warn("SSE 序列化失败", "event", event, "err", err)
			return false
		}
		return w.WriteEvent("", event, b) == nil
	}

	push("start", map[string]string{"status": "syncing"})
	total, failed, err := s.GroupMgr.SyncRAGProgress(ctx, func(done, fail int) error {
		if !push("progress", map[string]int{"done": done, "failed": fail}) {
			return fmt.Errorf("客户端已断开")
		}
		return ctx.Err() // 客户端断开时中止同步
	})
	if err != nil {
		if ctx.Err() != nil {
			return // 客户端断开，不再推送
		}
		push("error", map[string]string{"error": err.Error()})
		return
	}
	push("done", map[string]int{"total": total, "failed": failed})
}

// ListGroupMgrSamples 语录列表（?list_type=black/white 过滤；违禁语录管理页）。
func (s *Service) ListGroupMgrSamples(ctx context.Context, c *app.RequestContext) {
	// 白名单语录体系已剔除：列表只返回黑名单（存量 white 行保留不展示）
	list, err := s.DAO.GroupMgr.SampleListByList(ctx, "black")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.GroupMgrSampleResp, 0, len(list))
	for _, sp := range list {
		var lu *string
		if sp.LastUsedAt != nil {
			s := sp.LastUsedAt.Format("2006-01-02 15:04:05")
			lu = &s
		}
		// 派生 RAG tag（面板 UUID 展示/对账用，与检索侧一致）
		tag := ragtag.Sample(u32str(sp.ID))
		resp = append(resp, dto.GroupMgrSampleResp{
			ID: sp.ID, WordID: sp.WordID, ListType: sp.ListType, Text: sp.Text, Category: sp.Category, Source: sp.Source,
			HitCount: sp.HitCount, RAGSynced: sp.RAGSynced, RAGTag: tag.String(),
			LastUsedAt: lu, CreatedAt: sp.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// AddGroupMgrPhrase 新增黑名单违禁语录（单条添加；白名单语录体系已剔除）。
func (s *Service) AddGroupMgrPhrase(ctx context.Context, c *app.RequestContext) {
	var data dto.AddGroupMgrPhraseReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	data.Text = strings.TrimSpace(data.Text)
	if data.Text == "" || (data.ListType != "" && data.ListType != "black") {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "text 为空或 list_type 非法（仅 black）"}))
		return
	}
	if len([]rune(data.Text)) > 200 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "语录过长（≤200 字）"}))
		return
	}
	category := data.Category
	if category != "sensitive" {
		category = "ad"
	}
	if s.GroupMgr != nil {
		if _, err := s.GroupMgr.AddPhrase(ctx, data.Text, category, data.ListType); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ImportGroupMgrPhrases txt 导入违禁语录（一行一个；?list_type= 指定集合，?category= 违规类别）。
// 限制：单文件 ≤ 1MB、行数 ≤ 20000。
func (s *Service) ImportGroupMgrPhrases(ctx context.Context, c *app.RequestContext) {
	listType := strings.TrimSpace(c.Query("list_type"))
	if listType != "black" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "list_type 非法（仅 black）"}))
		return
	}
	category := strings.TrimSpace(c.Query("category"))
	if category != "sensitive" {
		category = "ad"
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "缺少 file 字段: " + err.Error()}))
		return
	}
	if fh.Size > maxWordImportSize {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.WordImportTooLarge, nil))
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	defer f.Close()
	data := make([]byte, 0, fh.Size)
	buf := make([]byte, 4096)
	for {
		n, rerr := f.Read(buf)
		data = append(data, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxWordImportLines {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.WordImportTooLarge, nil))
		return
	}
	imported, skipped := 0, 0
	seen := map[string]bool{}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || seen[t] {
			skipped++
			continue
		}
		seen[t] = true
		if s.GroupMgr != nil {
			if _, err := s.GroupMgr.AddPhrase(ctx, t, category, listType); err != nil {
				skipped++
				continue
			}
		}
		imported++
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]int{"imported": imported, "skipped": skipped}))
}

// DeleteGroupMgrSample 删除样本（双删 RAG）。
func (s *Service) DeleteGroupMgrSample(ctx context.Context, c *app.RequestContext) {
	id := parseUintParam(c, "id")
	if s.GroupMgr != nil {
		if err := s.GroupMgr.DeleteSample(ctx, id); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	} else if err := s.DAO.GroupMgr.SampleDelete(ctx, id); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ListGroupMgrViolations 违规记录。
func (s *Service) ListGroupMgrViolations(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.GroupMgr.ViolationList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.GroupMgrViolationResp, 0, len(list))
	for _, v := range list {
		resp = append(resp, dto.GroupMgrViolationResp{ID: v.ID, GroupID: v.GroupID, UserID: v.UserID, Username: v.Username, Count: v.Count, DetectionPath: v.DetectionPath, LLMReason: v.LLMReason})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// DeleteGroupMgrViolation 删除某条违规记录（面板"删除某行即重置该用户违规"）。
func (s *Service) DeleteGroupMgrViolation(ctx context.Context, c *app.RequestContext) {
	id := parseUintParam(c, "id")
	if err := s.DAO.GroupMgr.ViolationDelete(ctx, id); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// GetGroupMgrWhitelist 白名单列表。
func (s *Service) GetGroupMgrWhitelist(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.GroupMgr.WlList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	qqs := make([]int64, 0, len(list))
	for _, w := range list {
		qqs = append(qqs, w.QQ)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.GroupMgrQQListResp{QQList: qqs}))
}

// UpdateGroupMgrWhitelist 白名单全量覆盖。
func (s *Service) UpdateGroupMgrWhitelist(ctx context.Context, c *app.RequestContext) {
	s.updateGroupMgrQQList(ctx, c, true)
}

// GetGroupMgrAdmins 手动管理员列表。
func (s *Service) GetGroupMgrAdmins(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.GroupMgr.AdminList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	qqs := make([]int64, 0, len(list))
	for _, a := range list {
		qqs = append(qqs, a.QQ)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.GroupMgrQQListResp{QQList: qqs}))
}

// SyncGroupMgrAdminsFromAdapter 从 Adapter.Admins 同步管理员到群管理手动管理员表
// （去重合并，返回新增数量）。面板「从 Adapter 同步管理员」按钮。
func (s *Service) SyncGroupMgrAdminsFromAdapter(ctx context.Context, c *app.RequestContext) {
	if s.GroupMgr == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "群管理未初始化"}))
		return
	}
	added, err := s.GroupMgr.SyncAdminsFromAdapter(ctx, s.Adapter.Admins())
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]int{"added": added}))
}

// UpdateGroupMgrAdmins 手动管理员全量覆盖。
func (s *Service) UpdateGroupMgrAdmins(ctx context.Context, c *app.RequestContext) {
	s.updateGroupMgrQQList(ctx, c, false)
}

// updateGroupMgrQQList 全量覆盖白名单/手动管理员（whitelist=true 时操作白名单，否则操作管理员）。
func (s *Service) updateGroupMgrQQList(ctx context.Context, c *app.RequestContext, whitelist bool) {
	var data dto.UpdateGroupMgrQQListReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if whitelist {
		cur, _ := s.DAO.GroupMgr.WlList(ctx)
		for _, w := range cur {
			_ = s.DAO.GroupMgr.WlDelete(ctx, w.QQ)
		}
		for _, qq := range data.QQList {
			_ = s.DAO.GroupMgr.WlAdd(ctx, qq)
		}
	} else {
		cur, _ := s.DAO.GroupMgr.AdminList(ctx)
		for _, a := range cur {
			_ = s.DAO.GroupMgr.AdminDelete(ctx, a.QQ)
		}
		for _, qq := range data.QQList {
			_ = s.DAO.GroupMgr.AdminAdd(ctx, qq)
		}
	}
	if s.GroupMgr != nil {
		_ = s.GroupMgr.Reload(ctx)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// GetGroupMgrStats 统计（?group_id= 必填，Web 统计面板与 /groupstats 同源）。
func (s *Service) GetGroupMgrStats(ctx context.Context, c *app.RequestContext) {
	if s.GroupMgr == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "群管理未初始化"}))
		return
	}
	groupID, err := parseI64Param(c, "group_id")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "group_id 非法"}))
		return
	}
	st, err := s.GroupMgr.GroupStats(ctx, groupID)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.GroupMgrStatsResp{
		GroupID: st.GroupID, Date: st.Date, JoinToday: st.JoinToday, Warns: st.Warns,
		Mutes: st.Mutes, CopyWarns: st.CopyWarns, Ad: st.Ad, Sensitive: st.Sensitive, Kicks: st.Kicks,
	}))
}

// TestGroupMgr 链路测试：文本 → 判定流水（不处罚不写库）。
func (s *Service) TestGroupMgr(ctx context.Context, c *app.RequestContext) {
	if s.GroupMgr == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "群管理未初始化"}))
		return
	}
	var data dto.TestGroupMgrReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	rep := s.GroupMgr.TestViolation(ctx, data.Text)
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, rep))
}

// groupMgrConfigResp 模型 → DTO 响应映射（面板展示用）。
func groupMgrConfigResp(cfg *models.GroupMgrConfig) dto.GroupMgrConfigResp {
	return dto.GroupMgrConfigResp{
		Enabled: cfg.Enabled, LLMReview: cfg.LLMReview,
		BlackMinScore: cfg.BlackMinScore,
		LLMBatchWindow: cfg.LLMBatchWindow,
		ImgSpamWindow:  cfg.ImgSpamWindow, ImgSpamThreshold: cfg.ImgSpamThreshold, ImgMuteDuration: cfg.ImgMuteDuration,
		EnableCopyCheck: cfg.EnableCopyCheck, CopyThreshold: cfg.CopyThreshold,
		ViolationMuteSeconds: cfg.ViolationMuteSeconds,
		ExcludeGroups:        cfg.ExcludeGroups,
		LLMPrompt:            cfg.LLMPrompt, LLMCriteria: cfg.LLMCriteria, LLMGrayPrompt: cfg.LLMGrayPrompt, LLMHighRiskPrompt: cfg.LLMHighRiskPrompt,
	}
}

// parseUintParam 解析路径参数为 uint（非法返回 0）。
func parseUintParam(c *app.RequestContext, name string) uint {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

// parseI64Param 解析查询参数为 int64。
func parseI64Param(c *app.RequestContext, name string) (int64, error) {
	return strconv.ParseInt(c.Query(name), 10, 64)
}
