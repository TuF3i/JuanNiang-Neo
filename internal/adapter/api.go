package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// 消息发送 — Agent 最常用的 API
// ============================================================

// SendPrivateMsg 发送私聊消息。message 可以是 string / Segment / []Segment。
// 返回发送成功后的消息 ID。
func (p *Adapter) SendPrivateMsg(userID int64, message any) (int64, error) {
	return p.sendMsg("private", userID, message)
}

// SendGroupMsg 发送群聊消息。
func (p *Adapter) SendGroupMsg(groupID int64, message any) (int64, error) {
	return p.sendMsg("group", groupID, message)
}

// sendMsg 通用消息发送。
func (p *Adapter) sendMsg(msgType string, id int64, message any) (int64, error) {
	params := map[string]any{
		"message_type": msgType,
		"message":      p.resolveImageAssets(normalizeMessage(message)),
	}
	switch msgType {
	case "private":
		params["user_id"] = id
	case "group":
		params["group_id"] = id
	}

	rsp, err := p.call("send_msg", params)
	if err != nil {
		return 0, err
	}

	var data struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(rsp.Data.(string)), &data); err != nil {
		return 0, fmt.Errorf("parse send_msg response: %w", err)
	}
	return data.MessageID, nil
}

// DeleteMsg 撤回消息。
func (p *Adapter) DeleteMsg(messageID int64) error {
	_, err := p.call("delete_msg", map[string]any{"message_id": messageID})
	return err
}

// MarkMsgRead 标记消息已读。
func (p *Adapter) MarkMsgRead(messageID int64) error {
	_, err := p.call("mark_msg_as_read", map[string]any{"message_id": messageID})
	return err
}

// GetMsg 获取消息内容。
func (p *Adapter) GetMsg(messageID int64) (*MessageEvent, error) {
	rsp, err := p.call("get_msg", map[string]any{"message_id": messageID})
	if err != nil {
		return nil, err
	}
	var msg MessageEvent
	if err := json.Unmarshal([]byte(rsp.Data.(string)), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// MsgHistoryList 消息历史响应体（get_group_msg_history / get_friend_msg_history 的 data）。
type MsgHistoryList struct {
	Messages []MessageEvent `json:"messages"`
}

// GetGroupMsgHistory 获取群消息历史。messageSeq>0 时返回该消息之前的历史（升序）；
// count>0 时提示实现端返回条数（OneBot11 标准未定义 count，部分实现忽略，
// 调用方需自行按需截断）。
func (p *Adapter) GetGroupMsgHistory(groupID, messageSeq int64, count int) ([]MessageEvent, error) {
	params := map[string]any{"group_id": groupID}
	if messageSeq > 0 {
		params["message_seq"] = messageSeq
	}
	if count > 0 {
		params["count"] = count
	}
	rsp, err := callAndParse[MsgHistoryList](p, "get_group_msg_history", params)
	if err != nil {
		return nil, err
	}
	return rsp.Messages, nil
}

// GetFriendMsgHistory 获取好友私聊消息历史。参数语义同 GetGroupMsgHistory。
func (p *Adapter) GetFriendMsgHistory(userID int64, count, messageSeq int64) ([]MessageEvent, error) {
	params := map[string]any{"user_id": userID}
	if count > 0 {
		params["message_count"] = count
	}
	if messageSeq > 0 {
		params["message_seq"] = messageSeq
	}
	rsp, err := callAndParse[MsgHistoryList](p, "get_friend_msg_history", params)
	if err != nil {
		return nil, err
	}
	return rsp.Messages, nil
}

// ============================================================
// 用户相关
// ============================================================

// GetLoginInfo 获取登录号信息。
func (p *Adapter) GetLoginInfo() (*LoginInfo, error) {
	return callAndParse[LoginInfo](p, "get_login_info", nil)
}

// GetStrangerInfo 获取陌生人信息。
func (p *Adapter) GetStrangerInfo(userID int64) (*StrangerInfo, error) {
	return callAndParse[StrangerInfo](p, "get_stranger_info", map[string]any{"user_id": userID})
}

// GetFriendList 获取好友列表。
func (p *Adapter) GetFriendList() ([]FriendInfo, error) {
	return callAndParseSlice[FriendInfo](p, "get_friend_list", nil)
}

// ============================================================
// 群信息
// ============================================================

// GetGroupInfo 获取群信息。
func (p *Adapter) GetGroupInfo(groupID int64) (*GroupInfo, error) {
	return callAndParse[GroupInfo](p, "get_group_info", map[string]any{"group_id": groupID})
}

// GetGroupList 获取群列表。
func (p *Adapter) GetGroupList() ([]GroupInfo, error) {
	return callAndParseSlice[GroupInfo](p, "get_group_list", nil)
}

// GetGroupMemberInfo 获取群成员信息。
func (p *Adapter) GetGroupMemberInfo(groupID, userID int64) (*GroupMemberInfo, error) {
	return callAndParse[GroupMemberInfo](p, "get_group_member_info", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
	})
}

// GetGroupMemberList 获取群成员列表。
func (p *Adapter) GetGroupMemberList(groupID int64) ([]GroupMemberInfo, error) {
	return callAndParseSlice[GroupMemberInfo](p, "get_group_member_list", map[string]any{"group_id": groupID})
}

// GetGroupHonorInfo 获取群荣誉信息。
func (p *Adapter) GetGroupHonorInfo(groupID int64) (*GroupHonorInfo, error) {
	return callAndParse[GroupHonorInfo](p, "get_group_honor_info", map[string]any{"group_id": groupID})
}

// ============================================================
// 群管理
// ============================================================

// KickGroupMember 踢出群成员。rejectAdd 为 true 时不再接收加群申请。
func (p *Adapter) KickGroupMember(groupID, userID int64, rejectAdd bool) error {
	_, err := p.call("set_group_kick", map[string]any{
		"group_id":           groupID,
		"user_id":            userID,
		"reject_add_request": rejectAdd,
	})
	return err
}

// BanGroupMember 禁言群成员。duration 秒数, 0 表示解除。
func (p *Adapter) BanGroupMember(groupID, userID int64, duration int) error {
	_, err := p.call("set_group_ban", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"duration": duration,
	})
	return err
}

