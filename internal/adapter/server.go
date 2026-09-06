package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RomiChan/websocket"
	"github.com/tidwall/gjson"
)

// wsServer 管理 OneBot11 反向 WebSocket 连接。
type wsServer struct {
	listener net.Listener
	conns    map[int64]*wsConn
	mu       sync.RWMutex
	events   chan Event
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

type wsConn struct {
	conn       *websocket.Conn
	selfID     int64
	remoteAddr string
	mu         sync.Mutex
	seq        uint64
	responses  map[string]chan *APIResponse

	// closed 标志 + closeOnce：close() 幂等，所有 channel 操作（注册/投递/关闭）
	// 都在 conn.mu 保护下并检查 closed，避免断线重连/替换时向已关闭 channel 发送
	// 或对 nil map 赋值导致 panic。
	closed    bool
	closeOnce sync.Once
}

func newWSServer(ctx context.Context, addr, token string, events chan Event) (*wsServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	// 使用 context.Background() 而非传入的 ctx, 避免 caller (如 SyncConfig 的
	// 5s 超时 ctx) cancel 后级联取消 ws server 导致服务立即停止。
	// 生命周期完全由 wsServer.stop() 控制。
	ctx, cancel := context.WithCancel(context.Background())

	s := &wsServer{
		listener: listener,
		conns:    make(map[int64]*wsConn),
		events:   events,
		ctx:      ctx,
		cancel:   cancel,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.handleWS(w, r, token)
	})

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Info("ws server 启动", "addr", addr)
		if err := http.Serve(s.listener, mux); err != nil && !isServerClosed(err) {
			log.Error("http serve 异常", "err", err)
		}
		log.Info("ws server 已停止", "addr", addr)
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-s.ctx.Done()
		s.listener.Close()
	}()

	return s, nil
}

func (s *wsServer) stop() {
	s.cancel()
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.close()
	}
	s.conns = nil
}

func (s *wsServer) selfID() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id := range s.conns {
		return id
	}
	return 0
}

func (s *wsServer) connCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

func (s *wsServer) connIDs() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]int64, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	return ids
}

// ConnDetail 表示一条 WS 连接的展示信息。
type ConnDetail struct {
	ID   int64  `json:"id"`
	IP   string `json:"ip"`
	Self int64  `json:"self_id"`
}

// connDetails 返回所有连接的 ID + RemoteAddr 信息（供前端展示）。
func (s *wsServer) connDetails() []ConnDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ConnDetail, 0, len(s.conns))
	for id, c := range s.conns {
		out = append(out, ConnDetail{ID: id, IP: c.remoteAddr, Self: c.selfID})
	}
	return out
}

func (s *wsServer) callAPI(action string, params map[string]any) (*APIResponse, error) {
	selfID := s.selfID()
	if selfID == 0 {
		return nil, fmt.Errorf("no active connection")
	}

	s.mu.RLock()
	conn, ok := s.conns[selfID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no connection for selfID=%d", selfID)
	}

	echo := fmt.Sprint(atomic.AddUint64(&conn.seq, 1))
	ch := make(chan *APIResponse, 1)

	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return nil, fmt.Errorf("connection closed (selfID=%d)", selfID)
	}
	conn.responses[echo] = ch
	conn.mu.Unlock()

	defer func() {
		conn.mu.Lock()
		if ch, ok := conn.responses[echo]; ok {
			delete(conn.responses, echo)
			// 关闭 channel 释放等待方；readLoop 投递在 conn.mu 保护下检查
			// responses 是否仍含该 echo，因此不会向已关闭 channel 发送。
			close(ch)
		}
		conn.mu.Unlock()
	}()

	req := APIRequest{
		Action: action,
		Params: params,
		Echo:   echo,
	}

	conn.mu.Lock()
	err := conn.conn.WriteJSON(req)
	conn.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write api request: %w", err)
	}

	select {
	case rsp, ok := <-ch:
		if !ok {
			// channel 被 close() 关闭（连接断开）：等待中的调用方安全退出
			return nil, fmt.Errorf("connection closed while waiting for api %s", action)
		}
		if rsp.Status == "failed" {
			return rsp, fmt.Errorf("api %s failed: retcode=%d msg=%s", action, rsp.RetCode, rsp.Msg)
		}
		return rsp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("api %s timeout", action)
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *wsServer) handleWS(w http.ResponseWriter, r *http.Request, token string) {
	if token != "" && !checkAuth(r, token) {
		log.Warn("token 不匹配", "remote_addr", r.RemoteAddr)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("ws upgrade 失败", "remote_addr", r.RemoteAddr, "err", err)
		return
	}

	var handshake struct {
		SelfID int64 `json:"self_id"`
	}
	if err := conn.ReadJSON(&handshake); err != nil {
		log.Warn("握手失败", "remote_addr", r.RemoteAddr, "err", err)
		conn.Close()
		return
	}

	wsc := &wsConn{
		conn:       conn,
		selfID:     handshake.SelfID,
		remoteAddr: r.RemoteAddr,
		responses:  make(map[string]chan *APIResponse),
	}

	s.mu.Lock()
	if old, ok := s.conns[handshake.SelfID]; ok {
		old.close()
	}
	s.conns[handshake.SelfID] = wsc
	s.mu.Unlock()

	log.Info("客户端连接", "self_id", handshake.SelfID, "remote_addr", r.RemoteAddr)
	s.readLoop(wsc)
}

