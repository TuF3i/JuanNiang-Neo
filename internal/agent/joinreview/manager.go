// Package joinreview 加群请求 AI 攒批审核。
//
// 事件循环在 Phase 0.6 把命中生效群的加群请求（request_type=group, sub_type=add）
// 交给本模块：先落库（待审列表），再进入按群隔离的攒批缓冲——同群攒满 BatchSize
// 条立即送审，否则首条入队起算 FlushSeconds 秒后整批送审。LLM 按每群独立提示词
// 一批逐条判定，裁决经 set_group_add_request 执行（通过/拒绝），终态统一落
// GroupJoinReview（AI 与人工共用）；面板人工审核可随时兜底，执行失败（flag 过期等）
// 的请求保留待审行供人工重试。
//
// 攒批按群隔离（不跨群混批）：每群提示词独立，混批无法应用群特定口径。
package joinreview

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/logging"
)

var log = logging.NewModule("joinreview")

// 配置内存缓存 TTL：Web 面板保存后调用 Reload 立即失效，TTL 仅兜底。
const cfgCacheTTL = 30 * time.Second

const (
	defaultBatchSize   = 5  // 攒批阈值默认值
	defaultFlushSecond = 60 // 触发窗口默认值（秒）
)

// RequestExecutor 执行加群请求裁决的抽象（adapter.Adapter 实现之），便于测试替身。
type RequestExecutor interface {
	HandleGroupRequest(flag, subType string, approve bool, reason string) error
}

// Manager 加群审核。
type Manager struct {
	dao       *dao.JoinReviewDAO
	adp       RequestExecutor
	providers *provider.ProviderGroup

	// 攒批缓冲（按群隔离）+ 触发窗口定时器
	bufMu  sync.Mutex
	buf    map[int64][]*models.GroupJoinRequest // group_id → 窗口内待审请求
	timers map[int64]*time.Timer                // group_id → 首条入队开启的窗口定时器

	// 配置缓存（Reload 立即失效，TTL 兜底）
	cfgMu sync.RWMutex
	cfg   *models.GroupJoinReviewConfig
	cfgAt time.Time
}

// New 创建加群审核 Manager。
func New(d *dao.JoinReviewDAO, adp RequestExecutor, pg *provider.ProviderGroup) *Manager {
	return &Manager{
		dao:       d,
		adp:       adp,
		providers: pg,
		buf:       map[int64][]*models.GroupJoinRequest{},
		timers:    map[int64]*time.Timer{},
	}
}

// Init 初始化：建默认配置行 + 载入缓存。
func (m *Manager) Init(ctx context.Context) error {
	if err := m.dao.InitConfig(ctx); err != nil {
		return err
	}
	return m.Reload(ctx)
}

// Reload 重载配置（Web 面板保存后调用；TTL 兜底）。
func (m *Manager) Reload(ctx context.Context) error {
	cfg, err := m.dao.GetConfig(ctx)
	if err != nil {
		return err
	}
	m.cfgMu.Lock()
	m.cfg = cfg
	m.cfgAt = time.Now()
	m.cfgMu.Unlock()
	log.Info("加群审核配置已重载", "enabled_groups", len(cfg.EnabledGroups), "batch_size", cfg.BatchSize, "flush_seconds", cfg.FlushSeconds)
	return nil
}

// getCfg 读取配置缓存（TTL 内直接返回；过期重载，失败回退缓存/默认）。
func (m *Manager) getCfg(ctx context.Context) *models.GroupJoinReviewConfig {
	m.cfgMu.RLock()
	cfg, at := m.cfg, m.cfgAt
	m.cfgMu.RUnlock()
	if cfg != nil && time.Since(at) < cfgCacheTTL {
		return cfg
	}
	if err := m.Reload(ctx); err != nil {
		log.Warn("加群审核配置读取失败，使用缓存/默认", "err", err)
		m.cfgMu.RLock()
		defer m.cfgMu.RUnlock()
		if m.cfg != nil {
			return m.cfg
		}
		return &models.GroupJoinReviewConfig{BatchSize: defaultBatchSize, FlushSeconds: defaultFlushSecond}
	}
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.cfg
}