// SetGroupWholeBan 全员禁言。
func (p *Adapter) SetGroupWholeBan(groupID int64, enable bool) error {
	_, err := p.call("set_group_whole_ban", map[string]any{
		"group_id": groupID,
		"enable":   enable,
	})
	return err
}

// SetGroupAdmin 设置/取消管理员。
func (p *Adapter) SetGroupAdmin(groupID, userID int64, enable bool) error {
	_, err := p.call("set_group_admin", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"enable":   enable,
	})
	return err
}

// SetGroupCard 设置群名片。
func (p *Adapter) SetGroupCard(groupID, userID int64, card string) error {
	_, err := p.call("set_group_card", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"card":     card,
	})
	return err
}

// SetGroupName 设置群名。
func (p *Adapter) SetGroupName(groupID int64, name string) error {
	_, err := p.call("set_group_name", map[string]any{
		"group_id":   groupID,
		"group_name": name,
	})
	return err
}

// LeaveGroup 退出群。
func (p *Adapter) LeaveGroup(groupID int64) error {
	_, err := p.call("set_group_leave", map[string]any{"group_id": groupID})
	return err
}

// SetGroupSpecialTitle 设置群专属头衔。
func (p *Adapter) SetGroupSpecialTitle(groupID, userID int64, title string) error {
	_, err := p.call("set_group_special_title", map[string]any{
		"group_id":      groupID,
		"user_id":       userID,
		"special_title": title,
	})
	return err
}

// ============================================================
// 请求处理
// ============================================================

// HandleFriendRequest 处理好友请求。flag 来自请求事件。
func (p *Adapter) HandleFriendRequest(flag string, approve bool, remark string) error {
	_, err := p.call("set_friend_add_request", map[string]any{
		"flag":    flag,
		"approve": approve,
		"remark":  remark,
	})
	return err
}