func (s *wsServer) readLoop(wsc *wsConn) {
	defer func() {
		s.mu.Lock()
		// 仅当 map 中仍存的是自己时才删除：同 selfID 重连时旧连接的清理
		// 不能误删新注册的连接。
		if s.conns[wsc.selfID] == wsc {
			delete(s.conns, wsc.selfID)
		}
		s.mu.Unlock()
		wsc.close()
		log.Info("客户端断开", "self_id", wsc.selfID)
	}()

	for {
		_, payload, err := wsc.conn.ReadMessage()
		if err != nil {
			if !isConnClosed(err) {
				log.Warn("读取消息失败", "self_id", wsc.selfID, "err", err)
			}
			return
		}

		rsp := gjson.ParseBytes(payload)

		// API 调用响应
		if echo := rsp.Get("echo"); echo.Exists() {
			wsc.mu.Lock()
			// 在锁内检查 closed + channel 是否仍注册，且用 select-default 非阻塞投递，
			// 避免向已关闭/已删除的 channel 发送导致 panic。
			if !wsc.closed {
				if ch, ok := wsc.responses[echo.String()]; ok {
					select {
					case ch <- &APIResponse{
						Status:  rsp.Get("status").String(),
						RetCode: rsp.Get("retcode").Int(),
						Data:    rsp.Get("data").Raw,
						Msg:     rsp.Get("msg").String(),
					}:
					default:
						// 调用方已有超时兜底，丢弃即可
					}
				}
			}
			wsc.mu.Unlock()
			continue
		}

		// 心跳忽略
		if rsp.Get("meta_event_type").String() == "heartbeat" {
			continue
		}

		// 解析事件
		ev := parseEvent(payload)
		if ev.PostType != "" {
			select {
			case s.events <- ev:
			default:
				log.Warn("events channel 满, 丢弃事件")
			}
		}
	}
}

func parseEvent(raw []byte) Event {
	ev := Event{Raw: raw}
	json.Unmarshal(raw, &ev)

	switch ev.PostType {
	case "message":
		var msg MessageEvent
		json.Unmarshal(raw, &msg)
		// 无损规范化 reply id（大整数不经过 float64），见 normalizeReplyIDs
		normalizeReplyIDs(&msg, raw)
		ev.Message = &msg
		log.Info("收到消息", "type", msg.MessageType, "user_id", msg.UserID, "content", msg.RawMessage)
	case "notice":
		var n NoticeEvent
		json.Unmarshal(raw, &n)
		ev.Notice = &n
		log.Info("收到通知", "type", n.NoticeType, "user_id", n.UserID)
	case "request":
		var r RequestEvent
		json.Unmarshal(raw, &r)
		ev.Request = &r
		log.Info("收到请求", "type", r.RequestType, "user_id", r.UserID)
	case "meta_event":
		var m MetaEvent
		json.Unmarshal(raw, &m)
		ev.Meta = &m
		log.Info("元事件", "type", m.MetaEventType)
	}

	return ev
}

// normalizeReplyIDs 把 reply 段 data.id 从原始 JSON 无损规范化为 int64。
// 普通 json.Unmarshal 会把大整数落入 map[string]any → float64（QQ message_id
// 可达 19 位 > 2^53），导致引用关联/撤回失效；这里用 gjson 按字符串读取原值
// 再转 int64，规避 float64 中间态的精度丢失。
func normalizeReplyIDs(msg *MessageEvent, raw []byte) {
	for i := range msg.Message {
		if msg.Message[i].Type != "reply" {
			continue
		}
		p := fmt.Sprintf("message.%d.data.id", i)
		if !gjson.GetBytes(raw, p).Exists() {
			continue
		}
		if id, err := strconv.ParseInt(gjson.GetBytes(raw, p).String(), 10, 64); err == nil {
			msg.Message[i].Data["id"] = id
		}
	}
}

func (c *wsConn) close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.closed = true
		c.conn.Close()
		for _, ch := range c.responses {
			close(ch)
		}
		c.responses = nil
	})
}

func checkAuth(r *http.Request, token string) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		auth = r.URL.Query().Get("access_token")
	} else {
		_, after, ok := strings.Cut(auth, " ")
		if ok {
			auth = after
		}
	}
	return auth == token
}

func isServerClosed(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "Server closed")
}

func isConnClosed(err error) bool {
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
		strings.Contains(err.Error(), "closed network connection")
}
