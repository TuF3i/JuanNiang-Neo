package dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/core/ragtag"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GroupMgrDAO 群管理系统功能的全部持久化（config/words/samples/violations/whitelist/admins/stats）。
type GroupMgrDAO struct{ db *gorm.DB }

// NewGroupMgrDAO 构造群管理 DAO。
func NewGroupMgrDAO(db *gorm.DB) *GroupMgrDAO { return &GroupMgrDAO{db: db} }

// u64toa uint64 → 十进制字符串（RAG tag 派生用）。
func u64toa(v uint64) string { return strconv.FormatUint(v, 10) }

// ---------- 配置（单行） ----------

// Initialize default config (single row; default disabled, user enables via panel).
func (d *GroupMgrDAO) InitConfig(ctx context.Context) error {
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&models.GroupMgrConfig{
		ID:                   1,
		BlackMinScore:        0.7,
		LLMBatchWindow:       3,
		ImgSpamWindow:        2,
		ImgSpamThreshold:     3,
		ImgMuteDuration:      60,
		EnableCopyCheck:      true,
		CopyThreshold:        3,
		ViolationMuteSeconds: 1800,
	}).Error
}

// GetConfig 读取配置。
func (d *GroupMgrDAO) GetConfig(ctx context.Context) (*models.GroupMgrConfig, error) {
	var cfg models.GroupMgrConfig
	if err := d.db.WithContext(ctx).Where("id = 1").First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig 更新配置。
func (d *GroupMgrDAO) UpdateConfig(ctx context.Context, cfg *models.GroupMgrConfig) error {
	return d.db.WithContext(ctx).Where("id = 1").Save(cfg).Error
}

// ---------- 词条 ----------

// WordListAll 列出全部词条（按分类+词序）。
func (d *GroupMgrDAO) WordListAll(ctx context.Context) ([]models.GroupMgrWord, error) {
	var list []models.GroupMgrWord
	err := d.db.WithContext(ctx).Order("category ASC, word ASC").Find(&list).Error
	return list, err
}

// WordListByCategory 按分类列出词条。
func (d *GroupMgrDAO) WordListByCategory(ctx context.Context, category string) ([]models.GroupMgrWord, error) {
	var list []models.GroupMgrWord
	err := d.db.WithContext(ctx).Where("category = ?", category).Order("word ASC").Find(&list).Error
	return list, err
}

// WordUpsert 新增/更新词条（幂等、并发安全）。
// 处理路径：
//  1. Unscoped 查询含软删行：软删行复活（deleted_at 置空）并更新分类/来源；活动行直接更新；
//  2. 均不存在则插入（OnConflict DoNothing 防并发双插，冲突后重新查询返回已有行）；
//  3. 新插入行回填派生 RAG tag（ragtag.Word(id) → v5 UUID）。
//
// 复活/更新不重置 RAGSynced（由 syncRAG / AddWord 完成后置位）。
func (d *GroupMgrDAO) WordUpsert(ctx context.Context, word, category, source string) (uint, error) {
	var w models.GroupMgrWord
	err := d.db.WithContext(ctx).Unscoped().Where("word = ?", word).First(&w).Error
	if err == nil {
		if w.DeletedAt.Valid {
			// 软删行复活：普通唯一索引下也允许同词重建（清 deleted_at 后不与其他活动行冲突）
			if err := d.db.WithContext(ctx).Unscoped().Model(&w).
				UpdateColumns(map[string]any{"deleted_at": nil, "category": category, "source": source}).Error; err != nil {
				return 0, err
			}
			return w.ID, nil
		}
		w.Category = category
		w.Source = source
		if err := d.db.WithContext(ctx).Save(&w).Error; err != nil {
			return 0, err
		}
		return w.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	w = models.GroupMgrWord{Word: word, Category: category, Source: source}
	// OnConflict DoNothing：并发双插时一方插入失败，重新查询返回已有行
	if err := d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&w).Error; err != nil {
		return 0, err
	}
	if w.ID == 0 {
		if err := d.db.WithContext(ctx).Where("word = ?", word).First(&w).Error; err != nil {
			return 0, err
		}
		return w.ID, nil
	}
	// 派生 RAG tag（与检索侧 ragtag.Word(id) 一致，幂等可复现）
	w.RAGTag = ragtag.Word(u64toa(uint64(w.ID))).String()
	if err := d.db.WithContext(ctx).Model(&w).UpdateColumn("rag_tag", w.RAGTag).Error; err != nil {
		return 0, err
	}
	return w.ID, nil
}

// WordMarkRAGSynced 标记词条 RAG 同步状态（syncRAG/导入/删除后调用）。
func (d *GroupMgrDAO) WordMarkRAGSynced(ctx context.Context, id uint, synced bool) error {
	return d.db.WithContext(ctx).Model(&models.GroupMgrWord{}).Where("id = ?", id).
		UpdateColumns(map[string]any{"rag_synced": synced, "updated_at": time.Now()}).Error
}

// WordSetRAGTag 回填派生 RAG tag（历史词条/导入路径补 tag 用）。
func (d *GroupMgrDAO) WordSetRAGTag(ctx context.Context, id uint, tag string) error {
	return d.db.WithContext(ctx).Model(&models.GroupMgrWord{}).Where("id = ?", id).
		UpdateColumn("rag_tag", tag).Error
}

// WordListUnsynced 列出未同步到 RAG 的词条（同步按钮增量处理用）。
func (d *GroupMgrDAO) WordListUnsynced(ctx context.Context) ([]models.GroupMgrWord, error) {
	var list []models.GroupMgrWord
	err := d.db.WithContext(ctx).Where("rag_synced = ?", false).Order("id ASC").Find(&list).Error
	return list, err
}

// WordGet 按 ID 读取词条（含软删行，删除词条时取文本用）。
func (d *GroupMgrDAO) WordGet(ctx context.Context, id uint) (*models.GroupMgrWord, error) {
	var w models.GroupMgrWord
	if err := d.db.WithContext(ctx).Unscoped().First(&w, id).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// WordDelete 删除词条（软删）。软删后重建同名由部分唯一索引（PG）+
// WordUpsert 软删行复活（全方言）双重保障，不报唯一索引冲突。
func (d *GroupMgrDAO) WordDelete(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&models.GroupMgrWord{}, id).Error
}

// WordCount 词条总数（种子导入判断用）。
func (d *GroupMgrDAO) WordCount(ctx context.Context) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&models.GroupMgrWord{}).Count(&n).Error
	return n, err
}

