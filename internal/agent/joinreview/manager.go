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
	"errors"
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

// RequestExecutor 执行加群请求相关外部调用的抽象（adapter.Adapter 实现之），便于测试替身。
type RequestExecutor interface {
	HandleGroupRequest(flag, subType string, approve bool, reason string) error
	GetStrangerInfo(userID int64) (*adapter.StrangerInfo, error)
}

// ErrRequestBusy 请求正在审核中或已被处理（人工与 AI 抢占失败）。
var ErrRequestBusy = errors.New("该请求正在审核中或已被处理")

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
	m.pruneDisabledBuffers(cfg)
	log.Info("加群审核配置已重载", "enabled_groups", len(cfg.EnabledGroups), "batch_size", cfg.BatchSize, "flush_seconds", cfg.FlushSeconds)
	return nil
}

// pruneDisabledBuffers 配置变更后清理已移出生效群的攒批缓冲并停掉其窗口定时器，
// 避免已移出的群继续被 AI 送审；缓冲中的请求保持 pending 落库状态，面板可人工处理。
func (m *Manager) pruneDisabledBuffers(cfg *models.GroupJoinReviewConfig) {
	enabled := map[int64]bool{}
	for _, g := range cfg.EnabledGroups {
		enabled[g] = true
	}
	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	for gid := range m.buf {
		if enabled[gid] {
			continue
		}
		delete(m.buf, gid)
		if t := m.timers[gid]; t != nil {
			t.Stop()
			delete(m.timers, gid)
		}
	}
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
// 返回是否成功接管：落库失败返回 false，调用方应放行事件继续走插件兜底，避免吞掉请求。
func (m *Manager) Enqueue(ctx context.Context, ev adapter.Event) bool {
	req := ev.Request
	if req == nil || req.GroupID <= 0 || req.UserID <= 0 {
		return false
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
		return false
	}
	// 请求事件不含昵称：后台异步经 get_stranger_info 补采（不阻塞事件循环，失败保持空）
	go m.fetchUsername(ctx, rec)

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
	return true
}

// fetchUsername 异步补采申请人昵称（get_stranger_info），成功后回填 DB 行与缓冲内存对象。
// 失败静默：面板回退展示 QQ 号，不阻塞审核流程。
func (m *Manager) fetchUsername(ctx context.Context, rec *models.GroupJoinRequest) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("fetchUsername panic", "id", rec.ID, "recover", r)
		}
	}()
	// 脱离事件处理 ctx 的取消信号（否则 goroutine 刚起 ctx 就被取消）
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	info, err := m.adp.GetStrangerInfo(rec.UserID)
	if err != nil || info == nil || strings.TrimSpace(info.Nickname) == "" {
		log.Debug("申请人昵称补采失败，保持空", "user", rec.UserID, "err", err)
		return
	}
	name := strings.TrimSpace(info.Nickname)
	if err := m.dao.RequestUpdateUsername(cctx, rec.ID, name); err != nil {
		log.Warn("申请人昵称回写失败", "id", rec.ID, "err", err)
		return
	}
	m.setBufferedUsername(rec.GroupID, rec.ID, name)
}