// batchParams 攒批参数（含默认值兜底）。
func batchParams(cfg *models.GroupJoinReviewConfig) (batchSize, flushSeconds int) {
	batchSize, flushSeconds = defaultBatchSize, defaultFlushSecond
	if cfg != nil {
		if cfg.BatchSize > 0 {
			batchSize = cfg.BatchSize
		}
		if cfg.FlushSeconds > 0 {
			flushSeconds = cfg.FlushSeconds
		}
	}
	return
}

// Interested 该群是否配置为 AI 审核生效群（事件循环快速判断，未命中不接管）。
func (m *Manager) Interested(ctx context.Context, groupID int64) bool {
	cfg := m.getCfg(ctx)
	if cfg == nil {
		return false
	}
	for _, g := range cfg.EnabledGroups {
		if g == groupID {
			return true
		}
	}
	return false
}

// Enqueue 加群请求进入审核流：落库（待审列表可见）+ 入按群攒批缓冲。
// 本群首条入队开启触发窗口定时器；攒满阈值立即异步送审（定时器到点后发现缓冲已空则空转）。
func (m *Manager) Enqueue(ctx context.Context, ev adapter.Event) {
	req := ev.Request
	if req == nil || req.GroupID <= 0 || req.UserID <= 0 {
		return
	}
	rec := &models.GroupJoinRequest{
		GroupID: req.GroupID,
		UserID:  req.UserID,
		Comment: req.Comment,
		Flag:    req.Flag,
		SubType: req.SubType,
	}
	if err := m.dao.RequestCreate(ctx, rec); err != nil {
		// 落库失败不进缓冲：请求不出现在待审列表会造成"AI 审了但面板无记录"的裂痕
		log.Error("加群请求落库失败，跳过审核", "group", req.GroupID, "user", req.UserID, "err", err)
		return
	}

	cfg := m.getCfg(ctx)
	batchSize, flushSeconds := batchParams(cfg)

	m.bufMu.Lock()
	buf := append(m.buf[req.GroupID], rec)
	m.buf[req.GroupID] = buf
	first := len(buf) == 1
	full := len(buf) >= batchSize
	if first {
		gid := req.GroupID
		m.timers[gid] = time.AfterFunc(time.Duration(flushSeconds)*time.Second, func() {
			m.flushGroup(ctx, gid)
		})
	}
	m.bufMu.Unlock()

	log.Info("加群请求已入审核缓冲", "group", req.GroupID, "user", req.UserID, "buffered", len(m.bufSnapshot(req.GroupID)), "batch_size", batchSize)
	if full {
		go m.flushGroup(ctx, req.GroupID)
	}
}

// bufSnapshot 缓冲快照长度（日志用）。
func (m *Manager) bufSnapshot(gid int64) []*models.GroupJoinRequest {
	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	return m.buf[gid]
}

// flushGroup 取走该群整批缓冲送 LLM 审核（AfterFunc / 满批 go 触发，异步不阻塞事件循环）。
// LLM 整批失败时请求保持 pending（仍在待审列表），由人工兜底，不自动重试。
func (m *Manager) flushGroup(ctx context.Context, gid int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("flushGroup panic", "group", gid, "recover", r)
		}
	}()
	// 定时器/满批触发时事件处理早已返回，脱离其取消信号（保留 trace 值）
	ctx = context.WithoutCancel(ctx)

	m.bufMu.Lock()
	items := m.buf[gid]
	delete(m.buf, gid)
	if t := m.timers[gid]; t != nil {
		t.Stop()
		delete(m.timers, gid)
	}
	m.bufMu.Unlock()
	if len(items) == 0 {
		return // 满批提前触发后，旧窗口定时器到点空转
	}

	cfg := m.getCfg(ctx)
	results, err := m.reviewBatch(ctx, cfg, gid, items)
	if err != nil {
		log.Warn("加群审核 LLM 整批失败，留待人工处理", "group", gid, "count", len(items), "err", err)
		return
	}
	for i, rec := range items {
		res, ok := results[i]
		if !ok {
			// 该条无有效裁决（索引越界/非法 verdict/缺失）：留待人工
			log.Warn("加群审核裁决缺失，留待人工处理", "group", gid, "user", rec.UserID)
			continue
		}
		approve := res.Verdict == "approve"
		log.Info("加群审核 AI 裁决", "group", gid, "user", rec.UserID, "verdict", res.Verdict, "reason", res.Reason)
		m.applyVerdict(ctx, rec, approve, "ai", res.Reason)
	}
}

