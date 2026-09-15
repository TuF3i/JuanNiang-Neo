package dao

import (
	"context"

	"JuanNiang-Neo/internal/core/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JoinReviewDAO 加群审核（AI 攒批审核）持久化：配置 / 待审请求 / 审核记录。
type JoinReviewDAO struct{ db *gorm.DB }

// NewJoinReviewDAO 构造加群审核 DAO。
func NewJoinReviewDAO(db *gorm.DB) *JoinReviewDAO { return &JoinReviewDAO{db: db} }

// ---------- 配置（单行） ----------

// InitConfig 写入默认配置（单行 ID=1，OnConflict DoNothing 幂等）。
func (d *JoinReviewDAO) InitConfig(ctx context.Context) error {
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&models.GroupJoinReviewConfig{
		ID:           1,
		BatchSize:    5,
		FlushSeconds: 60,
	}).Error
}

// GetConfig 读取配置。
func (d *JoinReviewDAO) GetConfig(ctx context.Context) (*models.GroupJoinReviewConfig, error) {
	var cfg models.GroupJoinReviewConfig
	if err := d.db.WithContext(ctx).Where("id = 1").First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig 更新配置。
func (d *JoinReviewDAO) UpdateConfig(ctx context.Context, cfg *models.GroupJoinReviewConfig) error {
	return d.db.WithContext(ctx).Where("id = 1").Save(cfg).Error
}

// ---------- 待审请求 ----------

// RequestCreate 新增待审请求。
func (d *JoinReviewDAO) RequestCreate(ctx context.Context, req *models.GroupJoinRequest) error {
	return d.db.WithContext(ctx).Create(req).Error
}

// RequestList 列出全部待审请求（新申请在前）。
func (d *JoinReviewDAO) RequestList(ctx context.Context) ([]models.GroupJoinRequest, error) {
	var list []models.GroupJoinRequest
	err := d.db.WithContext(ctx).Order("created_at DESC, id DESC").Find(&list).Error
	return list, err
}

// RequestGet 按 ID 读取待审请求。
func (d *JoinReviewDAO) RequestGet(ctx context.Context, id uint) (*models.GroupJoinRequest, error) {
	var req models.GroupJoinRequest
	if err := d.db.WithContext(ctx).First(&req, id).Error; err != nil {
		return nil, err
	}
	return &req, nil
}

// RequestDelete 删除待审请求（审核终态落库后调用；行存在即 pending）。
func (d *JoinReviewDAO) RequestDelete(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&models.GroupJoinRequest{}, id).Error
}

// ---------- 审核记录 ----------

// ReviewCreate 写入审核记录（AI 与人工统一入口）。
func (d *JoinReviewDAO) ReviewCreate(ctx context.Context, rec *models.GroupJoinReview) error {
	return d.db.WithContext(ctx).Create(rec).Error
}

// ReviewListPaged 分页列出审核记录（最新在前），返回总数与当前页。
func (d *JoinReviewDAO) ReviewListPaged(ctx context.Context, page, pageSize int) (int64, []models.GroupJoinReview, error) {
	var total int64
	if err := d.db.WithContext(ctx).Model(&models.GroupJoinReview{}).Count(&total).Error; err != nil {
		return 0, nil, err
	}
	var list []models.GroupJoinReview
	err := d.db.WithContext(ctx).
		Order("reviewed_at DESC, id DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&list).Error
	return total, list, err
}