// HandleGroupRequest 处理群请求 (加群/邀请)。subType: add / invite。
func (p *Adapter) HandleGroupRequest(flag, subType string, approve bool, reason string) error {
	_, err := p.call("set_group_add_request", map[string]any{
		"flag":     flag,
		"sub_type": subType,
		"approve":  approve,
		"reason":   reason,
	})
	return err
}

// ============================================================
// 媒体
// ============================================================

// GetImage 获取图片信息。
func (p *Adapter) GetImage(file string) (*FileInfo, error) {
	return callAndParse[FileInfo](p, "get_image", map[string]any{"file": file})
}

// GetRecord 获取语音信息。
func (p *Adapter) GetRecord(file string) (*FileInfo, error) {
	return callAndParse[FileInfo](p, "get_record", map[string]any{"file": file})
}

// CanSendImage 检查是否可以发送图片。
func (p *Adapter) CanSendImage() (bool, error) {
	return callAndCheckYes(p, "can_send_image")
}

// CanSendRecord 检查是否可以发送语音。
func (p *Adapter) CanSendRecord() (bool, error) {
	return callAndCheckYes(p, "can_send_record")
}

// ============================================================
// 扩展 API
// ============================================================

// SendLike 发送赞。
func (p *Adapter) SendLike(userID int64, times int) error {
	if times <= 0 {
		times = 1
	}
	_, err := p.call("send_like", map[string]any{
		"user_id": userID,
		"times":   times,
	})
	return err
}

// GetCookies 获取 cookies。
func (p *Adapter) GetCookies() (*Cookies, error) {
	return callAndParse[Cookies](p, "get_cookies", nil)
}

// GetCSRFToken 获取 CSRF token。
func (p *Adapter) GetCSRFToken() (*CSRF, error) {
	return callAndParse[CSRF](p, "get_csrf_token", nil)
}

// GetCredentials 获取 cookies + CSRF token。
func (p *Adapter) GetCredentials() (*Credentials, error) {
	return callAndParse[Credentials](p, "get_credentials", nil)
}

// GetStatus 获取运行状态。
func (p *Adapter) GetStatus() (*Status, error) {
	return callAndParse[Status](p, "get_status", nil)
}

// GetVersionInfo 获取版本信息。
func (p *Adapter) GetVersionInfo() (*VersionInfo, error) {
	return callAndParse[VersionInfo](p, "get_version_info", nil)
}

// ============================================================
// 转发消息 (合并转发)
// ============================================================

// GetForwardMsg 获取合并转发消息内容。
func (p *Adapter) GetForwardMsg(messageID string) ([]ForwardNode, error) {
	return callAndParseSlice[ForwardNode](p, "get_forward_msg", map[string]any{"message_id": messageID})
}

// SendGroupForwardMsg 发送合并转发消息到群。nodes 支持两种节点（LLBot/OneBot11 扩展标准）：
//   - 构造节点（Uin/Name/Content）：序列化为 {type:"node", data:{user_id, nickname, content}}，
//     content 支持 string（含 CQ 码自动解析）或 []Segment；
//   - 引用节点（仅 ID）：序列化为 {type:"node", data:{id}}，引用群内已存在的消息作为转发节点。
func (p *Adapter) SendGroupForwardMsg(groupID int64, nodes []ForwardNode) (int64, error) {
	messages := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		data := make(map[string]any, 3)
		if n.ID != 0 {
			// 引用节点：直接引用已发送消息
			data["id"] = n.ID
		} else {
			// 构造节点
			data["user_id"] = n.Uin
			data["nickname"] = n.Name
			data["content"] = p.resolveImageAssets(normalizeMessage(n.Content))
		}
		messages = append(messages, map[string]any{"type": "node", "data": data})
	}
	rsp, err := p.call("send_group_forward_msg", map[string]any{
		"group_id": groupID,
		"messages": messages,
	})
	if err != nil {
		return 0, err
	}
	var data MessageData
	json.Unmarshal([]byte(rsp.Data.(string)), &data)
	return data.MessageID, nil
}

// ============================================================
// 内部辅助
// ============================================================

