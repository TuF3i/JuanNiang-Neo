package dao

import (
	"context"
	"errors"

	"JuanNiang-Neo/internal/core/models"

	"gorm.io/gorm"
)

type ReplyStrategyDAO struct{ db *gorm.DB }

func NewReplyStrategyDAO(db *gorm.DB) *ReplyStrategyDAO { return &ReplyStrategyDAO{db: db} }

// GetOrCreate 获取当前策略配置，不存在则创建默认行（always）。
func (d *ReplyStrategyDAO) GetOrCreate(ctx context.Context) (*models.ReplyStrategyConfig, error) {
	var cfg models.ReplyStrategyConfig
	err := d.db.WithContext(ctx).First(&cfg).Error
	if err == nil {
		return &cfg, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	cfg = models.ReplyStrategyConfig{
		ID:                 newUUID(),
		Strategy:           models.StrategyRelevance,
		RelevanceThreshold: 0.5,
		RelevanceTimeout:   10,
		JudgeFailPolicy:    "drop",
	}
	if err := d.db.WithContext(ctx).Create(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Update 更新策略配置。
func (d *ReplyStrategyDAO) Update(ctx context.Context, cfg *models.ReplyStrategyConfig) error {
	return d.db.WithContext(ctx).Save(cfg).Error
}
