package groupmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/core"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/ragtag"

	caller "JuanNiang-Neo/infrastructure/rag/handler"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestManagerForeignHit 构造 Manager：RAG 服务可用，但检索命中的 tag 不属于
// 本系统语录（模拟脏数据/残留向量：DB 已删但向量未双删的孤儿命中）。
// 回归：RAG 服务正常但无语录命中时，应视为"可用但无命中"（送 LLM 判定），
// 而不是降级为"RAG 不可用"（关键词兜底放行）。
// （scoop 化后外来集合 tag 不再可能出现在 groupmgr 检索结果中；
// 该用例保留 mock 知识 tag 仅作"DB 外孤儿 tag"的代理。）
func newTestManagerForeignHit(t *testing.T, score float64) *Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := core.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	gmdao := dao.NewBundle(db).GroupMgr
	if err := gmdao.InitConfig(context.Background()); err != nil {
		t.Fatalf("init config: %v", err)
	}

	// mock RAG：返回知识库 tag（k: 前缀），不属于语录候选集
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scoops/groupmgr/tags/search" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(caller.SearchResponse{Results: []caller.SearchHit{
			{Tag: ragtag.Knowledge(uuid.New().String()), Score: score},
		}})
	}))
	t.Cleanup(srv.Close)
	ragCli := &caller.Client{
		Config:     caller.Config{BaseURL: srv.URL, Timeout: 5 * time.Second},
		HttpClient: &http.Client{Timeout: 5 * time.Second},
	}

	m := New(gmdao, adapter.New(adapter.Config{}), func() *caller.Client { return ragCli }, provider.NewProviderGroup())
	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	return m
}

// TestViolationRAGAvailableNoPhraseHit RAG 服务正常但无语录命中 → 走 LLM 判定（review），
// 不是降级为"RAG 不可用"放行（回归：verifyByRAG 曾把无命中误判为不可用）。
func TestViolationRAGAvailableNoPhraseHit(t *testing.T) {
	m := newTestManagerForeignHit(t, 0.95) // 高置信命中，但属于知识向量
	rep := m.TestViolation(context.Background(), "低价流量卡办理")
	if !rep.RAGOK {
		t.Fatalf("RAG 服务可用应上报 rag_ok=true（无语录命中≠不可用），got false: %s", rep.Reason)
	}
	if rep.Verdict != "review" {
		t.Fatalf("无语录命中应送 LLM 判定（review），got %s (%s)", rep.Verdict, rep.Reason)
	}
	if rep.BlackScore != 0 {
		t.Fatalf("外来 tag 命中不应计入黑名单分数，black=%f", rep.BlackScore)
	}
}

// TestVerifyRAGForeignHitIsAvailable verifyByRAG 对"外来 tag 命中"应 ok=true 且无命中条目。
func TestVerifyRAGForeignHitIsAvailable(t *testing.T) {
	m := newTestManagerForeignHit(t, 0.9)
	v := m.verifyByRAG(context.Background(), "测试文本", false)
	if !v.ok {
		t.Fatal("RAG 服务可用（外来 tag 命中）应 ok=true")
	}
	if v.black != nil {
		t.Fatalf("外来 tag 不应产生黑名单命中，black=%v", v.black)
	}
}
