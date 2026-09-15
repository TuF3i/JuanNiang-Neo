package core

import (
	"context"
	"os"
	"sync"

	"JuanNiang-Neo/internal/core/acl"
	"JuanNiang-Neo/internal/core/cache"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/models"

	"JuanNiang-Neo/internal/logging"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var log = logging.NewModule("core")

// AutoMigrate 自动迁移所有 GORM 模型。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.AdminUser{},
		&models.Provider{},
		&models.MCPServer{},
		&models.Skill{},
		&models.ToolConfig{},
		&models.Prompt{},
		&models.ChatArea{},
		&models.Session{},
		&models.ShortTermMemory{},
		&models.LongTermMemory{},
		&models.LongTermMemoryItem{},
		&models.BackgroundTask{},
		&models.ChatRecord{},
		&models.Plugin{},
		&models.ACLRule{},
		&models.Onebot11Adapter{},
		&models.T2IConfig{},
		&models.SandboxConfig{},
		&models.RAGConfig{},
		&models.WebhookConfig{},
		&models.CronJob{},
		&models.ReplyStrategyConfig{},
		&models.SkillMemory{},
		&models.TokenUsageDaily{},
		&models.KnowledgeItem{},
		&models.ImageAsset{},
		&models.ImageFolder{},
		&models.Sticker{},
		&models.StickerTag{},
		&models.FishCalendarConfig{},
		&models.FishCalendarAffair{},
		&models.ScheduledMessage{},
		&models.GroupMgrConfig{},
		&models.GroupMgrWord{},
		&models.GroupMgrSample{},
		&models.GroupMgrViolation{},
		&models.GroupMgrWhitelist{},
		&models.GroupMgrAdmin{},
		&models.GroupMgrStat{},
		&models.GroupJoinRequest{},
		&models.GroupJoinReview{},
		&models.GroupJoinReviewConfig{},
	)
}

// InitAdminUser 首次启动时创建管理员账户 (初始密码 Admin123)。
func InitAdminUser(ctx context.Context, userDAO *dao.UserDAO) error {
	exists, err := userDAO.Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		log.Info("管理员用户已存在，跳过初始化")
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("Admin123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := &models.AdminUser{
		Username:     "admin",
		PasswordHash: string(hash),
		Role:         "admin",
	}
	if err := userDAO.Create(ctx, user); err != nil {
		return err
	}
	log.Warn("已创建默认管理员用户", "username", "admin", "password", "Admin123")
	return nil
}

// Core 聚合所有核心模块的初始化结果。
type Core struct {
	DB    *gorm.DB
	Cache *cache.Cache
	DAO   *dao.Bundle
	ACL   *acl.ACL
}

var (
	instance *Core
	once     sync.Once
)

// Init 初始化核心模块 (DB + Redis + AutoMigrate + ACL + AdminUser)。
// 仅在首次启动时执行数据库迁移和用户初始化。
func Init(ctx context.Context, db *gorm.DB, redisClient *redis.Client) (*Core, error) {
	var initErr error
	once.Do(func() {
		if err := AutoMigrate(db); err != nil {
			initErr = err
			return
		}

		// 迁移：移除旧普通唯一索引（不允许软删后重名，SQLSTATE 23505）。
		// 新部分唯一索引（WHERE deleted_at IS NULL）已由 AutoMigrate 按新索引名创建，
		// 旧索引继续阻塞软删后重建同名记录，这里幂等清理（含 image_folders 历史索引）。
		for _, idx := range []string{
			"idx_image_folders_name",   // image_folders 历史索引
			"idx_sticker_tags_name",    // sticker_tags
			"idx_plugins_name",         // plugins
			"idx_admin_users_username", // admin_users
			"idx_group_mgr_words_word", // group_mgr_words 旧普通唯一索引（软删后重建同名冲突）
		} {
			if err := db.Exec("DROP INDEX IF EXISTS " + idx).Error; err != nil {
				initErr = err
				return
			}
		}

		// 群管理词条：PG 部分唯一索引（WHERE deleted_at IS NULL）——软删后允许重建同名词条，
		// 且仍保证「活动词条不重名」。SQLite 测试环境无此索引，由 WordUpsert 软删行复活逻辑兕底。
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_gm_words_word_active " +
				"ON group_mgr_words (word) WHERE deleted_at IS NULL").Error; err != nil {
				initErr = err
				return
			}
		}

		// 回复策略收敛为仅 relevance：存量行（never_reply/at_only/always）
		// 统一迁移到唯一策略，避免历史配置在只保留 relevance 后失效或行为歧义。
		if err := db.Exec("UPDATE reply_strategy_config SET strategy = ? WHERE strategy <> ?",
			models.StrategyRelevance, models.StrategyRelevance).Error; err != nil {
			initErr = err
			return
		}

		// 长期记忆语义召回索引：pg_trgm 三元组倒排（GIN），加速 content ILIKE 子串匹配
		// 仅 PostgreSQL 方言生效（SQLite 测试环境无此扩展，跳过）。
		// 托管 PG（RDS/Cloud SQL 等）常禁止 CREATE EXTENSION 权限：失败降级 Warn，
		// 语义召回自动回退 recent/SQL 匹配——与 RAG/群管理「降级不报错」设计一致，不阻断启动。
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm").Error; err != nil {
				log.Warn("pg_trgm 扩展创建失败，长期记忆语义召回降级为 recent/SQL 匹配", "err", err)
			} else if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_ltm_content_trgm " +
				"ON long_term_memory_items USING GIN (content gin_trgm_ops)").Error; err != nil {
				log.Warn("长期记忆 trgm 索引创建失败，语义召回降级", "err", err)
			}
		}

		// 群管理语录集合：存量样本回填 list_type=black（AutoMigrate 加列默认值已覆盖新行，
		// 此处幂等兜底历史行），存量样本即黑名单语录。
		if err := db.Exec("UPDATE group_mgr_samples SET list_type = 'black' WHERE list_type IS NULL OR list_type = ''").Error; err != nil {
			initErr = err
			return
		}

		cacheInst := cache.NewCache(redisClient, os.Getenv("REDIS_PREFIX"))

		bundle := dao.NewBundle(db)
		aclInst := acl.NewACL(bundle.ACL)

		if err := InitAdminUser(ctx, bundle.User); err != nil {
			initErr = err
			return
		}

		// 系统内置「常用」表情标签：幂等创建（不存在则建），不可删除
		if err := bundle.Sticker.EnsureCommonTag(ctx); err != nil {
			initErr = err
			return
		}

		instance = &Core{
			DB:    db,
			Cache: cacheInst,
			DAO:   bundle,
			ACL:   aclInst,
		}
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Get 返回单例 Core 实例。如果尚未初始化则返回 nil。
func Get() *Core {
	return instance
}
