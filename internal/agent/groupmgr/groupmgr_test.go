package groupmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/core"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/ragtag"
	"JuanNiang-Neo/internal/metrics"

	caller "JuanNiang-Neo/infrastructure/rag/handler"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestManager 构造测试 Manager：sqlite 内存库 + 可选 mock RAG server（score 可定制）。
// mock 命中黑名单语录（ragtag.Sample("1")）。
func newTestManager(t *testing.T, ragScore *float64) (*Manager, *dao.GroupMgrDAO) {
	return newTestManagerEx(t, ragScore)
}

func newTestManagerEx(t *testing.T, ragScore *float64) (*Manager, *dao.GroupMgrDAO) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// :memory: 库每连接独立：固定单连接（含空闲池），保证并发测试共享同一库
	if sqlDB, derr := db.DB(); derr == nil {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	}
	if err := core.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	gmdao := dao.NewBundle(db).GroupMgr
	if err := gmdao.InitConfig(context.Background()); err != nil {
		t.Fatalf("init config: %v", err)
	}

	var ragCli *caller.Client
	if ragScore != nil {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/scoops/groupmgr/tags/search" {
				http.NotFound(w, r)
				return
			}
			// 返回一个命中：tag 与下方插入的语录行（首个自增 ID=1）对齐
			_ = json.NewEncoder(w).Encode(caller.SearchResponse{Results: []caller.SearchHit{
				{Tag: ragtag.Sample("1"), Score: *ragScore},
			}})
		}))
		t.Cleanup(srv.Close)
		ragCli = &caller.Client{
			Config:     caller.Config{BaseURL: srv.URL, Timeout: 5 * time.Second},
			HttpClient: &http.Client{Timeout: 5 * time.Second},
		}
	}

	m := New(gmdao, adapter.New(adapter.Config{}), func() *caller.Client { return ragCli }, provider.NewProviderGroup())
	// 语录候选集：插入一条黑名单语录（ID=1，与 mock 命中 tag 对齐）
	if _, err := gmdao.SampleAdd(context.Background(), "办卡加群办套餐", "ad", "seed"); err != nil {
		t.Fatalf("seed sample: %v", err)
	}
	// Init：默认配置 + 种子词库导入 + 内存缓存加载
	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	return m, gmdao
}

func TestViolationRAGUnavailableKeywordPath(t *testing.T) {
	m, _ := newTestManager(t, nil) // 无 RAG
	ctx := context.Background()

	// 黑词 → 高危复核（review）
	rep := m.TestViolation(ctx, "校园卡办卡免沸！低价流量卡，加裙114514")
	if rep.Verdict != "review" || rep.Word == "" {
		t.Fatalf("黑词应走 review，got verdict=%s word=%q", rep.Verdict, rep.Word)
	}
	// 无词 → 放行
	rep = m.TestViolation(ctx, "今天食堂的饭真好吃")
	if rep.Verdict != "pass" {
		t.Fatalf("无词应 pass，got %s", rep.Verdict)
	}
}

func TestViolationRAGHighScorePunish(t *testing.T) {
	score := 0.92
	m, _ := newTestManager(t, &score)
	rep := m.TestViolation(context.Background(), "0元购送福利，加我微信领流量卡")
	if !rep.RAGOK {
		t.Fatal("RAG 应可用")
	}
	if rep.Verdict != "punish" {
		t.Fatalf("黑名单高置信应 punish，got %s (%s)", rep.Verdict, rep.Reason)
	}
	if rep.BlackScore < 0.9 {
		t.Fatalf("black_score 应上报最高分，got %f", rep.BlackScore)
	}
}

func TestViolationRAGMidScoreReview(t *testing.T) {
	score := 0.6
	m, _ := newTestManager(t, &score)
	rep := m.TestViolation(context.Background(), "低价流量卡办理")
	if !rep.RAGOK {
		t.Fatal("RAG 应可用")
	}
	if rep.Verdict != "review" {
		t.Fatalf("未达黑名单阈值应 review（LLM 判定），got %s (%s)", rep.Verdict, rep.Reason)
	}
}

