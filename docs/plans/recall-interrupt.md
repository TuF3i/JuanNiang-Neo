# 功能计划：用户撤回消息时中断 ReAct 循环并标记记忆

> 状态：**待实施**（调研与设计已完成，未开发）
> 创建：2026-09-16
> 预估工作量：0.5~1 天
> 前置调研：两轮代码探索已完成，本文档含全部关键结论，可直接照此开发，无需重新调研

## 一、需求

用户发出某条消息后中途撤回：

1. 消息还在批处理窗口排队 → 从批次剔除，Agent 完全不触发（此阶段记忆尚未写入，无需标记）
2. ReAct 循环正在处理该消息 → **中断循环**，丢弃生成到一半的回复与工具待发队列，不发送任何内容
3. 循环已结束的晚到撤回 → 仅做记忆标记
4. 短期记忆与 Web 聊天记录中把该消息标记为「已撤回」，让 LLM 与面板都能感知用户收回了这句话

## 二、调研结论（关键机制）

| # | 事实 | 出处 |
|---|---|---|
| 1 | OneBot11 撤回事件（`group_recall`/`friend_recall`）payload 自带 `message_id`，但 `NoticeEvent` 结构体缺该字段；`parseEvent` 的 `json.Unmarshal(raw, &n)` 会自动填充新加的 json 字段，**加一个字段即零成本解析** | `internal/adapter/models.go` NoticeEvent；`internal/adapter/server.go` parseEvent notice 分支 |
| 2 | notice 事件目前是死路：GroupMgr 只认 `group_increase`，Lua 插件不消费则在 processEvent 末尾被丢弃，无旁路吞掉撤回 | `internal/agent/event.go` processEvent 末尾 `if ev.PostType != "message" { return }` |
| 3 | eino adk：`NewRunner` 的第一个 ctx 参数被忽略，只有 `Run()` 的 ctx 生效；**ctx 取消时 `iter.Next()` 会先送达带 Err 的事件再 ok=false**（adk/cancel_test.go 有官方语义测试） | eino v0.9.13 adk/turn_loop.go、flow.go |
| 4 | **取消后 finish 闭包仍会执行**（它不是 defer，是 iter 循环 break 后的显式调用）。若不短路：`assistantContent` 非空会 `sendReply` 发出半截内容；`deferredSends.Flush` 不检查 ctx，会把取消前工具排队的消息真发出去 | `internal/agent/event.go` handleMessage 内 finish 闭包 |
| 5 | `agentCtx, agentCancel := context.WithTimeout(ctx, agentRunTimeout)`（5min）在 handleMessage 内创建；`agentCancel` 提前调用幂等，与超时共存无冲突。一个循环对应一组（同用户）消息，应把组内全部 MessageID 映射到同一个 cancel | `internal/agent/event.go` |
| 6 | 短期记忆是 Redis List（key `shortterm:msgs:<areaID>`，每条 JSON 带 `MsgID` 十进制串），**没有单条改写原语**，唯一全量改写途径是 `Overwrite`（Del+RPUSH，与并发 Add 有丢写竞态）→ 需要 Lua 原子脚本 | `internal/agent/memory/shortterm/shortterm.go`；`internal/core/cache/cache.go` 的 RPushIfMsgIDAbsent 脚本可作参考 |
| 7 | `chat_records` 表没有 message_id 列；`Session.AppendRecord` 签名里也没有。补齐成本低：模型加列（AutoMigrate 自动迁移）+ 写入点带 ID + DAO 标记方法 | `internal/core/models/chatRecord.go`；`internal/agent/session/session.go` AppendRecord；写入点 `event.go`（用户消息 recordChat 处拿得到 MessageID） |
| 8 | Compact 会把窗口内消息压成长期记忆摘要，**摘要无 message_id** → 已被 Compact 的消息无法定位标记（已知局限，可接受：摘要本身已弱化原文） | `shortterm.go` buildCompactContent / Compact |
| 9 | 群管处罚性撤回（`punish.go` 调 `DeleteMsg`）与 `delete_msg` builtin 工具已存在，与本功能无冲突 | `internal/agent/groupmgr/punish.go`、`internal/agent/tool/builtin.go` |

## 三、设计方案

### 3.1 撤回事件入口

- `internal/adapter/models.go`：`NoticeEvent` 加 `MessageID int64 \`json:"message_id"\``（1 行）。
- `processEvent` 在 Phase 0 去重之后尽早加分支：`notice_type ∈ {group_recall, friend_recall}` → `h.handleRecall(ctx, ev)`。

### 3.2 handleRecall 的处理顺序

1. **定位 ChatArea**：group_recall → `AreaTypeGroup + GroupID`；friend_recall → `AreaTypePrivate + UserID`，走 `DAO.ChatArea.GetOrCreate`（与 `getChatArea` 同映射；area 几乎必然已存在）。
2. **批窗口期剔除**：`batchMu` 下从 `h.batches[areaID].events` 过滤掉该 message_id 的事件；批空则停 timer 并删批次。→ 结束（记忆未写，无需标记）。
3. **中断循环**：查 `recallCancels` 注册表，命中则 `cancel()`。
4. **记忆标记**（无论 2/3 是否命中，覆盖晚到撤回）：短期记忆 Lua 标记 + 聊天记录标记。

### 3.3 循环注册与中断

- HagoCenter 新增：

```go
recallMu      sync.Mutex
recallCancels map[string]context.CancelFunc // message_id 十进制串 → agentCancel
```

- `handleMessage`：把 agentCtx 的创建上移到 `Loops.Register` 同一区块，并用**专用撤回 cause** 区分取消来源（`context.Canceled` 无法区分用户撤回与父级取消/服务关停）：

