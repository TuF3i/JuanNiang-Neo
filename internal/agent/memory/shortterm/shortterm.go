package shortterm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/core/cache"
	"JuanNiang-Neo/internal/logging"
)

var log = logging.NewModule("shortterm")

// ChatMessage 聊天消息模型。
// MsgID 是消息唯一标识（OneBot11 message_id，仅用户消息携带），
// 用于幂等去重：同一消息重复写入短期记忆时跳过，防止并发/重试路径重复消费。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
	MsgID   string `json:"msg_id,omitempty"`
}

// Config 短期记忆配置。
type Config struct {
	WindowSize  int64
	AutoCompact bool
}

// ShortTermMemory 基于 Redis 的短期记忆，使用 List 实现滑动窗口。
// 所有方法按 areaID 隔离，实例本身无状态，可跨 ChatArea 共享。
type ShortTermMemory struct {
	conf       Config
	cache      *cache.Cache
	compacting sync.Map // areaID → *sync.Mutex (per-area compact lock)
}

func New(conf Config, c *cache.Cache) *ShortTermMemory {
	if conf.WindowSize <= 0 {
		conf.WindowSize = 100
	}
	return &ShortTermMemory{conf: conf, cache: c}
}

// compactLock 返回指定 areaID 的 Compact 互斥锁（不存在则创建）。
func (m *ShortTermMemory) compactLock(areaID string) *sync.Mutex {
	v, _ := m.compacting.LoadOrStore(areaID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *ShortTermMemory) WindowSize() int64     { return m.conf.WindowSize }
func (m *ShortTermMemory) SetWindowSize(n int64) { m.conf.WindowSize = n }
func (m *ShortTermMemory) AutoCompact() bool     { return m.conf.AutoCompact }
func (m *ShortTermMemory) SetAutoCompact(v bool) { m.conf.AutoCompact = v }

func (m *ShortTermMemory) key(areaID string) string {
	return "shortterm:msgs:" + areaID
}

// Add 追加一条消息并维护滑动窗口（保留最近 WindowSize 条，最早在前）。
func (m *ShortTermMemory) Add(ctx context.Context, areaID string, msg ChatMessage) error {
	return m.AddWithWindow(ctx, areaID, msg, m.conf.WindowSize)
}

// AddWithWindow 追加一条消息并按指定的窗口大小维护滑动窗口（最早在前）。
// Per-ChatArea 配置解析后由调用方传入该 area 的窗口大小，避免全局实例的共享配置互相覆盖。
// 幂等：携带 MsgID 的消息若已存在于窗口内则跳过（防止同一消息被重复消费写入）。
//
// 原子性：检查 MsgID + RPUSH + LTRIM 三步合并为单个 Lua 脚本（cache.RPushIfMsgIDAbsent），
// Redis 单线程执行保证原子。原 LRange+containsMsgID+RPush+LTrim 四步分离实现存在
// TOCTOU 竞态：两个并发 goroutine 同时 LRange 都未命中 → 同时 RPush → 短期记忆里出现
// 两条相同 MsgID 的用户消息，下游 LLM 看到重复上下文导致连锁重复回复。
func (m *ShortTermMemory) AddWithWindow(ctx context.Context, areaID string, msg ChatMessage, windowSize int64) error {
	if m.cache == nil {
		return fmt.Errorf("shortterm cache 未初始化")
	}
	if windowSize <= 0 {
		windowSize = m.conf.WindowSize
	}
	// 序列化方式须与原 cache.RPush 内部 json.Marshal 一致，保证脚本写入的
	// 字节流与历史数据格式相同（GetAll 反序列化时不会出错）。
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("序列化失败", "area_id", areaID, "role", msg.Role, "msg_id", msg.MsgID, "err", err)
		return err
	}
	written, err := m.cache.RPushIfMsgIDAbsent(ctx, m.key(areaID), string(data), msg.MsgID, windowSize)
	if err != nil {
		log.Warn("Lua 脚本执行失败",
			"area_id", areaID, "role", msg.Role, "msg_id", msg.MsgID, "window_size", windowSize, "err", err)
		return err
	}
	if !written {
		// MsgID 已存在被脚本跳过——并发去重命中的关键事件。
		// 出现这条日志说明：上游 redisDedup 也漏了（Redis 故障 / TTL 已过期 / 多实例独立内存），
		// Layer 2 兜底生效，避免了短期记忆里出现两条相同 MsgID 的用户消息。
		log.Info("Lua 幂等命中跳过",
			"area_id", areaID,
			"role", msg.Role,
			"msg_id", msg.MsgID,
			"window_size", windowSize,
		)
		return nil
	}
	log.Debug("短期记忆写入成功",
		"area_id", areaID,
		"role", msg.Role,
		"msg_id", msg.MsgID,
		"window_size", windowSize,
	)
	return nil
}