// setBufferedUsername 回填缓冲中同一请求的昵称（若该请求仍在攒批缓冲内）。
func (m *Manager) setBufferedUsername(gid int64, id uint, name string) {
	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	for _, it := range m.buf[gid] {
		if it.ID == id {
			it.Username = name
			return
		}
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

	// 逐条原子抢占（pending → processing）：已被人工抢先处理的请求跳过
	claimed := make([]*models.GroupJoinRequest, 0, len(items))
	for _, rec := range items {
		ok, err := m.dao.ClaimRequest(ctx, rec.ID)
		if err != nil {
			log.Warn("加群请求抢占失败，留待人工处理", "id", rec.ID, "err", err)
			continue
		}
		if !ok {
			log.Info("加群请求已被人工抢占，跳过 AI 审核", "id", rec.ID)
			continue
		}
		claimed = append(claimed, rec)
	}
	if len(claimed) == 0 {
		return
	}

	cfg := m.getCfg(ctx)
	// 提示词注入防护：留言含标记伪造特征的申请不送 AI（避免扰动批量判定结构），转人工处理
	var aiItems, manualItems []*models.GroupJoinRequest
	for _, rec := range claimed {
		if containsPromptInjection(rec.Comment) {
			manualItems = append(manualItems, rec)
			continue
		}
		aiItems = append(aiItems, rec)
	}
	if len(manualItems) > 0 {
		log.Warn("加群申请疑似提示词注入，转人工处理", "group", gid, "count", len(manualItems))
		for _, rec := range manualItems {
			_ = m.dao.ReleaseRequest(ctx, rec.ID)
		}
	}
	if len(aiItems) == 0 {
		return
	}

	results, err := m.reviewBatch(ctx, cfg, gid, aiItems)
	if err != nil {
		// 整批失败：释放回 pending，请求仍出现在待审列表供人工处理
		log.Warn("加群审核 LLM 整批失败，释放回待审", "group", gid, "count", len(aiItems), "err", err)
		for _, rec := range aiItems {
			_ = m.dao.ReleaseRequest(ctx, rec.ID)
		}
		return
	}
	for i, rec := range aiItems {
		res, ok := results[i]
		if !ok {
			// 该条无有效裁决（索引越界/非法 verdict/缺失）：释放回待审
			log.Warn("加群审核裁决缺失，释放回待审", "group", gid, "user", rec.UserID)
			_ = m.dao.ReleaseRequest(ctx, rec.ID)
			continue
		}
		log.Info("加群审核 AI 裁决", "group", gid, "user", rec.UserID, "verdict", res.Verdict, "reason", res.Reason)
		if err := m.applyVerdict(ctx, rec, res.Verdict, "ai", res.Reason); err != nil {
			log.Warn("加群审核裁决执行失败，保留待审行", "group", gid, "user", rec.UserID, "err", err)
		}
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
// 先原子抢占（pending → processing），防止与 AI 攒批或并发人工操作重复执行；
// 仍在 AI 攒批窗口内的请求同步从缓冲摘除。返回值透传执行错误（OneBot 失败等），
// 供 API 层提示管理员（终态记录与待审行状态由 applyVerdict 内部保证）。
func (m *Manager) ManualDecide(ctx context.Context, id uint, approve bool, reason string) error {
	ok, err := m.dao.ClaimRequest(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRequestBusy
	}
	rec, err := m.dao.RequestGet(ctx, id)
	if err != nil {
		_ = m.dao.ReleaseRequest(ctx, id)
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
	verdict := "reject"
	if approve {
		verdict = "approve"
	}
	return m.applyVerdict(ctx, rec, verdict, "manual", reason)
}

// applyVerdict 单条审核终态：approve/reject 调 OneBot 执行后按结果处理待审行；
// manual（AI 建议转人工）不调 OneBot，释放回 pending 留在待审列表由人工处理。
// 三种裁决均落审核记录。返回 OneBot 执行错误（供人工审核路径透传给面板提示）。
func (m *Manager) applyVerdict(ctx context.Context, rec *models.GroupJoinRequest, verdict, reviewer, reason string) error {
	var execErr error
	if verdict == "approve" || verdict == "reject" {
		execErr = m.adp.HandleGroupRequest(rec.Flag, rec.SubType, verdict == "approve", reason)
		if execErr != nil {
			reason = fmt.Sprintf("【执行失败，请人工在 QQ 中处理】%s（错误：%v）", reason, execErr)
			log.Warn("加群审核动作执行失败", "group", rec.GroupID, "user", rec.UserID, "verdict", verdict, "err", execErr)
		}
	}
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
		// 审核记录未落库：释放回 pending，避免面板丢失该请求（执行可能已成功，人工可复核）
		log.Error("加群审核记录落库失败，请求释放回待审", "group", rec.GroupID, "user", rec.UserID, "err", err)
		_ = m.dao.ReleaseRequest(ctx, rec.ID)
		return fmt.Errorf("审核记录落库失败: %w", err)
	}
	switch {
	case verdict == "manual":
		// AI 建议转人工：释放回 pending，留在待审列表由人工处理
		if err := m.dao.ReleaseRequest(ctx, rec.ID); err != nil {
			log.Error("转人工请求释放失败", "id", rec.ID, "err", err)
		}
	case execErr == nil:
		if err := m.dao.RequestDelete(ctx, rec.ID); err != nil {
			log.Error("待审请求删除失败", "id", rec.ID, "err", err)
		}
	default:
		if err := m.dao.ReleaseRequest(ctx, rec.ID); err != nil {
			log.Error("待审请求释放失败", "id", rec.ID, "err", err)
		}
	}
	return execErr
}

// groupPromptKey 每群提示词在配置 map 中的键（群号十进制字符串）。
func groupPromptKey(groupID int64) string { return strconv.FormatInt(groupID, 10) }
