package service

import (
	"context"
	"strconv"
	"time"

	"JuanNiang-Neo/internal/api/dto"
	"JuanNiang-Neo/internal/core/models"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ---------- 加群审核（AI 攒批审核） ----------

// joinReviewConfigResp 模型 → 配置 DTO。
func joinReviewConfigResp(cfg *models.GroupJoinReviewConfig) dto.JoinReviewConfigResp {
	if cfg == nil {
		return dto.JoinReviewConfigResp{
			EnabledGroups: []int64{},
			Prompts:       map[string]string{},
			BatchSize:     5,
			FlushSeconds:  60,
		}
	}
	prompts := map[string]string{}
	for k, v := range cfg.Prompts {
		prompts[k] = v
	}
	groups := make([]int64, 0, len(cfg.EnabledGroups))
	groups = append(groups, cfg.EnabledGroups...)
	return dto.JoinReviewConfigResp{
		EnabledGroups: groups,
		Prompts:       prompts,
		BatchSize:     cfg.BatchSize,
		FlushSeconds:  cfg.FlushSeconds,
	}
}

// GetJoinReviewConfig 读取加群审核配置（未初始化则写入默认配置）。
func (s *Service) GetJoinReviewConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.JoinReview.GetConfig(ctx)
	if err != nil {
		if initErr := s.DAO.JoinReview.InitConfig(ctx); initErr != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: initErr.Error()}))
			return
		}
		cfg, err = s.DAO.JoinReview.GetConfig(ctx)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, joinReviewConfigResp(cfg)))
}

// UpdateJoinReviewConfig 更新加群审核配置并热重载 Manager（攒批参数/生效群/提示词即时生效）。
func (s *Service) UpdateJoinReviewConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateJoinReviewConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	cfg, err := s.DAO.JoinReview.GetConfig(ctx)
	if err != nil {
		_ = s.DAO.JoinReview.InitConfig(ctx)
		cfg, _ = s.DAO.JoinReview.GetConfig(ctx)
	}
	if cfg == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "配置不存在"}))
		return
	}
	groups := models.Int64Slice(data.EnabledGroups)
	if groups == nil {
		groups = models.Int64Slice{}
	}
	prompts := models.StringMap(data.Prompts)
	if prompts == nil {
		prompts = models.StringMap{}
	}
	cfg.EnabledGroups = groups
	cfg.Prompts = prompts
	if data.BatchSize > 0 {
		cfg.BatchSize = data.BatchSize
	}
	if data.FlushSeconds > 0 {
		cfg.FlushSeconds = data.FlushSeconds
	}
	if err := s.DAO.JoinReview.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.JoinReview != nil {
		_ = s.JoinReview.Reload(ctx)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, joinReviewConfigResp(cfg)))
}

// ListJoinReviewRequests 待审加群请求列表（全量，新申请在前）。
func (s *Service) ListJoinReviewRequests(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.JoinReview.RequestList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.JoinReviewRequestResp, 0, len(list))
	for _, r := range list {
		resp = append(resp, dto.JoinReviewRequestResp{
			ID:        r.ID,
			GroupID:   r.GroupID,
			UserID:    r.UserID,
			Username:  r.Username,
			Comment:   r.Comment,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// DecideJoinReviewRequest 人工审核（通过/拒绝），理由随动作发送给申请者。
func (s *Service) DecideJoinReviewRequest(ctx context.Context, c *app.RequestContext) {
	id := parseUintParam(c, "id")
	if id == 0 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.JoinRequestNotExist, dto.ErrorDetail{ErrorDetail: "非法 ID"}))
		return
	}
	var data dto.JoinReviewDecisionReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.JoinReview == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "加群审核未启用"}))
		return
	}
	if err := s.JoinReview.ManualDecide(ctx, id, data.Approve, data.Reason); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.JoinRequestNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ListJoinReviewRecords 审核记录（服务端分页，最新在前）。
func (s *Service) ListJoinReviewRecords(ctx context.Context, c *app.RequestContext) {
	page := parsePageParam(c, "page", 1)
	pageSize := parsePageParam(c, "page_size", 15)
	if pageSize > 100 {
		pageSize = 100
	}
	total, list, err := s.DAO.JoinReview.ReviewListPaged(ctx, page, pageSize)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.JoinReviewRecordResp, 0, len(list))
	for _, r := range list {
		resp = append(resp, dto.JoinReviewRecordResp{
			ID:         r.ID,
			GroupID:    r.GroupID,
			UserID:     r.UserID,
			Username:   r.Username,
			Comment:    r.Comment,
			Verdict:    r.Verdict,
			Reviewer:   r.Reviewer,
			Reason:     r.Reason,
			ReviewedAt: r.ReviewedAt.Format(time.RFC3339),
		})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.JoinReviewRecordListResp{Total: total, List: resp}))
}

// parsePageParam 解析分页查询参数（缺省/非法返回默认值，最小 1）。
func parsePageParam(c *app.RequestContext, name string, def int) int {
	v, err := strconv.Atoi(c.Query(name))
	if err != nil || v < 1 {
		return def
	}
	return v
}