// GetAll 返回该 ChatArea 当前窗口内的全部消息（按时间最早→最新）。
func (m *ShortTermMemory) GetAll(ctx context.Context, areaID string) ([]ChatMessage, error) {
	if m.cache == nil {
		return nil, fmt.Errorf("shortterm cache 未初始化")
	}
	var msgs []ChatMessage
	if err := m.cache.LRange(ctx, m.key(areaID), 0, -1, &msgs); err != nil {
		return nil, fmt.Errorf("shortterm getall: %w", err)
	}
	return msgs, nil
}

// Overwrite 用新列表覆盖窗口（先清空后追加）。
func (m *ShortTermMemory) Overwrite(ctx context.Context, areaID string, msgs []ChatMessage) error {
	return m.OverwriteWithWindow(ctx, areaID, msgs, m.conf.WindowSize)
}

// OverwriteWithWindow 用新列表覆盖窗口，并按指定的窗口大小截断。
// Per-ChatArea 配置解析后由调用方传入该 area 的窗口大小。
func (m *ShortTermMemory) OverwriteWithWindow(ctx context.Context, areaID string, msgs []ChatMessage, windowSize int64) error {
	if m.cache == nil {
		return fmt.Errorf("shortterm cache 未初始化")
	}
	if windowSize <= 0 {
		windowSize = m.conf.WindowSize
	}
	key := m.key(areaID)
	if err := m.cache.Del(ctx, key); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	args := make([]any, len(msgs))
	for i, mm := range msgs {
		args[i] = mm
	}
	if err := m.cache.RPush(ctx, key, args...); err != nil {
		return err
	}
	return m.cache.LTrim(ctx, key, -windowSize, -1)
}

// Clear 清空窗口。
func (m *ShortTermMemory) Clear(ctx context.Context, areaID string) error {
	if m.cache == nil {
		return fmt.Errorf("shortterm cache 未初始化")
	}
	return m.cache.Del(ctx, m.key(areaID))
}

