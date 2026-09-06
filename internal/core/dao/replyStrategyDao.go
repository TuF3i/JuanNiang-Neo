package dao

import (
	"context"
	"errors"

	"JuanNiang-Neo/internal/core/models"

	"gorm.io/gorm"
)

type ReplyStrategyDAO struct{ db *gorm.DB }

func NewReplyStrategyDAO(db *gorm.DB) *ReplyStrategyDAO { return &ReplyStrategyDAO{db: db} }

// GetOrCreate 获取当前策略配置，不存在则创建默认行（参与窗口默认参数）。
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
		ID:                     newUUID(),
		QuietGapSeconds:        5,
		ForceCount:             5,
		MaxAgeSeconds:          20,
		WindowMaxMsgs:          20,
		JitterSeconds:          2,
		ForceCountJitter:       1,
		ParticipateProbability: 0.8,
		TypingDelayMaxMs:       1500,
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