// ---------- 样本 ----------

// SampleListAll 列出全部样本。
func (d *GroupMgrDAO) SampleListAll(ctx context.Context) ([]models.GroupMgrSample, error) {
	var list []models.GroupMgrSample
	err := d.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// SampleAdd 新增样本（幂等：text 已存在则刷新分类/来源并返回其 ID）。
func (d *GroupMgrDAO) SampleAdd(ctx context.Context, text, category, source string) (uint, error) {
	return d.SampleAddPhrase(ctx, text, category, source, "black")
}

// SampleAddWithWord 新增样本并关联词条 ID（词条派生样本 source=seed 用；WordID=0 表示无关联）。
// 幂等：text 已存在则刷新分类/来源并回填 WordID（不覆盖已有关联）。
func (d *GroupMgrDAO) SampleAddWithWord(ctx context.Context, text, category, source string, wordID uint) (uint, error) {
	return d.SampleAddPhraseWithWord(ctx, text, category, source, "black", wordID)
}

// SampleAddPhrase 新增语录（黑/白名单），幂等：text 已存在则刷新分类/来源。
func (d *GroupMgrDAO) SampleAddPhrase(ctx context.Context, text, category, source, listType string) (uint, error) {
	return d.SampleAddPhraseWithWord(ctx, text, category, source, listType, 0)
}

// SampleAddPhraseWithWord 新增语录并可选关联词条 ID。幂等：text 已存在则刷新
// 分类/来源并回填 WordID（不覆盖已有关联）；listType 以首次入库为准（黑白不混用）。
func (d *GroupMgrDAO) SampleAddPhraseWithWord(ctx context.Context, text, category, source, listType string, wordID uint) (uint, error) {
	var existing models.GroupMgrSample
	err := d.db.WithContext(ctx).Where("text = ?", text).First(&existing).Error
	if err == nil {
		if existing.Category != category || existing.Source != source || (wordID > 0 && existing.WordID != wordID) {
			existing.Category = category
			existing.Source = source
			if wordID > 0 {
				existing.WordID = wordID
			}
			if uerr := d.db.WithContext(ctx).Save(&existing).Error; uerr != nil {
				return 0, uerr
			}
		}
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	s := models.GroupMgrSample{WordID: wordID, ListType: listType, Text: text, Category: category, Source: source}
	if err := d.db.WithContext(ctx).Create(&s).Error; err != nil {
		return 0, err
	}
	return s.ID, nil
}

// SampleListByList 按语录集合列出（black/white；Web 违禁语录列表用）。
func (d *GroupMgrDAO) SampleListByList(ctx context.Context, listType string) ([]models.GroupMgrSample, error) {
	var list []models.GroupMgrSample
	err := d.db.WithContext(ctx).Where("list_type = ?", listType).Order("id ASC").Find(&list).Error
	return list, err
}

// SampleTouch 更新样本最近命中时间（黑=处罚 / 白=放行；GC 判定未使用记录用）。
func (d *GroupMgrDAO) SampleTouch(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Model(&models.GroupMgrSample{}).Where("id = ?", id).
		UpdateColumn("last_used_at", time.Now()).Error
}

// SampleListUnused 列出最近窗口内未被命中的语录（GC 用），按最近命中时间升序取 limit 条。
func (d *GroupMgrDAO) SampleListUnused(ctx context.Context, listType string, since time.Time, limit int) ([]models.GroupMgrSample, error) {
	var list []models.GroupMgrSample
	err := d.db.WithContext(ctx).Where("list_type = ? AND (last_used_at IS NULL OR last_used_at < ?)",
		listType, since).Order("last_used_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

// SampleCountByList 语录集合数量（自学习上限控制用）。
func (d *GroupMgrDAO) SampleCountByList(ctx context.Context, listType string) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&models.GroupMgrSample{}).
		Where("list_type = ?", listType).Count(&n).Error
	return n, err
}

// SampleListByText 按文本列出样本（词条删除时对账清理用，通常 1 条）。
func (d *GroupMgrDAO) SampleListByText(ctx context.Context, text string) ([]models.GroupMgrSample, error) {
	var list []models.GroupMgrSample
	err := d.db.WithContext(ctx).Where("text = ?", text).Find(&list).Error
	return list, err
}

// SampleListByWord 按词条 ID 列出派生样本（对账/面板展示用）。
func (d *GroupMgrDAO) SampleListByWord(ctx context.Context, wordID uint) ([]models.GroupMgrSample, error) {
	var list []models.GroupMgrSample
	err := d.db.WithContext(ctx).Where("word_id = ?", wordID).Find(&list).Error
	return list, err
}

// SampleDelete 删除样本。
func (d *GroupMgrDAO) SampleDelete(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&models.GroupMgrSample{}, id).Error
}

// SampleIncrHit 命中计数 +1。
func (d *GroupMgrDAO) SampleIncrHit(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Model(&models.GroupMgrSample{}).Where("id = ?", id).
		UpdateColumn("hit_count", gorm.Expr("hit_count + 1")).Error
}

// SampleMarkRAGSynced 标记样本 RAG 同步状态（同步/删除失败时置 false，面板展示可信）。
func (d *GroupMgrDAO) SampleMarkRAGSynced(ctx context.Context, id uint, synced bool) error {
	return d.db.WithContext(ctx).Model(&models.GroupMgrSample{}).Where("id = ?", id).
		UpdateColumn("rag_synced", synced).Error
}

// ---------- 违规记录 ----------

// ViolationIncr 原子自增违规计数并返回新值（单条 UPSERT ... RETURNING，无 read-modify-write 竞争）。
// 事件循环（关键词直罚）与 Run 循环（LLM 追罚）双 goroutine 并发时不会丢计数/双重处罚。
// PG 用 now()、SQLite 用 CURRENT_TIMESTAMP（两者均支持 RETURNING，SQLite ≥ 3.35）。
func (d *GroupMgrDAO) ViolationIncr(ctx context.Context, groupID, userID int64, meta ViolationMeta) (int, error) {
	nowExpr := "now()"
	if d.db.Dialector.Name() != "postgres" {
		nowExpr = "CURRENT_TIMESTAMP"
	}
	var count int
	sql := fmt.Sprintf(`
		INSERT INTO group_mgr_violations (group_id, user_id, count, username, detection_path, llm_reason, updated_at)
		VALUES (?, ?, 1, ?, ?, ?, %s)
		ON CONFLICT (group_id, user_id)
		DO UPDATE SET count = group_mgr_violations.count + 1,
			username = EXCLUDED.username,
			detection_path = EXCLUDED.detection_path,
			llm_reason = EXCLUDED.llm_reason,
			updated_at = %s
		RETURNING count`, nowExpr, nowExpr)
	err := d.db.WithContext(ctx).Raw(sql, groupID, userID, meta.Username, meta.DetectionPath, meta.LLMReason).Scan(&count).Error
	return count, err
}

// ViolationGet 读取某群某用户的违规次数（无记录返回 0, nil）。
func (d *GroupMgrDAO) ViolationGet(ctx context.Context, groupID, userID int64) (int, error) {
	var v models.GroupMgrViolation
	err := d.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).First(&v).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return v.Count, nil
}