```go
timeoutCtx, timeoutCancel := context.WithTimeoutCause(ctx, agentRunTimeout, ErrAgentRunTimeout)
defer timeoutCancel()
agentCtx, agentCancel := context.WithCancelCause(timeoutCtx)
defer agentCancel(nil)
// 组内全部非 0 MessageID 注册 agentCancel；撤回处理中调用 agentCancel(ErrRecalled)
```

- 取消后 `iter.Next()` 送达 Err 事件 → 走现有 `event.Err` break 分支；收尾判定用 `context.Cause(agentCtx)`：
  - `errors.Is(cause, ErrRecalled)` → 用户撤回：outcome=`"cancelled"`，走撤回短路
  - `errors.Is(cause, ErrAgentRunTimeout)` → 5 分钟超时：行为保持现状（outcome=`"timeout"`）
  - `errors.Is(cause, context.Canceled)` → 父级取消/服务关停：按现有 error 收尾，**不**标 cancelled、不触发撤回语义

### 3.4 finish 短路（核心安全点）

finish 闭包开头：

```go
if errors.Is(context.Cause(agentCtx), ErrRecalled) {
    // 仅用户撤回触发：丢弃半截回复与工具待发队列，用户侧记录已在派发前写入，直接收尾
    log.Info("消息已撤回，中断回复", ...)
    return
}
```

跳过：WaitReview 闸门、`deferredSends.Flush`、`sendReply`、assistant 侧 recordChat 与短期记忆写入。
超时（`ErrAgentRunTimeout`）与父级取消（`context.Canceled`）路径行为不变。

### 3.5 短期记忆标记

- `internal/core/cache/cache.go` 新增 Lua 原子脚本 `MarkMsgRecalled(key, msgID, marker)`：LRANGE 找 `decoded.msg_id` 匹配项 → LSET 把 content 前缀加 marker；无匹配返回 0（参考同文件 RPushIfMsgIDAbsent 的脚本写法）。注意：字段名与短期记忆 `ChatMessage.MsgID` 的 json 序列化键 `msg_id` 严格一致；`msgID` 入参必须是 `strconv.FormatInt(message_id)` 的**十进制字符串**，与存储值做字符串等值比对（OneBot message_id 可能为负数，勿做数值转换或正负归一）。
- `shortterm.go` 新增 `MarkRecalledByMsgID(ctx, areaID, msgID, marker)`；`memory/memory.go` 加转发 `MarkShortTermMessageRecalled`。
- marker 文案：`【该消息已被发送者撤回】` + 原内容（原文保留，LLM 能理解语境与用户意图变化）。

### 3.6 聊天记录标记

- `models.ChatRecord` 加 `MessageID int64 \`gorm:"index"\``（AutoMigrate 自动加列，存量数据该列为 0 不受影响）。
- `Session.AppendRecord` 签名加 messageID 参数，event.go 全部调用点带上。
- `chatRecordDao` 新增 `MarkRecalledByMessageID(ctx, chatAreaID, messageID)`：`role='user'` 的记录 content 追加 `[已撤回]`。

## 四、范围界定（V1 不做）

- **不撤回 bot 已发出的回复**：中断只能覆盖「批窗口排队」与「循环运行中」两个阶段；若回复已发出，撤回无效。如后续需要，需记录「触发消息 ID → bot 回复 ID」映射并调 `DeleteMsg`，作为 V2。
- 不做 Compact 后长期记忆摘要的撤回标记（摘要无 message_id，且摘要本身已弱化原文）。
- 多人群批中撤回只精确中断该用户组，其他组已读取的「背景消息」上下文不回溯修改。

## 五、测试计划

- **单测**（纯逻辑，无外部依赖）：
  - recallCancels 注册表：注册/注销/取消幂等
  - 批窗口剔除：events 过滤、空批停 timer 清理
  - finish 短路判定：`context.Canceled` vs `DeadlineExceeded` 区分
- **Lua 标记脚本**：go.mod 无 miniredis，不引新依赖；以 `make dev`（本地 Redis）集成验证为主
- **集成验证路径**：三种时序——发消息立即撤回（批窗口期）/ 等 Agent 开始回复再撤回（运行期）/ 回复完再撤回（晚到），观察 outcome=cancelled、短期记忆 content 前缀、面板聊天记录 [已撤回] 标记
- 全量 `go build` + `go vet` + `go test ./...`

## 六、实施顺序与提交切分

| 步骤 | 内容 | 提交 |
|---|---|---|
| 1 | NoticeEvent.MessageID + processEvent 撤回分支 + handleRecall 骨架（批剔除 + 注册表） | `feat(agent): 撤回事件接入与批窗口剔除` |
| 2 | 循环注册表 + agentCtx 上移 + finish 短路 + outcome=cancelled | `feat(agent): 撤回中断 ReAct 循环` |
| 3 | cache Lua 脚本 + shortterm/memory 转发 | `feat(memory): 短期记忆撤回标记` |
| 4 | ChatRecord.MessageID + AppendRecord + DAO 标记 | `feat(api): 聊天记录撤回标记` |
| 5 | 单测 | `test(agent): 撤回中断单测` |

分支建议：基于 main 建 `feature/recall-interrupt`。

## 七、开放问题（实施前需拍板）

1. ~~bot 已发出的回复是否连带撤回~~ → V1 不做（如需 V2 再立项）
2. 撤回标记的文案是否需要可配置（面板项）→ V1 硬编码，如有需要后续加 config
3. friend_recall 在部分 OneBot 实现（如 go-cqhttp）支持度不一 → 失败静默降级即可，联调时确认 NapCat 行为
