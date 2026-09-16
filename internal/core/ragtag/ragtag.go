// Package ragtag 派生 RAG-Service 的 tag（UUID v5 确定性映射）。
//
// 背景：RAG-Service 按分库（scoop）隔离各功能块的向量（检索只在目标 scoop
// 内进行，避免不同集合互相挤占 top-k）；tag 必须是 UUID，无法直接携带业务
// 前缀，因此用固定命名空间 + 前缀做 UUID v5 派生：
//   - 知识：v5(ragNS, "k:"+itemID) → scoop knowledge
//   - 记忆：v5(ragNS, "m:"+itemID) → scoop memory
//   - 群管理黑样本/词条：v5(ragNS, "s:"+itemID) → scoop groupmgr
//   - 群管理白语录：v5(ragNS, "wt:"+itemID) → scoop groupmgr
//
// 派生函数确定性、可复现：写入/检索/删除/手动同步都用同一函数，无需额外映射字段
// （表结构零改动）。同一 tag 只允许归属一个 scoop（服务端注册表强校验，409）。
package ragtag

import "github.com/google/uuid"

// Scoop 常量：对应 RAG-Service 的分库白名单（服务端枚举见 RAG-Service src/store.rs），
// 所有 RAG API 调用都必须带上归属分库。
const (
	ScoopKnowledge = "knowledge" // 知识库条目
	ScoopMemory    = "memory"    // 长期记忆条目
	ScoopGroupMgr  = "groupmgr"  // 群管理黑白语录/词条（黑白同库：一次检索取两边最优命中）
	ScoopPlugin    = "plugin"    // 插件 jn.rag 通用 API 默认库
)

// ragNS 固定命名空间（任意常量 UUID，保证派生结果跨实例稳定）。
var ragNS = uuid.MustParse("e8c5c747-3a2b-4c1f-9d5e-0a6b7c8d9e0f")

// Knowledge 返回知识条目的 RAG tag。
func Knowledge(itemID string) uuid.UUID {
	return uuid.NewSHA1(ragNS, []byte("k:"+itemID))
}

// Memory 返回长期记忆条目的 RAG tag。
func Memory(itemID string) uuid.UUID {
	return uuid.NewSHA1(ragNS, []byte("m:"+itemID))
}

// Word 返回群管理违规关键词条的 RAG tag（与 k:/m:/s: 前缀互不污染）。
func Word(itemID string) uuid.UUID {
	return uuid.NewSHA1(ragNS, []byte("w:"+itemID))
}

// Sample 返回群管理违规样本的 RAG tag（学习闭环/种子/导入样本）。
func Sample(itemID string) uuid.UUID {
	return uuid.NewSHA1(ragNS, []byte("s:"+itemID))
}