// ViolationMeta 违规现场信息（处罚时记录，面板展示用）。
type ViolationMeta struct {
	Username      string // 群名片/昵称
	DetectionPath string // rag / keyword / llm
	LLMReason     string // LLM 审核返回的 reason
}

// ViolationSet 写入违规记录（count <= 0 视为删除）。
// meta 为本次处罚的现场信息：用户名 / 判定来源 / LLM reason（逐次覆盖）。
func (d *GroupMgrDAO) ViolationSet(ctx context.Context, groupID, userID int64, count int, meta ViolationMeta) error {
	if count <= 0 {
		return d.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).
			Delete(&models.GroupMgrViolation{}).Error
	}
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "group_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"count", "username", "detection_path", "llm_reason", "updated_at",
		}),
	}).Create(&models.GroupMgrViolation{
		GroupID: groupID, UserID: userID, Count: count,
		Username: meta.Username, DetectionPath: meta.DetectionPath, LLMReason: meta.LLMReason,
	}).Error
}

// ViolationList 列出全部违规记录。
func (d *GroupMgrDAO) ViolationList(ctx context.Context) ([]models.GroupMgrViolation, error) {
	var list []models.GroupMgrViolation
	err := d.db.WithContext(ctx).Order("updated_at DESC").Find(&list).Error
	return list, err
}