func TestViolationRAGLowScoreReview(t *testing.T) {
	score := 0.3
	m, _ := newTestManager(t, &score)
	rep := m.TestViolation(context.Background(), "明天要交作业了吗")
	if rep.Verdict != "review" {
		t.Fatalf("低分未命中黑名单 → LLM 判定，got %s (%s)", rep.Verdict, rep.Reason)
	}
}

// TestBuildPhraseSetSkipsLegacyWhite 回归：白名单语录体系剔除后，存量 list_type='white'
// 行必须被候选集跳过——不得进入黑名单集合（防止历史白语录被语义命中造成误罚），
// 也不得缓存空集（否则正常黑名单语录同步后无法重建候选集）。
func TestBuildPhraseSetSkipsLegacyWhite(t *testing.T) {
	m, gmdao := newTestManagerEx(t, nil)
	ctx := context.Background()
	// 追加一条存量白名单语录（ID=2），模拟剔除前的历史数据
	if _, err := gmdao.SampleAddPhrase(ctx, "明天一起食堂吃饭吗", "ok", "seed", "white"); err != nil {
		t.Fatalf("seed legacy white phrase: %v", err)
	}
	set := m.buildPhraseSet(ctx)
	if set == nil {
		t.Fatal("候选集构建失败")
	}
	if len(set.black) != 1 {
		t.Fatalf("候选集应只含 1 条黑名单语录，got %d", len(set.black))
	}
	for tag := range set.black {
		if tag == ragtag.Sample("2") {
			t.Fatal("白名单语录不应进入候选集")
		}
	}
	if _, ok := set.black[ragtag.Sample("1")]; !ok {
		t.Fatal("黑名单语录应在候选集中")
	}
}

// 候选集外 tag（存量白语录 wt: 向量 / 外来数据）的检索命中忽略机制，
// 由 TestRAGForeignTagNotMatched 覆盖（同一代码路径：owned.black 查不到即忽略）。

// TestViolationDoesNotObserveMetrics 回归：链路测试（TestViolation）不得观测生产指标
// （RAGSearchLatency / GroupMgrRAGScore / RAGSearchErrorsTotal），否则面板反复粘贴文本
// 诊断会把测试流量混入生产分布——分数分布面板（调阈值依据）最易被带偏。
func TestViolationDoesNotObserveMetrics(t *testing.T) {
	score := 0.92 // mock RAG 命中：覆盖分数/延迟观测路径
	m, _ := newTestManager(t, &score)

	scoreBefore := histogramSamples(t, metrics.GroupMgrRAGScore)
	latencyBefore := histogramSamples(t, metrics.RAGSearchLatency)
	errBefore := testutil.ToFloat64(metrics.RAGSearchErrorsTotal)

	rep := m.TestViolation(context.Background(), "0元购送福利，加我微信领流量卡")
	if !rep.RAGOK || rep.Verdict != "punish" {
		t.Fatalf("预置应走 RAG 命中路径，got ok=%v verdict=%s", rep.RAGOK, rep.Verdict)
	}
	if scoreAfter := histogramSamples(t, metrics.GroupMgrRAGScore); scoreAfter != scoreBefore {
		t.Fatalf("链路测试不应观测 RAG 分数，before=%d after=%d", scoreBefore, scoreAfter)
	}
	if latencyAfter := histogramSamples(t, metrics.RAGSearchLatency); latencyAfter != latencyBefore {
		t.Fatalf("链路测试不应观测 RAG 检索延迟，before=%d after=%d", latencyBefore, latencyAfter)
	}
	if errAfter := testutil.ToFloat64(metrics.RAGSearchErrorsTotal); errAfter != errBefore {
		t.Fatalf("链路测试不应观测 RAG 检索错误，before=%v after=%v", errBefore, errAfter)
	}
}

// histogramSamples 读取直方图 sample_count（testutil.ToFloat64 不支持直方图，会 panic）。
func histogramSamples(t *testing.T, h prometheus.Histogram) uint64 {
	t.Helper()
	ch := make(chan prometheus.Metric, 4)
	h.Collect(ch)
	m := <-ch // 直方图首个 metric 为 sample_count
	dtoM := &dto.Metric{}
	if err := m.Write(dtoM); err != nil {
		t.Fatal(err)
	}
	return dtoM.GetHistogram().GetSampleCount()
}

