package groupmgr

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// cqCodeRegexp 匹配 CQ 码: [CQ:type,key=value,...]（剥离富文本 payload 用）。
var cqCodeRe = regexp.MustCompile(`\[CQ:[a-zA-Z_]+(?:,[^\]]+)?\]`)

// recordJoin 入群统计（notice group_increase）。
func (m *Manager) recordJoin(ctx context.Context, groupID int64) {
	date := time.Now().Format("2006-01-02")
	_, err := m.dao.StatIncr(ctx, gkey(groupID, "stats:join:"+date))
	if err != nil {
		log.Warn("入群统计失败", "group", groupID, "err", err)
	}
}

// phraseHit 黑名单语录命中计数（RAG 处罚时 +1 并更新最近命中时间）。
// 候选集 phraseInfo 已携带 ID，直接按 ID 更新，不再 O(n) 文本反查。
func (m *Manager) phraseHit(ctx context.Context, tag uuid.UUID) {
	m.sampleMu.Lock()
	info, ok := m.sampleSet.black[tag]
	m.sampleMu.Unlock()
	if !ok || info.id == 0 {
		return
	}
	_ = m.dao.SampleIncrHit(ctx, info.id)
	_ = m.dao.SampleTouch(ctx, info.id)
}

// Stats 统计摘要（/groupstats 命令与 Web 面板共用）。
type Stats struct {
	GroupID   int64  `json:"group_id"`
	Date      string `json:"date"`
	JoinToday int64  `json:"join_today"`
	Warns     int64  `json:"warns"`
	Mutes     int64  `json:"mutes"`
	CopyWarns int64  `json:"copy_warns"`
	Ad        int64  `json:"ad"`
	Sensitive int64  `json:"sensitive"`
	Kicks     int64  `json:"kicks"`
}

// GroupStats 统计某群数据（/groupstats 与 Web 统计页）。
func (m *Manager) GroupStats(ctx context.Context, groupID int64) (*Stats, error) {
	date := time.Now().Format("2006-01-02")
	get := func(suffix string) int64 {
		v, err := m.dao.StatGet(ctx, gkey(groupID, suffix))
		if err != nil {
			return 0
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	st := &Stats{
		GroupID:   groupID,
		Date:      date,
		JoinToday: get("stats:join:" + date),
		Warns:     get("stats:warn"),
		Mutes:     get("stats:mute"),
		CopyWarns: get("stats:copy_warn"),
		Ad:        get("stats:ad") + get("stats:card"),
		Sensitive: get("stats:sensitive"),
		Kicks:     get("stats:kick"),
	}
	return st, nil
}

// StatsText 组装 /groupstats 回复文本（与旧插件格式一致）。
func (m *Manager) StatsText(ctx context.Context, groupID int64) string {
	st, err := m.GroupStats(ctx, groupID)
	if err != nil {
		return "统计数据读取失败，稍后再试哦～"
	}
	return fmt.Sprintf("📊 群管理统计 (%s)\n────────────────\n今日入群: %d 人\n刷屏警告: %d 次\n刷屏禁言: %d 次\n复读警告: %d 次\n广告违规: %d 次\n敏感违规: %d 次\n踢出群聊: %d 次",
		st.Date, st.JoinToday, st.Warns, st.Mutes, st.CopyWarns, st.Ad, st.Sensitive, st.Kicks)
}