// Compact 压缩短期记忆: 调用 LLM 摘要后写入长期记忆，并清理短期记忆窗口。
// 同一 ChatArea 已有 Compact 在运行时返回 ErrCompactInProgress（并发防护，非致命错误）。
func (m *ShortTermMemory) Compact(ctx context.Context, areaID string, llm provider.Provider, store CompactStore) error {
	mu := m.compactLock(areaID)
	if !mu.TryLock() {
		return fmt.Errorf("%w %s", ErrCompactInProgress, areaID)
	}
	defer mu.Unlock()

	msgs, err := m.GetAll(ctx, areaID)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	content := buildCompactContent(msgs)

	// 获取当前长期记忆供 LLM 参考
	var existingMemory string
	if existing, err := store.GetExistingLongTermMemory(ctx, areaID); err == nil && len(existing) > 0 {
		existingMemory = "已有的长期记忆摘要:\n" + strings.Join(existing, "\n") + "\n\n"
	}

	prompt := existingMemory + "以下是最近的对话记录:\n" + content

	systemPrompt := `你是一个对话记忆管理助手。请根据对话记录生成一份简洁的长期记忆摘要。

要求：
1. 提取对话中的关键信息：用户偏好、重要事实、待办事项、反复提及的话题
2. 保留对后续对话有价值的上下文信息
3. 用简洁的中文描述，不要超过200字
4. 如果已有长期记忆，在其基础上更新（加入新信息，去除已过时的内容）
5. 不要包含无意义的寒暄和重复内容
6. 直接输出摘要内容，不要加任何前缀或解释`

	resp, err := llm.Chat(ctx, provider.ChatRequest{
		Messages: []provider.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		log.Error("Compact LLM 调用失败", "err", err)
		return err
	}

	summary := resp.Message.Content
	if err := store.AddLongTermMemory(ctx, areaID, summary); err != nil {
		return fmt.Errorf("compact 写入长期记忆失败: %w", err)
	}

	// 同时触发技能记忆更新
	if sm, ok := store.(SkillMemoryUpdater); ok {
		if err := sm.UpdateSkillMemory(ctx, msgs); err != nil {
			log.Warn("Compact 技能记忆更新失败", "err", err)
		}
	}

	// 清理短期记忆：只保留最近 compactKeepRecent 条原始消息（其余已压缩进长期记忆）。
	// 若不清理，窗口持续满载，每条新消息都会再次触发 Compact（频繁 "already in progress"）。
	if len(msgs) > compactKeepRecent {
		if err := m.Overwrite(ctx, areaID, msgs[len(msgs)-compactKeepRecent:]); err != nil {
			log.Warn("Compact 后清理短期记忆失败", "area_id", areaID, "err", err)
		} else {
			log.Info("Compact 已清理短期记忆，保留最近消息", "area_id", areaID, "kept", compactKeepRecent)
		}
	}

	log.Info("短期记忆 Compact 完成", "area_id", areaID, "summary_len", len(summary))
	return nil
}

// compactKeepRecent Compact 完成后短期记忆保留的最近消息条数，
// 保证上下文连贯性的同时避免窗口满载重复触发 Compact。
const compactKeepRecent = 10

// ErrCompactInProgress 同一 ChatArea 已有 Compact 在运行（并发防护触发）。
// 属于预期的竞争现象（窗口满时多条消息先后触发），调用方可据此降级日志级别。
var ErrCompactInProgress = errors.New("compact already in progress for area")

// CompactStore 是 Compact 时需要的存储接口。
type CompactStore interface {
	AddLongTermMemory(ctx context.Context, areaID string, content string) error
	GetExistingLongTermMemory(ctx context.Context, areaID string) ([]string, error)
}

// SkillMemoryUpdater 可选接口：CompactStore 若实现此接口，Compact 时会自动更新技能记忆。
type SkillMemoryUpdater interface {
	UpdateSkillMemory(ctx context.Context, recentMsgs []ChatMessage) error
}

// compactMsgidMarkerRe 匹配短期记忆中的 [msgid:N] 长消息标记。
// Compact 摘要前剥离该展示层内部标记，避免把其抄进长期记忆/技能记忆语料污染召回。
var compactMsgidMarkerRe = regexp.MustCompile(`\[msgid:\d+\]`)

func buildCompactContent(msgs []ChatMessage) string {
	var out string
	for _, m := range msgs {
		out += fmt.Sprintf("[%s]: %s\n", m.Role, compactMsgidMarkerRe.ReplaceAllString(m.Content, ""))
	}
	return out
}

func (m *ShortTermMemory) Export(ctx context.Context, areaID string) ([]provider.ChatMessage, error) {
	msgs, err := m.GetAll(ctx, areaID)
	if err != nil {
		return nil, err
	}
	out := make([]provider.ChatMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = provider.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}
	return out, nil
}

func (m *ShortTermMemory) ExportJSON(ctx context.Context, areaID string) ([]byte, error) {
	msgs, err := m.GetAll(ctx, areaID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(msgs)
}