// TestViolationIncrConcurrent 违规计数原子自增：并发 N 次自增最终计数 = N。
// 回归：punish 曾 ViolationGet → count++ → ViolationSet 非原子，事件循环与
// Run 循环双 goroutine 竞争会丢计数（双重处罚/错档惩罚）。
func TestViolationIncrConcurrent(t *testing.T) {
	_, gmdao := newTestManager(t, nil)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = gmdao.ViolationIncr(ctx, 100, 200, dao.ViolationMeta{Username: "并发", DetectionPath: "keyword", LLMReason: "r"})
		}()
	}
	wg.Wait()

	c, err := gmdao.ViolationGet(ctx, 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	if c != n {
		t.Fatalf("并发自增后计数应为 %d，实际 %d（read-modify-write 丢计数）", n, c)
	}
	// 现场信息为最后一次处罚覆盖
	list, _ := gmdao.ViolationList(ctx)
	if len(list) != 1 {
		t.Fatalf("违规记录应 1 条，got %d", len(list))
	}
	if list[0].Count != n {
		t.Fatalf("DB 内 count 应为 %d，got %d", n, list[0].Count)
	}
}

// TestPunishTiersConcurrent 双 goroutine 并发处罚：同一用户两条违规消息同时处理，
// 最终 count 精确为 2（ViolationIncr 单条 UPSERT 原子自增不丢级）；-race 下同时验证无数据竞争。
func TestPunishTiersConcurrent(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()
	ev := groupEv(100, 200, "广告")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.punish(ctx, ev, "广告违规：并发路径1", "ad", "keyword")
	}()
	go func() {
		defer wg.Done()
		m.punish(ctx, ev, "广告违规：并发路径2", "ad", "llm")
	}()
	wg.Wait()

	c, err := gmdao.ViolationGet(ctx, 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	if c != 2 {
		t.Fatalf("并发处罚后 count 应为 2，实际 %d（read-modify-write 丢级）", c)
	}
}

func TestDAOFixtures(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()

	// 词条幂等：重复 upsert 不增加计数（词条表已废弃，仅验证 DAO 旧行为）
	before, _ := gmdao.WordCount(ctx)
	if _, err := gmdao.WordUpsert(ctx, "校园卡", "gray", "import"); err != nil {
		t.Fatal(err)
	}
	if _, err := gmdao.WordUpsert(ctx, "校园卡", "gray", "import"); err != nil {
		t.Fatal(err)
	}
	after, _ := gmdao.WordCount(ctx)
	if after != before && after != before+1 {
		t.Fatalf("词条幂等失败，before=%d after=%d", before, after)
	}
	// 违规记录
	if err := gmdao.ViolationSet(ctx, 100, 200, 1, dao.ViolationMeta{}); err != nil {
		t.Fatal(err)
	}
	c, _ := gmdao.ViolationGet(ctx, 100, 200)
	if c != 1 {
		t.Fatalf("违规次数 = %d", c)
	}
	if err := gmdao.ViolationSet(ctx, 100, 200, 0, dao.ViolationMeta{}); err != nil {
		t.Fatal(err)
	}
	if c, _ = gmdao.ViolationGet(ctx, 100, 200); c != 0 {
		t.Fatalf("清零后违规次数 = %d", c)
	}
	// 统计
	if _, err := gmdao.StatIncr(ctx, "100:stats:warn"); err != nil {
		t.Fatal(err)
	}
	if v, _ := gmdao.StatGet(ctx, "100:stats:warn"); v != "1" {
		t.Fatalf("统计值 = %q", v)
	}
	// 词库热更新后命中（词库从 go:embed txt 加载到内存，不入 DB；含办校园卡等黑词）
	_ = m.Reload(ctx)
	hit, cat := m.wordHit(ctx, "帮我办校园卡")
	if hit == "" || cat == "" {
		t.Fatalf("词命中 = %q/%s", hit, cat)
	}
	_ = fmt.Sprint()
}