// ViolationDelete 删除单条违规记录（面板"删除某行即重置该用户违规"）。
func (d *GroupMgrDAO) ViolationDelete(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&models.GroupMgrViolation{}, id).Error
}

// ViolationClearUser 清除指定群内某 QQ 号的违规记录，返回清除条数。
// （/豁免 按群清除，避免跨群清空其他群的惩罚阶梯）
func (d *GroupMgrDAO) ViolationClearUser(ctx context.Context, groupID, userID int64) (int, error) {
	res := d.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&models.GroupMgrViolation{})
	return int(res.RowsAffected), res.Error
}

// ViolationClearUserAll 清除某 QQ 号全部群的违规记录，返回清除条数。
// （白名单等全局豁免语义使用；/豁免 请用按群版本的 ViolationClearUser）
func (d *GroupMgrDAO) ViolationClearUserAll(ctx context.Context, userID int64) (int, error) {
	res := d.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&models.GroupMgrViolation{})
	return int(res.RowsAffected), res.Error
}

// ---------- 白名单 / 手动管理员 ----------

// WlList 白名单列表。
func (d *GroupMgrDAO) WlList(ctx context.Context) ([]models.GroupMgrWhitelist, error) {
	var list []models.GroupMgrWhitelist
	err := d.db.WithContext(ctx).Order("qq ASC").Find(&list).Error
	return list, err
}