// removeBuf 从攒批缓冲摘除指定请求（人工审核抢先处理时调用，防 AI 稍后重复审）。
// 缓冲清空时停掉该群窗口定时器。
func (m *Manager) removeBuf(gid int64, id uint) {
	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	buf := m.buf[gid]
	kept := buf[:0]
	for _, it := range buf {
		if it.ID != id {
			kept = append(kept, it)
		}
	}
	if len(kept) == 0 {
		delete(m.buf, gid)
		if t := m.timers[gid]; t != nil {
			t.Stop()
			delete(m.timers, gid)
		}
		return
	}
	m.buf[gid] = kept
}

// ManualDecide 人工审核（Web 面板）：执行裁决并落终态记录。
// 若请求仍在 AI 攒批窗口内，先从缓冲摘除，防止 AI 稍后重复审核同一请求。
func (m *Manager) ManualDecide(ctx context.Context, id uint, approve bool, reason string) error {
	rec, err := m.dao.RequestGet(ctx, id)
	if err != nil {
		return err
	}
	m.removeBuf(rec.GroupID, rec.ID)
	if strings.TrimSpace(reason) == "" {
		if approve {
			reason = "管理员手动通过"
		} else {
			reason = "管理员手动拒绝"
		}
	}
	m.applyVerdict(ctx, rec, approve, "manual", reason)
	return nil
}

// applyVerdict 单条审核终态：调 OneBot 执行（通过/拒绝）→ 落审核记录 → 执行成功才删待审行。
// 执行失败（flag 过期/平台错误等）保留待审行，理由附加失败说明，人工可在 QQ 或面板重试。
func (m *Manager) applyVerdict(ctx context.Context, rec *models.GroupJoinRequest, approve bool, reviewer, reason string) {
	verdict := "reject"
	if approve {
		verdict = "approve"
	}
	execErr := m.adp.HandleGroupRequest(rec.Flag, rec.SubType, approve, reason)
	if execErr != nil {
		reason = fmt.Sprintf("【执行失败，请人工在 QQ 中处理】%s（错误：%v）", reason, execErr)
		log.Warn("加群审核动作执行失败", "group", rec.GroupID, "user", rec.UserID, "verdict", verdict, "err", execErr)
	}
	if err := m.dao.ReviewCreate(ctx, &models.GroupJoinReview{
		RequestID:  rec.ID,
		GroupID:    rec.GroupID,
		UserID:     rec.UserID,
		Username:   rec.Username,
		Comment:    rec.Comment,
		Verdict:    verdict,
		Reviewer:   reviewer,
		Reason:     reason,
		ReviewedAt: time.Now(),
	}); err != nil {
		log.Error("加群审核记录落库失败", "group", rec.GroupID, "user", rec.UserID, "err", err)
	}
	if execErr == nil {
		if err := m.dao.RequestDelete(ctx, rec.ID); err != nil {
			log.Error("待审请求删除失败", "id", rec.ID, "err", err)
		}
	}
}

// groupPromptKey 每群提示词在配置 map 中的键（群号十进制字符串）。
func groupPromptKey(groupID int64) string { return strconv.FormatInt(groupID, 10) }