// normalizeMessage 将 Agent 传入的消息转为 OneBot11 message 字段格式。
// string → string; Segment/[]Segment → OneBot11 数组; 含 CQ 码 → segments 数组。
func normalizeMessage(msg any) any {
	switch v := msg.(type) {
	case string:
		// 先修复 LLM 生成的 CQ 码格式瑕疵（如 "[ CQ:face,id=66]"），再解析
		v = NormalizeCQCodes(v)
		if HasCQCode(v) {
			return ParseCQCodes(v)
		}
		return v
	case Segment:
		return []Segment{v}
	case []Segment:
		return v
	case *MessageBuilder:
		return v.Build()
	default:
		log.Warn("未知消息类型", "type", fmt.Sprintf("%T", msg))
		return fmt.Sprint(v)
	}
}

// resolveImageAssets 把消息中的图床图片引用（file="imgs://<id>"）与表情引用（file="stk://<短UUID>"）
// 替换为 base64。纯文本消息（不含 CQ 码）原样返回；只处理 []Segment。
func (p *Adapter) resolveImageAssets(msg any) any {
	segs, ok := msg.([]Segment)
	if !ok || len(segs) == 0 {
		return msg
	}
	p.mu.RLock()
	imageResolver := p.imageResolver
	stickerResolver := p.stickerResolver
	p.mu.RUnlock()
	out := make([]Segment, len(segs))
	for i, seg := range segs {
		if seg.Type == "image" {
			if f, ok := seg.Data["file"].(string); ok && f != "" {
				// 表情引用：stk://<短UUID> → 短 UUID 查表情 → 图床长 UUID → base64，强制 subType=1
				if strings.HasPrefix(f, "stk://") && stickerResolver != nil {
					if b64, resolved := stickerResolver(strings.TrimPrefix(f, "stk://")); resolved {
						seg.Data["file"] = b64
						seg.Data["subType"] = 1
					}
				} else if imageResolver != nil {
					// 图床图片引用：imgs://<id>（含普通图片/富文本内嵌表情场景）
					if b64, resolved := imageResolver(f); resolved {
						seg.Data["file"] = b64
					}
				}
			}
		}
		out[i] = seg
	}
	return out
}

// callAndParse 调用 API 并解析单个对象响应。
func callAndParse[T any](p *Adapter, action string, params map[string]any) (*T, error) {
	rsp, err := p.call(action, params)
	if err != nil {
		return nil, err
	}
	if rsp == nil {
		return nil, fmt.Errorf("api %s: nil response", action)
	}
	dataStr, ok := rsp.Data.(string)
	if !ok {
		return nil, fmt.Errorf("api %s: unexpected data type %T", action, rsp.Data)
	}
	var result T
	if err := json.Unmarshal([]byte(dataStr), &result); err != nil {
		return nil, fmt.Errorf("api %s parse: %w, data=%s", action, err, dataStr)
	}
	return &result, nil
}

// callAndParseSlice 调用 API 并解析数组响应。
func callAndParseSlice[T any](p *Adapter, action string, params map[string]any) ([]T, error) {
	rsp, err := p.call(action, params)
	if err != nil {
		return nil, err
	}
	if rsp == nil {
		return nil, fmt.Errorf("api %s: nil response", action)
	}
	dataStr, ok := rsp.Data.(string)
	if !ok {
		return nil, fmt.Errorf("api %s: unexpected data type %T", action, rsp.Data)
	}
	var result []T
	if err := json.Unmarshal([]byte(dataStr), &result); err != nil {
		return nil, fmt.Errorf("api %s parse: %w, data=%s", action, err, dataStr)
	}
	return result, nil
}

// callAndCheckYes 检查 API 响应 data 中 yes 字段是否为 true。
func callAndCheckYes(p *Adapter, action string) (bool, error) {
	rsp, err := p.call(action, nil)
	if err != nil {
		return false, err
	}
	if rsp == nil {
		return false, fmt.Errorf("api %s: nil response", action)
	}
	dataStr, ok := rsp.Data.(string)
	if !ok {
		return false, fmt.Errorf("api %s: unexpected data type %T", action, rsp.Data)
	}
	var result struct {
		Yes bool `json:"yes"`
	}
	json.Unmarshal([]byte(dataStr), &result)
	return result.Yes, nil
}