// WlAdd 加入白名单（幂等）。
func (d *GroupMgrDAO) WlAdd(ctx context.Context, qq int64) error {
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&models.GroupMgrWhitelist{QQ: qq}).Error
}

// WlDelete 移出白名单。
func (d *GroupMgrDAO) WlDelete(ctx context.Context, qq int64) error {
	return d.db.WithContext(ctx).Where("qq = ?", qq).Delete(&models.GroupMgrWhitelist{}).Error
}

// AdminList 手动管理员列表。
func (d *GroupMgrDAO) AdminList(ctx context.Context) ([]models.GroupMgrAdmin, error) {
	var list []models.GroupMgrAdmin
	err := d.db.WithContext(ctx).Order("qq ASC").Find(&list).Error
	return list, err
}

// AdminAdd 加入手动管理员（幂等）。
func (d *GroupMgrDAO) AdminAdd(ctx context.Context, qq int64) error {
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&models.GroupMgrAdmin{QQ: qq}).Error
}

// AdminDelete 移除手动管理员。
func (d *GroupMgrDAO) AdminDelete(ctx context.Context, qq int64) error {
	return d.db.WithContext(ctx).Where("qq = ?", qq).Delete(&models.GroupMgrAdmin{}).Error
}

// ---------- 统计 / 状态 kv ----------

// StatGet 读取 kv（无则返回空串）。
func (d *GroupMgrDAO) StatGet(ctx context.Context, key string) (string, error) {
	var s models.GroupMgrStat
	err := d.db.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return s.Value, nil
}

// StatSet 写 kv（幂等 upsert）。
func (d *GroupMgrDAO) StatSet(ctx context.Context, key, value string) error {
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&models.GroupMgrStat{Key: key, Value: value}).Error
}

// StatIncr 数值 +1（数据库级原子自增，消除 read-modify-write 竞争；非数值态按 1 起算）。
// PG：UPDATE 内 CASE 判断数值态后自增，RETURNING 返回新值；
// 其他方言（SQLite 测试环境）：本地读改写回退（并发低，可接受）。
func (d *GroupMgrDAO) StatIncr(ctx context.Context, key string) (int64, error) {
	if d.db.Dialector.Name() == "postgres" {
		var v string
		err := d.db.WithContext(ctx).Raw(`
			INSERT INTO group_mgr_stats (key, value)
			VALUES (?, '1')
			ON CONFLICT (key) DO UPDATE
			SET value = CASE
				WHEN group_mgr_stats.value ~ '^[0-9]+$' THEN (group_mgr_stats.value::bigint + 1)::text
				ELSE '1'
			END
			RETURNING value`, key).Scan(&v).Error
		if err != nil {
			return 0, err
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		return n, nil
	}
	v, err := d.StatGet(ctx, key)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	n++
	return n, d.StatSet(ctx, key, strconv.FormatInt(n, 10))
}

// StatDelete 删除 kv（图片刷屏窗口清理等用，防无限增长）。
func (d *GroupMgrDAO) StatDelete(ctx context.Context, key string) error {
	return d.db.WithContext(ctx).Where("key = ?", key).Delete(&models.GroupMgrStat{}).Error
}

// StatListPrefix 列出前缀匹配的 kv（统计页聚合展示用）。
func (d *GroupMgrDAO) StatListPrefix(ctx context.Context, prefix string) ([]models.GroupMgrStat, error) {
	var list []models.GroupMgrStat
	err := d.db.WithContext(ctx).Where("key LIKE ?", prefix+"%").Order("key ASC").Find(&list).Error
	return list, err
}
