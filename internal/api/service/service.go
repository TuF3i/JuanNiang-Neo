package service

import (
	ragcaller "JuanNiang-Neo/infrastructure/rag/handler"
	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/mcp"
	"JuanNiang-Neo/internal/agent/memory"
	"JuanNiang-Neo/internal/agent/prompt"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/agent/skill"
	"JuanNiang-Neo/internal/api/dto"
	"JuanNiang-Neo/internal/api/middleware"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/core/ragtag"
	"JuanNiang-Neo/internal/logging"
	"JuanNiang-Neo/internal/pluggin"
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var log = logging.NewModule("api")

// builtinToolPrefix 内置工具的 ID 前缀（如 builtin:send_group_msg）。
const builtinToolPrefix = "builtin:"

// pluginNameRe 插件名白名单：仅允许字母/数字/下划线/连字符。
// 插件名会拼接进文件路径（data/pluggins/<name>），必须拒绝 "/"、".." 等
// 路径分隔符，防止上传/删除时路径穿越到插件目录之外。
var pluginNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validPluginName 校验插件名是否安全（非空 + 白名单）。
func validPluginName(name string) bool {
	return name != "" && pluginNameRe.MatchString(name)
}

func (s *Service) Login(ctx context.Context, c *app.RequestContext) {
	var data dto.LoginReq

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	user, err := s.DAO.User.GetByUsername(ctx, data.Username)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.UserOrPasswordWrong, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(data.Password)) != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.UserOrPasswordWrong, nil))
		return
	}

	token, err := middleware.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.GenTokenFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.TokenResp{Token: token}))
}

func (s *Service) ChangePassword(ctx context.Context, c *app.RequestContext) {
	var data dto.ChangePasswordReq

	userID := c.GetUint("user_id")

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	user, err := s.DAO.User.GetByUsername(ctx, "admin")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.UserNotExists, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(data.OldPassword)) != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OriginPasswordWrong, nil))
		return
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(data.NewPassword), bcrypt.DefaultCost)
	if err := s.DAO.User.UpdatePassword(ctx, userID, string(hash)); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.UpdatePasswordFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) GetAdapterStatus(ctx context.Context, c *app.RequestContext) {
	raw := s.Adapter.Status()

	status := dto.AdapterStatus{
		Running:    raw.Running,
		ListenAddr: raw.ListenAddr,
		SelfID:     raw.SelfID,
		ConnCount:  raw.ConnCount,
		ConnIDs:    raw.ConnIDs,
		Conns:      raw.Conns,
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, status))
}

func (s *Service) GetAdapterConfig(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.Onebot11Adapter.GetAdapterConfig(ctx)
	if err != nil {
		// 数据库无配置 → 初始化默认配置再读取一次, 避免前端报 "record not found"
		if initErr := s.DAO.Onebot11Adapter.InitAdapterConfig(ctx); initErr != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: fmt.Sprintf("init: %v; query: %v", initErr, err)}))
			return
		}
		raw, err = s.DAO.Onebot11Adapter.GetAdapterConfig(ctx)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	data := dto.AdapterConfig{
		Addr:           raw.Addr,
		Port:           raw.Port,
		Token:          raw.Token,
		AdminQQNumbers: raw.AdminQQNumbers,
		Enabled:        raw.Enabled,
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, data))
}

func (s *Service) UpdateAdapterConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateAdapterConfigReq

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	conf := adapter.Config{
		Addr:   data.Addr,
		Port:   data.Port,
		Token:  data.Token,
		Admins: data.AdminQQNumbers,
		Enable: data.Enabled,
	}

	if err := s.DAO.Onebot11Adapter.UpdateAdapterConfig(ctx, &models.Onebot11Adapter{
		Addr:           data.Addr,
		Port:           data.Port,
		Token:          data.Token,
		AdminQQNumbers: data.AdminQQNumbers,
		Enabled:        data.Enabled,
	}); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	if err := s.Adapter.SyncConfig(ctx, conf); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.UpdateAdapterConfigFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) RestartAdapter(ctx context.Context, c *app.RequestContext) {
	if err := s.Adapter.Restart(ctx); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) ListProviders(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.Provider.List(ctx, "")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	list := dto.RawProviderList2Resp(raw)

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, list))
}

// ListProviderPresets 返回国产厂商协议能力预设表（供前端渲染协议下拉）。
func (s *Service) ListProviderPresets(ctx context.Context, c *app.RequestContext) {
	presets := provider.ListProviderPresets()
	resp := make([]dto.ProviderPresetResp, 0, len(presets))
	for _, p := range presets {
		rp := dto.ProviderPresetResp{Key: p.Key, Name: p.Name}
		for _, proto := range p.Protocols {
			rp.Protocols = append(rp.Protocols, dto.ProviderProtocolResp{
				APIMode:    string(proto.APIMode),
				BaseURL:    proto.BaseURL,
				AuthHeader: proto.AuthHeader,
				Note:       proto.Note,
			})
		}
		resp = append(resp, rp)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

func (s *Service) GetProvider(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.Provider.GetByID(ctx, c.Param("id"))

	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ProviderNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	data := dto.RawProvider2Resp(raw)

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, data))
}

func (s *Service) AddProvider(ctx context.Context, c *app.RequestContext) {
	var data dto.AddProviderReq

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	id := newUUID()
	providerConfig := models.Provider{
		ID:             id,
		Name:           data.Name,
		Type:           data.Type,
		Endpoint:       data.Endpoint,
		Token:          data.Token,
		Model:          data.Model,
		Temperature:    float32(data.Temperature),
		IsActive:       data.IsActive,
		EnableThinking: data.EnableThinking,
	}
	dto.ApplyProviderFields(&providerConfig, data.APIMode, data.ThinkingEffort, data.ThinkingBudget, data.MaxTokens, data.TopP, data.TopK, data.FrequencyPenalty, data.PresencePenalty, data.RepetitionPenalty, data.ProviderKey, data.AuthHeader, data.URLMode)

	// 事务内行锁校验 + 写入：并发 AddProvider 请求无法同时通过"同类型已存在启用"
	// 检查并各自创建 is_active=true 的同类型 Provider。
	// 事务开头 ListForUpdate 锁定全部 Provider 行，与 Update/Delete/Toggle 加锁顺序一致，避免死锁。
	err := s.providerTx(ctx, func(tx *gorm.DB) error {
		if !data.IsActive {
			return s.DAO.Provider.WithTx(tx).Create(ctx, &providerConfig)
		}
		list, err := s.DAO.Provider.WithTx(tx).ListForUpdate(ctx, "")
		if err != nil {
			return err
		}
		// 防御：若同类型已存在启用 Provider，新添加的强制为未激活（不抢占已有开启的），
		// 用户需在列表中手动切换。避免前端异常/误操作导致两个同类型同时激活。
		for _, p := range list {
			if p.IsActive && p.Type == data.Type {
				data.IsActive = false
				providerConfig.IsActive = false
				break
			}
		}
		return s.DAO.Provider.WithTx(tx).Create(ctx, &providerConfig)
	})
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：先移除同类型旧的运行时 Provider，再添加新的
	if data.IsActive && s.ProviderGroup != nil {
		provType := provider.ModelType(data.Type)
		for _, p := range s.ProviderGroup.ListProviders() {
			if p.Type() == provType {
				s.ProviderGroup.DelProvider(p.ID())
			}
		}
		s.ProviderGroup.AddProvider(provider.NewProvider(provider.ProviderConfigFromModel(&providerConfig)))
	}

	// Provider 变更影响 Eino Agent 的 model adapter（构建时持有实例引用），必须重建
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, data))
}

func (s *Service) UpdateProvider(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateProviderReq

	id := c.Param("id")

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 目标状态校验：激活、停用、变更类型都必须保证操作后至少保留一个启用的 Text 模型。
	// 放在分支前：IsActive=true 且把最后一个 Text Provider 改为其他类型时同样拦截。
	targetType := provider.ModelType(data.Type)
	targetActive := data.IsActive

	providerConfig := models.Provider{
		ID:             id,
		Name:           data.Name,
		Type:           data.Type,
		Endpoint:       data.Endpoint,
		Token:          data.Token,
		Model:          data.Model,
		Temperature:    float32(data.Temperature),
		IsActive:       data.IsActive,
		EnableThinking: data.EnableThinking,
	}
	dto.ApplyProviderFields(&providerConfig, data.APIMode, data.ThinkingEffort, data.ThinkingBudget, data.MaxTokens, data.TopP, data.TopK, data.FrequencyPenalty, data.PresencePenalty, data.RepetitionPenalty, data.ProviderKey, data.AuthHeader, data.URLMode)

	provType := provider.ModelType(data.Type)
	providerConfig_ := provider.ProviderConfigFromModel(&providerConfig)

	// 事务内行锁校验 + 写操作：并发请求无法同时通过校验并移除最后一个启用 Text Provider。
	// 事务开头 ListForUpdate 锁定全部 Provider 行，与 Delete/Toggle 加锁顺序一致，避免死锁。
	err := s.providerTx(ctx, func(tx *gorm.DB) error {
		txProvider := s.DAO.Provider.WithTx(tx)
		if err := s.checkTextModelRemains(ctx, txProvider, id, &targetType, &targetActive); err != nil {
			return err
		}
		// 同类型只能有一个 Active：激活前先停用同类型其他 Provider
		if data.IsActive {
			if err := txProvider.DeactivateByType(ctx, data.Type, id); err != nil {
				return err
			}
		}
		return txProvider.Update(ctx, &providerConfig)
	})
	if err != nil {
		providerWriteErrResp(c, err)
		return
	}

	// 运行时同步（DB 已提交，以 DB 为准）：先移除同类型旧的，再同步新配置
	if s.ProviderGroup != nil {
		if data.IsActive {
			for _, p := range s.ProviderGroup.ListProviders() {
				if p.Type() == provType && p.ID() != id {
					s.ProviderGroup.DelProvider(p.ID())
				}
			}
		}
		s.ProviderGroup.SyncConfig(providerConfig_)
	}

	// Provider 变更影响 Eino Agent 的 model adapter，必须重建
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ErrTextModelRequired 是"操作后无启用 Text 模型"的业务哨兵错误。
// 与 DAO 查询错误区分：仅该错误映射为 dto.TextProviderRequired（40048），
// 其余（如 DB 故障）映射为 ServerInternalErr，避免客户端误判。
var ErrTextModelRequired = errors.New("至少保留一个启用的 Text 模型")

// providerWriteErrResp 统一 Provider 写操作失败响应：
// 业务校验失败（ErrTextModelRequired）→ 40047；其他错误 → 500。
func providerWriteErrResp(c *app.RequestContext, err error) {
	resp := dto.ServerInternalErr
	if errors.Is(err, ErrTextModelRequired) {
		resp = dto.TextProviderRequired
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(resp, dto.ErrorDetail{ErrorDetail: err.Error()}))
}

// isDeadlockErr 判断是否为 Postgres 死锁/序列化失败（SQLSTATE 40P01 / 40001），
// 这类错误可安全重试整个事务。使用结构化 *pgconn.PgError 判定，
// 不依赖错误文本匹配，避免错误详情误触发重试。
func isDeadlockErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40P01" || pgErr.Code == "40001"
}

// providerTx 在事务内执行 fn；遇死锁/序列化冲突时退避重试（最多 3 次）。
// 事务闭包由 gorm DB.Transaction 管理：返回错误或 panic 时自动回滚，不泄漏连接。
// 退避防止竞争事务同时重试互相踩踏；ctx 取消时立即终止，不再发起新事务。
func (s *Service) providerTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.DAO.DB.WithContext(ctx).Transaction(fn)
		if !isDeadlockErr(err) {
			return err
		}
		if attempt < 2 {
			backoff := time.Duration(attempt+1) * 50 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return err
}

// checkTextModelRemains 校验停用/删除/更新操作后仍至少保留一个启用的 Text 模型。
// Eino Agent 构建依赖 Text 模型：若全部停用，rebuild 报"无可用 Text 模型"、
// 对话功能整体失能（历史故障）。targetType/targetActive 为操作后目标 provider
// 的预期状态；nil 表示目标将被删除或不再处于启用态。
// p 应传入事务内 DAO（WithTx），以行锁（ListForUpdate）保证校验与写操作原子性。
func (s *Service) checkTextModelRemains(ctx context.Context, p *dao.ProviderDAO, id string, targetType *provider.ModelType, targetActive *bool) error {
	list, err := p.ListForUpdate(ctx, "")
	if err != nil {
		return err
	}
	activeText := 0
	for _, pr := range list {
		if pr.ID == id {
			continue
		}
		if pr.IsActive && provider.ModelType(pr.Type) == provider.ModelTypeText {
			activeText++
		}
	}
	if targetType != nil && *targetType == provider.ModelTypeText && targetActive != nil && *targetActive {
		activeText++
	}
	if activeText == 0 {
		return ErrTextModelRequired
	}
	return nil
}

func (s *Service) DeleteProvider(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// 事务内行锁校验 + 删除：并发请求无法同时通过校验并删除最后一个启用 Text Provider
	err := s.providerTx(ctx, func(tx *gorm.DB) error {
		txProvider := s.DAO.Provider.WithTx(tx)
		if err := s.checkTextModelRemains(ctx, txProvider, id, nil, nil); err != nil {
			return err
		}
		return txProvider.Delete(ctx, id)
	})
	if err != nil {
		providerWriteErrResp(c, err)
		return
	}

	if s.ProviderGroup != nil {
		s.ProviderGroup.DelProvider(id)
	}

	// Provider 变更影响 Eino Agent 的 model adapter，必须重建
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) ToggleProvider(ctx context.Context, c *app.RequestContext) {
	var data dto.ToggleProviderReq

	id := c.Param("id")

	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	var raw *models.Provider
	err := s.providerTx(ctx, func(tx *gorm.DB) error {
		txProvider := s.DAO.Provider.WithTx(tx)
		// 统一加锁顺序：事务开头先锁定全部 Provider 行（与 Update/Delete 一致），
		// 再执行 GetByID 与写操作，避免并发写路径形成循环等待（死锁）。
		if _, err := txProvider.ListForUpdate(ctx, ""); err != nil {
			return err
		}
		if data.IsActive {
			// 获取当前 Provider 信息以确认类型（事务内，与写操作一致）。
			// 直接用 %w 包装原始错误：First 对"记录不存在"本就返回 gorm.ErrRecordNotFound，
			// 连接错误/死锁（40P01）等不会被误判为 ProviderNotExist。
			var err error
			raw, err = txProvider.GetByID(ctx, id)
			if err != nil {
				return fmt.Errorf("获取 provider 失败: %w", err)
			}
			// 同类型只能有一个 Active：先停用同类型其他 Provider
			if err := txProvider.DeactivateByType(ctx, raw.Type, id); err != nil {
				return err
			}
		} else {
			// 停用前校验：至少保留一个启用的 Text 模型（事务内行锁校验 + 写操作）
			if err := s.checkTextModelRemains(ctx, txProvider, id, nil, nil); err != nil {
				return err
			}
		}
		return txProvider.SetActive(ctx, id, data.IsActive)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ProviderNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
		providerWriteErrResp(c, err)
		return
	}

	// 运行时同步（DB 已提交，以 DB 为准）
	if s.ProviderGroup != nil {
		if data.IsActive {
			provType := provider.ModelType(raw.Type)
			for _, p := range s.ProviderGroup.ListProviders() {
				if p.Type() == provType {
					s.ProviderGroup.DelProvider(p.ID())
				}
			}
			s.ProviderGroup.AddProvider(provider.NewProvider(provider.ProviderConfigFromModel(raw)))
		} else {
			s.ProviderGroup.DelProvider(id)
		}
	}

	// Provider 变更影响 Eino Agent 的 model adapter，必须重建
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// TestProvider 用请求中的配置构建临时 Provider 并发送一条最小探测消息，
// 校验 API 地址 / Token / 模型名是否可用（不落库、不影响运行时）。
func (s *Service) TestProvider(ctx context.Context, c *app.RequestContext) {
	var data dto.AddProviderReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.ProviderGroup == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.TestProviderResp{Ok: false, Message: "Provider 运行时未初始化"}))
		return
	}

	m := &models.Provider{
		Name:           data.Name,
		Type:           data.Type,
		Endpoint:       data.Endpoint,
		Token:          data.Token,
		Model:          data.Model,
		Temperature:    float32(data.Temperature),
		EnableThinking: data.EnableThinking,
	}
	dto.ApplyProviderFields(m, data.APIMode, data.ThinkingEffort, data.ThinkingBudget, data.MaxTokens, data.TopP, data.TopK, data.FrequencyPenalty, data.PresencePenalty, data.RepetitionPenalty, data.ProviderKey, data.AuthHeader, data.URLMode)

	p := provider.NewProvider(provider.ProviderConfigFromModel(m))
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := p.Chat(testCtx, provider.ChatRequest{
		Messages:  []provider.ChatMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 16,
	})
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.TestProviderResp{Ok: false, Message: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.TestProviderResp{Ok: true, Message: resp.Message.Content}))
}

func (s *Service) ListMCPServers(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.MCPServer.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawMCPServerList2Resp(list)))
}

func (s *Service) GetMCPServer(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.MCPServer.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.MCPNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawMCPServer2Resp(raw)))
}

func (s *Service) AddMCPServer(ctx context.Context, c *app.RequestContext) {
	var data dto.AddMCPServerReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	m := models.MCPServer{
		ID:            newUUID(),
		Name:          data.Name,
		ServerURL:     data.ServerURL,
		Headers:       ensureNonNilMap(data.Headers),
		Timeout:       data.Timeout,
		RetryCount:    data.RetryCount,
		ToolFilter:    ensureNonNilSlice(data.ToolFilter),
		AutoReconnect: data.AutoReconnect,
		IsActive:      data.IsActive,
	}
	if err := s.DAO.MCPServer.Create(ctx, &m); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：如果 Active，创建 MCP 客户端并连接
	if data.IsActive && s.MCPGroup != nil {
		client := mcp.NewSSEMCPClient(buildMcpSSEConfig(&m))
		if err := client.Connect(ctx); err != nil {
			// 连接失败不影响 DB 写入结果，仅记录日志
			log.Error("MCP 连接失败", "name", m.Name, "err", err)
		} else {
			s.MCPGroup.AddMCP(client)
		}
	}

	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawMCPServer2Resp(&m)))
}

func (s *Service) UpdateMCPServer(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateMCPServerReq
	id := c.Param("id")
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	m := models.MCPServer{
		ID:            id,
		Name:          data.Name,
		ServerURL:     data.ServerURL,
		Headers:       ensureNonNilMap(data.Headers),
		Timeout:       data.Timeout,
		RetryCount:    data.RetryCount,
		ToolFilter:    ensureNonNilSlice(data.ToolFilter),
		AutoReconnect: data.AutoReconnect,
		IsActive:      data.IsActive,
	}
	if err := s.DAO.MCPServer.Update(ctx, &m); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：断开旧连接，若 Active 则重连
	if s.MCPGroup != nil {
		if old, ok := s.MCPGroup.GetMCP(id); ok {
			old.Disconnect()
			s.MCPGroup.DelMCP(id)
		}
		if data.IsActive {
			client := mcp.NewSSEMCPClient(buildMcpSSEConfig(&m))
			if err := client.Connect(ctx); err != nil {
				log.Error("MCP 重连失败", "name", m.Name, "err", err)
			} else {
				s.MCPGroup.AddMCP(client)
			}
		}
	}

	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawMCPServer2Resp(&m)))
}

func (s *Service) DeleteMCPServer(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// 运行时同步：断开连接并移除
	if s.MCPGroup != nil {
		if old, ok := s.MCPGroup.GetMCP(id); ok {
			old.Disconnect()
			s.MCPGroup.DelMCP(id)
		}
	}

	if err := s.DAO.MCPServer.Delete(ctx, id); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) ToggleMCPServer(ctx context.Context, c *app.RequestContext) {
	var data dto.ToggleMCPServerReq
	id := c.Param("id")
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步
	if s.MCPGroup != nil {
		if data.IsActive {
			raw, err := s.DAO.MCPServer.GetByID(ctx, id)
			if err == nil {
				client := mcp.NewSSEMCPClient(buildMcpSSEConfig(raw))
				if err := client.Connect(ctx); err != nil {
					log.Error("MCP 连接失败", "id", id, "err", err)
				} else {
					s.MCPGroup.AddMCP(client)
				}
			}
		} else {
			if old, ok := s.MCPGroup.GetMCP(id); ok {
				old.Disconnect()
				s.MCPGroup.DelMCP(id)
			}
		}
	}

	if err := s.DAO.MCPServer.SetActive(ctx, id, data.IsActive); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) CheckMCPServer(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	raw, err := s.DAO.MCPServer.GetByID(ctx, id)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.MCPNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	client := mcp.NewSSEMCPClient(buildMcpSSEConfig(raw))
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Connect(checkCtx); err != nil {
		client.Disconnect()
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
			"healthy": false,
			"error":   err.Error(),
		}))
		return
	}
	client.Disconnect()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
		"healthy": true,
		"error":   "",
	}))
}

// ====================================================================
// Skill
// ====================================================================

func (s *Service) ListSkills(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.Skill.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSkillList2Resp(list)))
}

func (s *Service) AddSkill(ctx context.Context, c *app.RequestContext) {
	var data dto.AddSkillReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	sk := models.Skill{
		ID:           newUUID(),
		Name:         data.Name,
		Description:  data.Description,
		Keywords:     data.Keywords,
		RegexPattern: data.RegexPattern,
		PromptRefs:   data.PromptRefs,
		ToolRefs:     data.ToolRefs,
		McpRefs:      data.McpRefs,
		IsActive:     data.IsActive,
		IsSystem:     data.IsSystem,
		Priority:     data.Priority,
	}
	if err := s.DAO.Skill.Create(ctx, &sk); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步
	if s.SkillEngine != nil {
		s.SkillEngine.AddSkill(buildSkillConfig(&sk))
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSkill2Resp(&sk)))
}

func (s *Service) UpdateSkill(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateSkillReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	sk := models.Skill{
		ID:           c.Param("id"),
		Name:         data.Name,
		Description:  data.Description,
		Keywords:     data.Keywords,
		RegexPattern: data.RegexPattern,
		PromptRefs:   data.PromptRefs,
		ToolRefs:     data.ToolRefs,
		McpRefs:      data.McpRefs,
		IsActive:     data.IsActive,
		IsSystem:     data.IsSystem,
		Priority:     data.Priority,
	}
	if err := s.DAO.Skill.Update(ctx, &sk); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：AddSkill 会按 ID 覆盖已有 Skill
	if s.SkillEngine != nil {
		s.SkillEngine.AddSkill(buildSkillConfig(&sk))
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSkill2Resp(&sk)))
}

func (s *Service) DeleteSkill(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// 运行时同步
	if s.SkillEngine != nil {
		s.SkillEngine.DeleteSkill(id)
	}

	if err := s.DAO.Skill.Delete(ctx, id); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ====================================================================
// Prompt
// ====================================================================

func (s *Service) ListPrompts(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.Prompt.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawPromptList2Resp(list)))
}

func (s *Service) AddPrompt(ctx context.Context, c *app.RequestContext) {
	var data dto.AddPromptReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 禁止用户创建 system 类型（system 类型仅由系统锁定提示词使用）
	if data.Type == models.PromptTypeSystem {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PromptIsSystem, dto.ErrorDetail{ErrorDetail: "system 类型由系统保留，请使用 personality 或 custom"}))
		return
	}

	p := models.Prompt{
		ID:       newUUID(),
		Name:     data.Name,
		Content:  data.Content,
		Type:     data.Type,
		IsActive: data.IsActive,
	}
	if err := s.DAO.Prompt.Create(ctx, &p); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	s.invalidatePromptCache()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawPrompt2Resp(&p)))
}

func (s *Service) UpdatePrompt(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdatePromptReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 系统锁定提示词禁止修改
	id := c.Param("id")
	existing, err := s.DAO.Prompt.GetByID(ctx, id)
	if err == nil && existing != nil && existing.IsSystem {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PromptIsSystem, dto.ErrorDetail{ErrorDetail: "系统锁定提示词不允许修改"}))
		return
	}

	// 禁止用户将类型改为 system
	if data.Type == models.PromptTypeSystem {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PromptIsSystem, dto.ErrorDetail{ErrorDetail: "system 类型由系统保留，请使用 personality 或 custom"}))
		return
	}

	p := models.Prompt{
		ID:       id,
		Name:     data.Name,
		Content:  data.Content,
		Type:     data.Type,
		IsActive: data.IsActive,
	}
	if err := s.DAO.Prompt.Update(ctx, &p); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	s.invalidatePromptCache()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawPrompt2Resp(&p)))
}

func (s *Service) DeletePrompt(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// 系统锁定提示词禁止删除
	existing, err := s.DAO.Prompt.GetByID(ctx, id)
	if err == nil && existing != nil && existing.IsSystem {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PromptIsSystem, dto.ErrorDetail{ErrorDetail: "系统锁定提示词不允许删除"}))
		return
	}

	if err := s.DAO.Prompt.Delete(ctx, id); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	s.invalidatePromptCache()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) TogglePrompt(ctx context.Context, c *app.RequestContext) {
	var data dto.TogglePromptReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 系统锁定提示词禁止停用（但允许启用）
	id := c.Param("id")
	if !data.IsActive {
		existing, err := s.DAO.Prompt.GetByID(ctx, id)
		if err == nil && existing != nil && existing.IsSystem {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PromptIsSystem, dto.ErrorDetail{ErrorDetail: "系统锁定提示词不允许停用"}))
			return
		}
	}

	if err := s.DAO.Prompt.SetActive(ctx, id, data.IsActive); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	s.invalidatePromptCache()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// invalidatePromptCache 提示词变更后使 PromptManager 静态缓存失效，下次消息即时生效。
func (s *Service) invalidatePromptCache() {
	if s.PromptMgr != nil {
		s.PromptMgr.Invalidate()
	}
}

// ====================================================================
// Tool
// ====================================================================

func (s *Service) ListTools(ctx context.Context, c *app.RequestContext) {
	// 1. 读取数据库中所有 ToolConfig (代表可启用/停用的条目, 包括用户自定义与历史保存的内置工具条目)
	dbList, err := s.DAO.ToolConfig.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	// name → resp, 便于运行时内置工具查表合并 DB 状态
	byName := make(map[string]dto.ToolConfigResp, len(dbList))
	for _, item := range dbList {
		byName[item.Name] = dto.RawToolConfig2Resp(&item)
	}

	// 2. 合并运行时 ToolRegistry 中的内置工具 (这部分工具始终启用, 不由 DB 控制启停)
	out := make([]dto.ToolConfigResp, 0, len(dbList)+8)
	seen := make(map[string]bool)
	if s.ToolRegistry != nil {
		for _, t := range s.ToolRegistry.List() {
			if !t.IsBuiltin() {
				continue
			}
			name := t.Name()
			seen[name] = true
			paramsJSON, _ := json.Marshal(t.Parameters())
			resp := dto.ToolConfigResp{
				ID:          builtinToolPrefix + name,
				Name:        name,
				Description: t.Description(),
				Parameters:  models.JSONMap{},
				Timeout:     0,
				IsActive:    true, // 内置工具运行时常驻
				IsBuiltin:   true,
			}
			_ = json.Unmarshal(paramsJSON, &resp.Parameters)
			// 若 DB 中有对应 name 的 ToolConfig, 合并其状态
			if db, ok := byName[name]; ok {
				resp.ID = db.ID
				resp.IsActive = db.IsActive
				resp.AdminOnly = db.AdminOnly
				resp.CreatedAt = db.CreatedAt
			}
			out = append(out, resp)
		}
	}

	// 3. 追加 DB 中的非内置条目 (用户自定义工具)
	for _, item := range dbList {
		if seen[item.Name] {
			continue
		}
		out = append(out, dto.RawToolConfig2Resp(&item))
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, out))
}

func (s *Service) ToggleTool(ctx context.Context, c *app.RequestContext) {
	var data dto.ToggleToolReq
	id := c.Param("id")
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 内置工具: DB 不一定有对应记录, 仅当存在记录时才切换 DB 状态。
	// 内置工具运行时始终在注册表中, 不允许真正"停用"——DB 拒绝切换。
	if strings.HasPrefix(id, builtinToolPrefix) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ToolIsBuiltin, dto.ErrorDetail{ErrorDetail: "内置工具运行时常驻, 不支持启停"}))
		return
	}

	// 运行时同步：停用时从注册表移除；启用时内置工具已在 init 时注册，无需重复
	if s.ToolRegistry != nil && !data.IsActive {
		raw, err := s.DAO.ToolConfig.GetByID(ctx, id)
		if err == nil {
			s.ToolRegistry.Unregister(raw.Name)
		}
	}

	if err := s.DAO.ToolConfig.SetActive(ctx, id, data.IsActive); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// UpdateToolAdminOnly 更新工具的"仅管理员"标志（内置/自定义工具均可）。
// 更新成功后回调 OnUpdateToolAdminOnly 刷新 Agent 运行时权限表，立即生效。
func (s *Service) UpdateToolAdminOnly(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateToolAdminOnlyReq
	id := c.Param("id")
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 内置工具 ID 形如 builtin:<name>：按 name 查找/创建对应 ToolConfig 行
	name := id
	if strings.HasPrefix(id, builtinToolPrefix) {
		name = strings.TrimPrefix(id, builtinToolPrefix)
	}

	tc, err := s.DAO.ToolConfig.GetByName(ctx, name)
	if err == nil {
		if err := s.DAO.ToolConfig.SetAdminOnly(ctx, tc.ID, data.AdminOnly); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	} else {
		// 无对应行（内置工具首次设置）：幂等创建并写入标志
		desc := ""
		if s.ToolRegistry != nil {
			if t, ok := s.ToolRegistry.Get(name); ok {
				desc = t.Description()
			}
		}
		if err := s.DAO.ToolConfig.EnsureBuiltin(ctx, name, desc, data.AdminOnly); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	// 刷新 Agent 运行时名单，立即生效
	if s.OnUpdateToolAdminOnly != nil {
		s.OnUpdateToolAdminOnly()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ====================================================================
// Session
// ====================================================================

func (s *Service) ListSessions(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.Session.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSessionList2Resp(list)))
}

func (s *Service) GetSession(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.Session.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.SessionNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSession2Resp(raw)))
}

func (s *Service) DeleteSession(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// 使用 SessionManager 删除（同时清除 DB + Redis 缓存）
	if s.SessionMgr != nil {
		if err := s.SessionMgr.DeleteSession(ctx, id); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	} else {
		if err := s.DAO.Session.Delete(ctx, id); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ====================================================================
// Plugin
// ====================================================================

func (s *Service) ListPlugins(ctx context.Context, c *app.RequestContext) {
	// 优先使用 PluginEngine 的运行时列表（包含 manifest 信息与 is_system 标志）
	if s.PluginEngine != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, s.PluginEngine.ListMaps()))
		return
	}
	// 回退到 DB（仅静态记录）
	list, err := s.DAO.Plugin.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawPluginList2Resp(list)))
}

func (s *Service) UploadPlugin(ctx context.Context, c *app.RequestContext) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.EmptyFileToUpload, nil))
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "无法打开文件"}))
		return
	}
	defer src.Close()

	tmpFile, err := os.CreateTemp("", "pluggin-*.zip")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.TempFileCreateFail, nil))
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, src); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.WriteFileFail, nil))
		return
	}
	tmpFile.Close()

	reader, err := zip.OpenReader(tmpFile.Name())
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.InvalidZipFile, nil))
		return
	}
	defer reader.Close()

	var pluginName string
	for _, f := range reader.File {
		if f.Name == "pluggin.yaml" || filepath.Base(f.Name) == "pluggin.yaml" {
			pluginName = filepath.Base(filepath.Dir(f.Name))
			if pluginName == "." {
				pluginName = filepath.Base(tmpFile.Name())
				pluginName = pluginName[:len(pluginName)-4]
			}
			break
		}
	}
	if pluginName == "" {
		pluginName = file.Filename
		pluginName = pluginName[:len(pluginName)-len(filepath.Ext(pluginName))]
	}
	// 插件名白名单校验：仅允许字母/数字/下划线/连字符，
	// 防止含 "/"、".." 等路径分隔符的名称逃逸 data/pluggins 目录。
	if !validPluginName(pluginName) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.InvalidPluginName, dto.ErrorDetail{ErrorDetail: "插件名不合法: " + pluginName}))
		return
	}

	basePath := "data/pluggins"
	if s.PluginEngine != nil {
		basePath = s.PluginEngine.BasePath()
	}

	// 统一安装入口：暂存解压 → 全量校验 → 原子提交 → 引擎加载（升级先卸载），
	// 失败自动回滚旧版；pe 为 nil 时仅落盘不加载。
	if err := pluggin.InstallStagedPlugin(s.PluginEngine, basePath, pluginName, &reader.Reader, ""); err != nil {
		if errors.Is(err, pluggin.ErrUnsafeZipEntry) {
			log.Warn("插件包包含非法条目，拒绝安装", "plugin", pluginName, "err", err)
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginPackageUnsafe, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginLoadFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.PluginUploadResp{Name: pluginName, Status: "loaded"}))
}

func (s *Service) TogglePlugin(ctx context.Context, c *app.RequestContext) {
	var data dto.TogglePluginReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	name := c.Param("id")

	// 系统插件禁止停用（但允许"启用"——幂等场景）
	if !data.IsActive && s.PluginEngine != nil && s.PluginEngine.IsSystem(name) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginIsSystem, dto.ErrorDetail{ErrorDetail: "系统插件不允许停用"}))
		return
	}

	// 运行时同步：启用时加载插件，停用时卸载
	if s.PluginEngine != nil {
		if data.IsActive {
			if err := s.PluginEngine.Load(name); err != nil {
				log.Error("插件加载失败", "name", name, "err", err)
			}
		} else {
			if err := s.PluginEngine.Unload(name); err != nil {
				log.Error("插件卸载失败", "name", name, "err", err)
			}
		}
	}

	// 持久化启用/停用状态到插件自身的 pluggin.yaml
	if s.PluginEngine != nil {
		if err := s.PluginEngine.SetEnabled(name, data.IsActive); err != nil {
			log.Warn("写入插件 enabled 状态失败", "name", name, "err", err)
		}
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) DeletePlugin(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")

	// 插件名白名单校验：防止路径穿越删除 data/pluggins 之外任意目录
	if !validPluginName(name) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.InvalidPluginName, dto.ErrorDetail{ErrorDetail: "插件名不合法: " + name}))
		return
	}

	// 系统插件禁止删除
	if s.PluginEngine != nil && s.PluginEngine.IsSystem(name) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginIsSystem, dto.ErrorDetail{ErrorDetail: "系统插件不允许删除"}))
		return
	}

	// 运行时同步：先卸载
	if s.PluginEngine != nil {
		if err := s.PluginEngine.Unload(name); err != nil {
			// 非系统插件的卸载错误仅记录，不阻断删除流程
			log.Warn("插件卸载失败（继续删除流程）", "name", name, "err", err)
		}
	}

	// 通过 name 查找 DB 中的 Plugin 记录获取 UUID，然后删除 DB 记录
	dbPlugin, err := s.DAO.Plugin.GetByName(ctx, name)
	if err == nil {
		if err := s.DAO.Plugin.Delete(ctx, dbPlugin.ID); err != nil {
			log.Warn("插件 DB 记录删除失败", "name", name, "err", err)
		}
	}

	// 删除插件目录
	pluginDir := filepath.Join("data/pluggins", name)
	if err := os.RemoveAll(pluginDir); err != nil {
		log.Warn("插件目录删除失败", "dir", pluginDir, "err", err)
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ReloadAllPlugins 重载所有插件（先卸载非系统插件，再全量加载）。
func (s *Service) ReloadAllPlugins(ctx context.Context, c *app.RequestContext) {
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	if err := s.PluginEngine.ReloadAll(); err != nil {
		log.Error("重载所有插件失败", "err", err)
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginLoadFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ReloadPlugin 重载单个插件。
func (s *Service) ReloadPlugin(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	if err := s.PluginEngine.Reload(name); err != nil {
		log.Error("重载插件失败", "name", name, "err", err)
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginLoadFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// GetPluginConfig 返回插件的配置 schema + 当前值（供 Web 动态渲染）。
func (s *Service) GetPluginConfig(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, s.PluginEngine.ConfigSchemaMap(name)))
}

// SavePluginConfig 保存插件的配置值（写入 config.yaml）。
func (s *Service) SavePluginConfig(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	var req map[string]any
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	// 兼容 { values: {...} } 与直接 { key: value } 两种请求体
	values := req
	if v, ok := req["values"].(map[string]any); ok {
		values = v
	}
	if err := s.PluginEngine.SaveConfig(name, values); err != nil {
		log.Error("保存插件配置失败", "name", name, "err", err)
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// GetPluginReadme 返回插件的 README.md 内容（markdown）。
func (s *Service) GetPluginReadme(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	content, err := s.PluginEngine.GetReadme(name)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginNotExist, dto.ErrorDetail{ErrorDetail: "该插件没有 README.md"}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]string{"content": content}))
}

// GetPluginAvatar 返回插件的 avatar.png。
func (s *Service) GetPluginAvatar(ctx context.Context, c *app.RequestContext) {
	name := c.Param("id")
	if s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "PluginEngine 未初始化"}))
		return
	}
	data, err := s.PluginEngine.GetAvatar(name)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginNotExist, dto.ErrorDetail{ErrorDetail: "该插件没有 avatar.png"}))
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(consts.StatusOK, "image/png", data)
}

// ====================================================================
// Plugin Store（商店浏览 / 安装 / 镜像源管理）
// ====================================================================

// StoreList 拉取商店插件列表。
func (s *Service) StoreList(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	list, err := s.StoreClient.List()
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, list))
}

// StoreReadme 拉取商店中某插件的 README.md。
func (s *Service) StoreReadme(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	path := string(c.Query("path"))
	content, err := s.StoreClient.GetReadmeRaw(path)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]string{"content": content}))
}

// StoreAvatar 拉取商店中某插件的 avatar.png。
func (s *Service) StoreAvatar(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	path := string(c.Query("path"))
	data, err := s.StoreClient.GetAvatarRaw(path)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginNotExist, dto.ErrorDetail{ErrorDetail: "无头像"}))
		return
	}
	// 头像每次实时拉取，禁止浏览器缓存，保证仓库更新图片后刷新立即可见
	c.Header("Cache-Control", "no-store")
	c.Data(consts.StatusOK, "image/png", data)
}

// StoreInstall 从商店下载并安装插件。
func (s *Service) StoreInstall(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil || s.PluginEngine == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient/PluginEngine 未初始化"}))
		return
	}
	path := string(c.Query("path"))
	zipData, err := s.StoreClient.DownloadPlugin(path)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginLoadFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	name, err := pluggin.InstallPluginZip(s.PluginEngine, path, zipData)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.PluginLoadFail, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.PluginUploadResp{Name: name, Status: "loaded"}))
}

// StoreConfigGet 返回商店配置与镜像源列表。
func (s *Service) StoreConfigGet(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
		"config":  s.StoreClient.GetConfig(),
		"mirrors": s.StoreClient.ListMirrors(),
	}))
}

// StoreConfigUpdate 更新商店配置（repo 信息 + 镜像列表）。
func (s *Service) StoreConfigUpdate(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	var req struct {
		RepoOwner string   `json:"repo_owner"`
		RepoName  string   `json:"repo_name"`
		Branch    string   `json:"branch"`
		Mirrors   []string `json:"mirrors"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	// 若传了 mirrors 则整体覆盖自定义镜像列表（内置镜像始终前置）。
	var mirrors []string
	if req.Mirrors != nil {
		mirrors = req.Mirrors
	}
	if err := s.StoreClient.SetConfig(pluggin.StoreConfig{
		RepoOwner: req.RepoOwner,
		RepoName:  req.RepoName,
		Branch:    req.Branch,
		Mirrors:   mirrors,
	}); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// StoreMirrorAdd 添加一个自定义镜像源。
func (s *Service) StoreMirrorAdd(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	var req struct {
		Mirror string `json:"mirror"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.StoreClient.AddMirror(req.Mirror); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// StoreMirrorTest 测试指定镜像源是否可用（拉取 plugins.json 验证）。
func (s *Service) StoreMirrorTest(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	var req struct {
		Mirror string `json:"mirror"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if req.Mirror == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "镜像地址不能为空"}))
		return
	}
	latency, err := s.StoreClient.TestMirror(req.Mirror)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{"latency_ms": latency.Milliseconds()}))
}

// StoreMirrorSelect 手动指定生效镜像源（mirror 为空恢复默认自动选择）。
func (s *Service) StoreMirrorSelect(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	var req struct {
		Mirror string `json:"mirror"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.StoreClient.SelectMirror(req.Mirror); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// StoreMirrorRemove 删除一个自定义镜像源。
func (s *Service) StoreMirrorRemove(ctx context.Context, c *app.RequestContext) {
	if s.StoreClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "StoreClient 未初始化"}))
		return
	}
	var req struct {
		Mirror string `json:"mirror"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.StoreClient.RemoveMirror(req.Mirror); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ====================================================================
// ACL
// ====================================================================

func (s *Service) ListACLRules(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.ACL.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawACLRuleList2Resp(list)))
}

func (s *Service) AddACLRule(ctx context.Context, c *app.RequestContext) {
	var data dto.AddACLRuleReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	r := models.ACLRule{
		ChatAreaID: data.ChatAreaID,
		Scope:      data.Scope,
		Permission: data.Permission,
		TargetType: data.TargetType,
		UserIDs:    data.UserIDs,
		ToolIDs:    data.ToolIDs,
		MCPIDs:     data.MCPIDs,
	}

	// 运行时同步：使用 ACL.AddRule（存在则更新）
	if s.ACLMgr != nil {
		if err := s.ACLMgr.AddRule(ctx, &r); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	} else {
		if err := s.DAO.ACL.Create(ctx, &r); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawACLRule2Resp(&r)))
}

func (s *Service) DeleteACLRule(ctx context.Context, c *app.RequestContext) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.InvalidACLID, nil))
		return
	}

	// 运行时同步
	if s.ACLMgr != nil {
		if err := s.ACLMgr.RemoveRule(ctx, id); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	} else {
		if err := s.DAO.ACL.Delete(ctx, id); err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ====================================================================
// Chat Records
// ====================================================================

func (s *Service) GetChatRecords(ctx context.Context, c *app.RequestContext) {
	chatAreaID := c.Param("chatAreaID")
	limit := atoi(c.DefaultQuery("limit", "20"))
	offset := atoi(c.DefaultQuery("offset", "0"))
	role := c.Query("role")

	if role != "" {
		list, total, err := s.DAO.ChatRecord.ListByChatAreaAndRole(ctx, chatAreaID, role, limit, offset)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ChatRecordListResp{
			Total: total,
			List:  dto.RawChatRecordList2Resp(list),
		}))
		return
	}

	list, total, err := s.DAO.ChatRecord.ListByChatArea(ctx, chatAreaID, limit, offset)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ChatRecordListResp{
		Total: total,
		List:  dto.RawChatRecordList2Resp(list),
	}))
}

func (s *Service) GetChatAreaTokenUsage(ctx context.Context, c *app.RequestContext) {
	chatAreaID := c.Param("chatAreaID")
	sess, err := s.DAO.Session.GetOrCreate(ctx, chatAreaID)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSession2Resp(sess)))
}

func (s *Service) GetChatAreas(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.ChatArea.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawChatAreaList2Resp(list)))
}

// ====================================================================
// Overview
// ====================================================================

func (s *Service) GetOverview(ctx context.Context, c *app.RequestContext) {
	chatAreaCount, _ := s.DAO.ChatArea.Count(ctx)

	mcpList, _ := s.DAO.MCPServer.List(ctx)
	mcpCount := int64(len(mcpList))

	// Plugin 数量优先取运行时 PluginEngine (包含 manifest 加载的插件)，
	// 否则回退到 DB 记录数。
	var pluginCount int64
	if s.PluginEngine != nil {
		pluginCount = int64(len(s.PluginEngine.ListMaps()))
	} else {
		pluginList, _ := s.DAO.Plugin.List(ctx)
		pluginCount = int64(len(pluginList))
	}

	totalTokens, _ := s.DAO.ChatRecord.TotalTokenUsage(ctx)

	providerList, _ := s.DAO.Provider.List(ctx, "")
	skillList, _ := s.DAO.Skill.List(ctx)
	sessionList, _ := s.DAO.Session.List(ctx)

	// 系统状态
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// T2I / Sandbox / RAG 状态
	t2iActive := s.T2IClient != nil
	t2iHealthy := false
	if t2iActive {
		t2iHealthy = s.T2IClient.HealthCheck() == nil
	}
	sandboxActive := s.SandboxClient != nil
	sandboxHealthy := false
	if sandboxActive {
		sandboxHealthy = s.SandboxClient.HealthCheck() == nil
	}
	ragActive := s.RAGClient != nil
	ragHealthy := false
	if ragActive {
		ragHealthy = s.RAGClient.HealthCheck() == nil
	}

	// Adapter 运行状态
	adapterRunning := s.Adapter.Status().Running

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.OverviewResp{
		ChatAreaCount:   chatAreaCount,
		MCPCount:        mcpCount,
		AdapterCount:    1,
		PluginCount:     pluginCount,
		ProviderCount:   len(providerList),
		SkillCount:      len(skillList),
		SessionCount:    len(sessionList),
		TotalTokenUsage: totalTokens,

		CPUCount:          runtime.NumCPU(),
		GoroutineNum:      runtime.NumGoroutine(),
		MemAllocBytes:     memStats.Alloc,
		MemSysBytes:       memStats.Sys,
		MemHeapInUseBytes: memStats.HeapInuse,
		GoVersion:         runtime.Version(),

		AdapterRunning: adapterRunning,
		T2IActive:      t2iActive,
		T2IHealthy:     t2iHealthy,
		SandboxActive:  sandboxActive,
		SandboxHealthy: sandboxHealthy,
		RAGActive:      ragActive,
		RAGHealthy:     ragHealthy,
	}))
}

// GetDailyTokenUsage 返回近 N 天（默认 7，上限 30）的每日 Token 用量，日期连续、缺失补 0。
func (s *Service) GetDailyTokenUsage(ctx context.Context, c *app.RequestContext) {
	days := 7
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 30 {
			days = n
		}
	}

	end := time.Now()
	start := end.AddDate(0, 0, -(days - 1))

	list, err := s.DAO.TokenUsageDaily.ListByRange(ctx, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 补齐缺失日期为 0，保证前端折线图连续
	byDate := make(map[string]int64, len(list))
	for _, item := range list {
		byDate[item.Date] = item.TokenCount
	}
	out := make([]dto.DailyTokenUsageResp, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, dto.DailyTokenUsageResp{Date: d, TokenCount: byDate[d]})
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, out))
}

// ====================================================================
// Memory
// ====================================================================

func (s *Service) GetShortTermMemoryConfig(ctx context.Context, c *app.RequestContext) {
	m, err := s.DAO.ShortTermMemory.GetOrCreate(ctx, c.Param("chatAreaID"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawShortTermMemory2Resp(m)))
}

func (s *Service) UpdateShortTermMemoryConfig(ctx context.Context, c *app.RequestContext) {
	chatAreaID := c.Param("chatAreaID")
	m, err := s.DAO.ShortTermMemory.GetOrCreate(ctx, chatAreaID)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	var data dto.UpdateShortTermMemoryReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	m.WindowSize = data.WindowSize
	m.AutoCompact = data.AutoCompact
	if err := s.DAO.ShortTermMemory.Update(ctx, m); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步
	if s.MemoryGroup != nil {
		s.MemoryGroup.UpdateShortTermConfig(chatAreaID, memory.ShortTermMemoryConfig{
			WindowSize:  int64(data.WindowSize),
			AutoCompact: data.AutoCompact,
		})
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawShortTermMemory2Resp(m)))
}

func (s *Service) GetLongTermMemoryConfig(ctx context.Context, c *app.RequestContext) {
	m, err := s.DAO.LongTermMemory.GetOrCreate(ctx, c.Param("chatAreaID"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawLongTermMemory2Resp(m)))
}

func (s *Service) UpdateLongTermMemoryConfig(ctx context.Context, c *app.RequestContext) {
	chatAreaID := c.Param("chatAreaID")
	m, err := s.DAO.LongTermMemory.GetOrCreate(ctx, chatAreaID)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	var data dto.UpdateLongTermMemoryReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	m.HotAreaSize = data.HotAreaSize
	m.HotMemoryTTL = data.HotMemoryTTL
	if data.GCIntervalDays > 0 {
		m.GCIntervalDays = data.GCIntervalDays
	}
	if err := s.DAO.LongTermMemory.Update(ctx, m); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步
	if s.MemoryGroup != nil {
		s.MemoryGroup.UpdateLongTermConfig(memory.LongTermMemoryConfig{
			HotAreaSize: data.HotAreaSize,
		})
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawLongTermMemory2Resp(m)))
}

// ---------- T2I ----------

func (s *Service) GetT2IConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.T2I.GetConfig(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.T2IConfigNotFound, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	healthy := false
	if s.T2IClient != nil {
		healthy = s.T2IClient.HealthCheck() == nil
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawT2IConfig2Resp(cfg, healthy)))
}

func (s *Service) UpdateT2IConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateT2IConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// allowlist 校验：SelectedStyle 仅允许空串（随机）、"random" 或风格库中定义的风格名，
	// 防止 API 调用者持久化列表外的风格名使配置脱离已定义契约。
	if !prompt.IsValidT2IStyle(strings.TrimSpace(data.SelectedStyle)) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.T2IStyleInvalid, dto.ErrorDetail{ErrorDetail: "selected_style: " + data.SelectedStyle}))
		return
	}
	data.SelectedStyle = strings.TrimSpace(data.SelectedStyle)

	cfg := &models.T2IConfig{
		ID:            1,
		BaseURL:       data.BaseURL,
		Timeout:       data.Timeout,
		IsActive:      data.IsActive,
		SelectedStyle: data.SelectedStyle,
	}
	if err := s.DAO.T2I.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 同步渲染风格选择到提示词注入（空 = 随机）
	prompt.SetSelectedT2IStyle(data.SelectedStyle)

	// 运行时同步：创建新客户端并同步到 HagoCenter
	if data.IsActive {
		client := t2iClientFactory(data.BaseURL, data.Timeout)
		s.T2IClient = client
		if s.OnUpdateT2I != nil {
			s.OnUpdateT2I(client)
		}
	} else {
		s.T2IClient = nil
		if s.OnUpdateT2I != nil {
			s.OnUpdateT2I(nil)
		}
	}

	healthy := false
	if s.T2IClient != nil {
		healthy = s.T2IClient.HealthCheck() == nil
	}

	// T2I 状态变化影响 text_to_image 工具可用性，重建 Eino Agent 自动注册/卸载
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawT2IConfig2Resp(cfg, healthy)))
}

func (s *Service) CheckT2IHealth(ctx context.Context, c *app.RequestContext) {
	if s.T2IClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": false}))
		return
	}
	err := s.T2IClient.HealthCheck()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": err == nil}))
}

// GetT2IStyles 返回风格库列表（供管理面板下拉选择），从风格库文件实时读取。
func (s *Service) GetT2IStyles(ctx context.Context, c *app.RequestContext) {
	styles := prompt.LoadT2IStyleList()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, styles))
}

// ---------- Sandbox ----------

func (s *Service) GetSandboxConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.Sandbox.GetConfig(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.SandboxConfigNotFound, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	healthy := false
	if s.SandboxClient != nil {
		healthy = s.SandboxClient.HealthCheck() == nil
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSandboxConfig2Resp(cfg, healthy)))
}

func (s *Service) UpdateSandboxConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateSandboxConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	cfg := &models.SandboxConfig{
		ID:       1,
		BaseURL:  data.BaseURL,
		APIKey:   data.APIKey,
		Timeout:  data.Timeout,
		IsActive: data.IsActive,
	}
	if err := s.DAO.Sandbox.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：创建新客户端并同步到 HagoCenter
	if data.IsActive {
		client := sandboxClientFactory(data.BaseURL, data.APIKey, data.Timeout)
		s.SandboxClient = client
		if s.OnUpdateSandbox != nil {
			s.OnUpdateSandbox(client)
		}
	} else {
		s.SandboxClient = nil
		if s.OnUpdateSandbox != nil {
			s.OnUpdateSandbox(nil)
		}
	}

	healthy := false
	if s.SandboxClient != nil {
		healthy = s.SandboxClient.HealthCheck() == nil
	}

	// Sandbox 状态变化影响 sandbox 系列工具可用性，重建 Eino Agent 自动注册/卸载
	s.notifyRebuild()

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSandboxConfig2Resp(cfg, healthy)))
}

func (s *Service) CheckSandboxHealth(ctx context.Context, c *app.RequestContext) {
	if s.SandboxClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": false}))
		return
	}
	err := s.SandboxClient.HealthCheck()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": err == nil}))
}

// ---------- RAG ----------

func (s *Service) GetRAGConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.RAG.GetConfig(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.RAGConfigNotFound, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	healthy := false
	if s.RAGClient != nil {
		healthy = s.RAGClient.HealthCheck() == nil
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawRAGConfig2Resp(cfg, healthy)))
}

func (s *Service) UpdateRAGConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateRAGConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	cfg, err := s.DAO.RAG.GetConfig(ctx)
	if err != nil {
		// 数据库无配置 → 初始化默认配置
		if initErr := s.DAO.RAG.InitConfig(ctx); initErr != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: initErr.Error()}))
			return
		}
		cfg, err = s.DAO.RAG.GetConfig(ctx)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.RAGConfigNotFound, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}

	cfg.BaseURL = data.BaseURL
	cfg.Timeout = data.Timeout
	cfg.IsActive = data.IsActive

	if err := s.DAO.RAG.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步：启用且健康检查通过则注入客户端，否则置 nil（走降级路径）
	if data.IsActive {
		client := ragClientFactory(data.BaseURL, data.Timeout)
		s.RAGClient = client
		if s.OnUpdateRAG != nil {
			s.OnUpdateRAG(client)
		}
	} else {
		s.RAGClient = nil
		if s.OnUpdateRAG != nil {
			s.OnUpdateRAG(nil)
		}
	}

	healthy := false
	if s.RAGClient != nil {
		healthy = s.RAGClient.HealthCheck() == nil
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawRAGConfig2Resp(cfg, healthy)))
}

func (s *Service) CheckRAGHealth(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": false}))
		return
	}
	err := s.RAGClient.HealthCheck()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]bool{"healthy": err == nil}))
}

// GetRAGInfo 查询 RAG-Service 运行状态（模型/内存/规模），供管理面板展示。
func (s *Service) GetRAGInfo(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{"ready": false}))
		return
	}
	info, err := s.RAGClient.Info(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{"ready": false, "error": err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, info))
}

// ---------- Webhook ----------

func (s *Service) GetWebhookConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.Webhook.GetConfig(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	running := s.WebhookAdapter != nil && s.WebhookAdapter.IsRunning()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.WebhookConfigResp{
		Addr:    cfg.Addr,
		Port:    cfg.Port,
		Token:   cfg.Token,
		Enabled: cfg.Enabled,
		Running: running,
	}))
}

func (s *Service) UpdateWebhookConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateWebhookConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	cfg := &models.WebhookConfig{
		ID:      1,
		Addr:    data.Addr,
		Port:    data.Port,
		Token:   data.Token,
		Enabled: data.Enabled,
	}
	if err := s.DAO.Webhook.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 运行时同步
	if s.WebhookAdapter != nil {
		conf := adapter.WebhookConfig{
			Addr:   data.Addr,
			Port:   data.Port,
			Token:  data.Token,
			Enable: data.Enabled,
			Admins: s.Adapter.Admins(),
		}
		if err := s.WebhookAdapter.SyncConfig(ctx, conf); err != nil {
			log.Error("webhook adapter 配置同步失败", "err", err)
		}
	}

	running := s.WebhookAdapter != nil && s.WebhookAdapter.IsRunning()
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.WebhookConfigResp{
		Addr:    cfg.Addr,
		Port:    cfg.Port,
		Token:   cfg.Token,
		Enabled: cfg.Enabled,
		Running: running,
	}))
}

// ---------- Logs ----------

// GetLogs 返回最近 250 条日志，最新的在最前。
func (s *Service) GetLogs(ctx context.Context, c *app.RequestContext) {
	if s.LogHub == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, []dto.LogEntryResp{}))
		return
	}
	entries := s.LogHub.Recent()
	// 反转为倒序: 最新写入的排最前
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawLogEntryList2Resp(entries)))
}

// StreamLogs 通过 SSE 推送实时日志。
//
// 连接建立后：
//  1. 先发送最近 250 条历史日志（按时间顺序）
//  2. 然后订阅 Hub，实时推送新日志
//  3. 客户端断开或服务停止时退出
func (s *Service) StreamLogs(ctx context.Context, c *app.RequestContext) {
	if s.LogHub == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "log hub 未初始化"}))
		return
	}

	w := sse.NewWriter(c)
	defer w.Close()

	// 1. 推送历史日志
	for _, entry := range s.LogHub.Recent() {
		if err := writeLogEvent(w, entry); err != nil {
			return
		}
	}

	// 2. 订阅新日志
	ch := s.LogHub.Subscribe()
	defer s.LogHub.Unsubscribe(ch)

	// 心跳定时器：保持代理/中间件不会因为空闲而关连接，
	// 同时让写失败能被快速检测到（客户端已断开）。
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			if err := writeLogEvent(w, entry); err != nil {
				return
			}
		case <-ticker.C:
			if err := w.WriteKeepAlive(); err != nil {
				return
			}
		}
	}
}

// writeLogEvent 把单条日志序列化为 JSON 并通过 SSE 发出。
func writeLogEvent(w *sse.Writer, entry logging.Entry) error {
	data, err := json.Marshal(dto.LogEntryResp{
		Time:    entry.Time,
		Level:   entry.Level,
		Message: entry.Message,
		Attrs:   entry.Attrs,
	})
	if err != nil {
		return err
	}
	return w.WriteEvent("", "log", data)
}

// ---------- Agent 活跃循环 ----------

// ListAgentLoops 返回当前所有活跃的 Agent ReAct 循环（监控展示）。
func (s *Service) ListAgentLoops(ctx context.Context, c *app.RequestContext) {
	loops := s.LoopTracker.List()
	resp := make([]dto.AgentLoopResp, 0, len(loops))
	for _, l := range loops {
		resp = append(resp, dto.AgentLoopResp{
			ID:          l.ID,
			ChatAreaID:  l.ChatAreaID,
			MessageType: l.MessageType,
			TargetID:    l.TargetID,
			UserID:      l.UserID,
			UserMsg:     l.UserMsg,
			CurrentTool: l.CurrentTool,
			StartedAt:   l.StartedAt,
		})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// ---------- CronJob ----------

func (s *Service) ListCronJobs(ctx context.Context, c *app.RequestContext) {
	list, err := s.DAO.CronJob.List(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawCronJobList2Resp(list)))
}

func (s *Service) GetCronJob(ctx context.Context, c *app.RequestContext) {
	raw, err := s.DAO.CronJob.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawCronJob2Resp(raw)))
}

func (s *Service) AddCronJob(ctx context.Context, c *app.RequestContext) {
	var data dto.AddCronJobReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	if data.MessageType == "" {
		data.MessageType = "private"
	}

	pluginIDsJSON, _ := json.Marshal(data.PluginIDs)
	m := models.CronJob{
		Name:        data.Name,
		CronExpr:    data.CronExpr,
		Message:     data.Message,
		MessageType: data.MessageType,
		TargetID:    data.TargetID,
		IsActive:    data.IsActive,
		PluginIDs:   string(pluginIDsJSON),
		Payload:     data.Payload,
	}
	if err := s.DAO.CronJob.Create(ctx, &m); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 同步调度器
	if s.CronJobManager != nil {
		_ = s.CronJobManager.Reload(ctx)
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawCronJob2Resp(&m)))
}

func (s *Service) UpdateCronJob(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateCronJobReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	raw, err := s.DAO.CronJob.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	if data.MessageType == "" {
		data.MessageType = "private"
	}

	raw.Name = data.Name
	raw.CronExpr = data.CronExpr
	raw.Message = data.Message
	raw.MessageType = data.MessageType
	raw.TargetID = data.TargetID
	raw.IsActive = data.IsActive
	pluginIDsJSON, _ := json.Marshal(data.PluginIDs)
	raw.PluginIDs = string(pluginIDsJSON)
	raw.Payload = data.Payload

	if err := s.DAO.CronJob.Update(ctx, raw); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 同步调度器
	if s.CronJobManager != nil {
		_ = s.CronJobManager.Reload(ctx)
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawCronJob2Resp(raw)))
}

func (s *Service) DeleteCronJob(ctx context.Context, c *app.RequestContext) {
	if err := s.DAO.CronJob.Delete(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 同步调度器
	if s.CronJobManager != nil {
		_ = s.CronJobManager.Reload(ctx)
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

func (s *Service) ToggleCronJob(ctx context.Context, c *app.RequestContext) {
	var data dto.ToggleCronJobReq
	id := c.Param("id")
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	if err := s.DAO.CronJob.SetActive(ctx, id, data.IsActive); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 同步调度器
	if s.CronJobManager != nil {
		_ = s.CronJobManager.Reload(ctx)
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ---------- 回复策略 ----------

// normalizeInt 参与窗口整数参数归一化：非法值直接报错，0=使用默认值。
func normalizeInt(v, min, max, def int) (int, bool) {
	if v < 0 {
		return 0, false
	}
	if v == 0 {
		return def, true
	}
	if v < min || v > max {
		return 0, false
	}
	return v, true
}

func (s *Service) GetReplyStrategy(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.ReplyStrategy.GetOrCreate(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ReplyStrategyResp{
		BotName:                cfg.BotName,
		StripMarkdown:          cfg.StripMarkdown,
		AgentLite:              cfg.AgentLite,
		QuietGapSeconds:        cfg.QuietGapSeconds,
		ForceCount:             cfg.ForceCount,
		MaxAgeSeconds:          cfg.MaxAgeSeconds,
		WindowMaxMsgs:          cfg.WindowMaxMsgs,
		JitterSeconds:          cfg.JitterSeconds,
		ForceCountJitter:       cfg.ForceCountJitter,
		ParticipateProbability: cfg.ParticipateProbability,
		TypingDelayMaxMs:       cfg.TypingDelayMaxMs,
	}))
}

func (s *Service) UpdateReplyStrategy(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateReplyStrategyReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 参与窗口整数参数校验（0=默认值，越界 400）
	var ok bool
	if data.QuietGapSeconds, ok = normalizeInt(data.QuietGapSeconds, 1, 30, 5); !ok {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "安静间隔必须在 1-30 秒之间（0=默认 5s）"}, nil))
		return
	}
	if data.ForceCount, ok = normalizeInt(data.ForceCount, 2, 20, 5); !ok {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "插话计数强发必须在 2-20 条之间（0=默认 5）"}, nil))
		return
	}
	if data.MaxAgeSeconds, ok = normalizeInt(data.MaxAgeSeconds, 5, 120, 20); !ok {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "最迟必发必须在 5-120 秒之间（0=默认 20s）"}, nil))
		return
	}
	if data.WindowMaxMsgs, ok = normalizeInt(data.WindowMaxMsgs, 5, 50, 20); !ok {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "窗口消息数上限必须在 5-50 条之间（0=默认 20）"}, nil))
		return
	}
	// 随机抖动字段 0=关闭（合法值，不做 0=默认），仅做范围校验
	if data.JitterSeconds < 0 || data.JitterSeconds > 10 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "随机抖动幅度必须在 0-10 秒之间"}, nil))
		return
	}
	if data.ForceCountJitter < 0 || data.ForceCountJitter > 5 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "计数抖动幅度必须在 0-5 之间"}, nil))
		return
	}
	// 参与概率 0-1（0=从不主动参与，合法值，不做 0=默认）
	if data.ParticipateProbability < 0 || data.ParticipateProbability > 1 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "参与概率必须在 0-1 之间"}, nil))
		return
	}
	if data.TypingDelayMaxMs < 0 || data.TypingDelayMaxMs > 5000 {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.Response{Status: 40032, Info: "打字延迟必须在 0-5000 毫秒之间"}, nil))
		return
	}

	cfg, err := s.DAO.ReplyStrategy.GetOrCreate(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	cfg.BotName = data.BotName
	cfg.StripMarkdown = data.StripMarkdown
	cfg.AgentLite = data.AgentLite
	cfg.QuietGapSeconds = data.QuietGapSeconds
	cfg.ForceCount = data.ForceCount
	cfg.MaxAgeSeconds = data.MaxAgeSeconds
	cfg.WindowMaxMsgs = data.WindowMaxMsgs
	cfg.JitterSeconds = data.JitterSeconds
	cfg.ForceCountJitter = data.ForceCountJitter
	cfg.ParticipateProbability = data.ParticipateProbability
	cfg.TypingDelayMaxMs = data.TypingDelayMaxMs

	if err := s.DAO.ReplyStrategy.Update(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 失效 Agent 侧内存缓存（TTL 20min 兜底，此处立即生效）
	if s.OnReplyStrategyChanged != nil {
		s.OnReplyStrategyChanged()
	}

	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ReplyStrategyResp{
		BotName:                cfg.BotName,
		StripMarkdown:          cfg.StripMarkdown,
		AgentLite:              cfg.AgentLite,
		QuietGapSeconds:        cfg.QuietGapSeconds,
		ForceCount:             cfg.ForceCount,
		MaxAgeSeconds:          cfg.MaxAgeSeconds,
		WindowMaxMsgs:          cfg.WindowMaxMsgs,
		JitterSeconds:          cfg.JitterSeconds,
		ForceCountJitter:       cfg.ForceCountJitter,
		ParticipateProbability: cfg.ParticipateProbability,
		TypingDelayMaxMs:       cfg.TypingDelayMaxMs,
	}))
}

// ---------- 知识库 ----------

// ListKnowledge 分页列出知识库条目。
func (s *Service) ListKnowledge(ctx context.Context, c *app.RequestContext) {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	list, err := s.DAO.Knowledge.List(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	total, _ := s.DAO.Knowledge.Count(ctx)
	resp := make([]dto.KnowledgeResp, 0, len(list))
	for i := range list {
		resp = append(resp, dto.RawKnowledge2Resp(&list[i]))
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{"total": total, "list": resp}))
}

// GetKnowledge 知识库条目详情。
func (s *Service) GetKnowledge(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Knowledge.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawKnowledge2Resp(item)))
}

// AddKnowledge 新增知识库条目（触发异步关键词提取）。
func (s *Service) AddKnowledge(ctx context.Context, c *app.RequestContext) {
	var data dto.AddKnowledgeReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.KnowledgeContentEmpty, nil))
		return
	}

	item := &models.KnowledgeItem{
		ID:            newUUID(),
		Title:         data.Title,
		Content:       data.Content,
		KeywordStatus: models.KeywordStatusPending,
	}
	if err := s.DAO.Knowledge.Create(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 双写：同步向量到 RAG-Service（未配置时静默跳过，不报错）
	s.syncKnowledgeVector(item.ID, item.Content)

	// 异步提取关键词 + 失效 LRU
	if s.OnExtractKnowledge != nil {
		s.OnExtractKnowledge(item.ID)
	}
	if s.OnKnowledgeChanged != nil {
		s.OnKnowledgeChanged()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawKnowledge2Resp(item)))
}

// UpdateKnowledge 编辑知识库条目（重新触发关键词提取）。
func (s *Service) UpdateKnowledge(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateKnowledgeReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.KnowledgeContentEmpty, nil))
		return
	}

	item, err := s.DAO.Knowledge.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	item.Title = data.Title
	item.Content = data.Content
	item.KeywordStatus = models.KeywordStatusPending
	if err := s.DAO.Knowledge.Update(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	// 双写：内容变更后同步覆写向量（Upsert 幂等；未配置时静默跳过）
	s.syncKnowledgeVector(item.ID, item.Content)

	if s.OnExtractKnowledge != nil {
		s.OnExtractKnowledge(item.ID)
	}
	if s.OnKnowledgeChanged != nil {
		s.OnKnowledgeChanged()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawKnowledge2Resp(item)))
}

// DeleteKnowledge 删除知识库条目（双删：Postgres + RAG 向量）。
func (s *Service) DeleteKnowledge(ctx context.Context, c *app.RequestContext) {
	if err := s.DAO.Knowledge.Delete(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	// 双删：同步删除 RAG 向量（未配置时静默跳过；失败仅告警，残留无害）
	s.deleteKnowledgeVector(c.Param("id"))
	if s.OnKnowledgeChanged != nil {
		s.OnKnowledgeChanged()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ReExtractKnowledge 手动重试关键词提取（failed 状态时用）。
func (s *Service) ReExtractKnowledge(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Knowledge.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.DAO.Knowledge.SetKeywordStatus(ctx, item.ID, models.KeywordStatusPending); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnExtractKnowledge != nil {
		s.OnExtractKnowledge(item.ID)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ---------- RAG 向量同步 ----------

// syncKnowledgeVector 把知识条目内容同步写入 RAG 向量库（双写）。
// RAG 未配置 → 直接返回（不报错）；写入失败仅告警，由手动同步按钮兜底。
func (s *Service) syncKnowledgeVector(itemID, content string) {
	if s.RAGClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.RAGClient.Upsert(ctx, ragtag.ScoopKnowledge, ragtag.Knowledge(itemID), content); err != nil {
		log.Warn("知识向量同步失败（可在知识库页手动同步）", "id", itemID, "err", err)
	}
}

// deleteKnowledgeVector 删除知识条目的 RAG 向量（双删）。
// RAG 未配置 → 直接返回；删除失败仅告警（残留向量无害）。
func (s *Service) deleteKnowledgeVector(itemID string) {
	if s.RAGClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.RAGClient.Delete(ctx, ragtag.ScoopKnowledge, ragtag.Knowledge(itemID)); err != nil {
		log.Warn("知识向量删除失败", "id", itemID, "err", err)
	}
}

// SyncKnowledgeVector 手动全量同步知识库到 RAG 向量库（页面按钮触发）：
// 全部条目按 50 条一批 BatchUpsert（一次嵌入一次发布）；
// RAG 未启用直接返回提示，不报错。
func (s *Service) SyncKnowledgeVector(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
			"ready": false, "synced": 0, "failed": 0, "total": 0,
			"message": "RAG 未启用，无法同步向量库",
		}))
		return
	}
	items, err := s.DAO.Knowledge.ListAllContent(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	const batchSize = 50
	synced, failed := 0, 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := make([]ragcaller.BatchItem, 0, end-i)
		for _, it := range items[i:end] {
			batch = append(batch, ragcaller.BatchItem{Tag: ragtag.Knowledge(it.ID), Text: it.Content})
		}
		resp, err := s.RAGClient.BatchUpsert(ctx, ragtag.ScoopKnowledge, batch)
		if err != nil {
			log.Warn("知识向量同步批次失败", "batch", i/batchSize+1, "err", err)
			failed += len(batch)
			continue
		}
		for _, r := range resp.Results {
			if r.Error != nil {
				failed++
			} else {
				synced++
			}
		}
	}
	log.Info("知识库向量同步完成", "total", len(items), "synced", synced, "failed", failed)
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
		"ready": true, "synced": synced, "failed": failed, "total": len(items),
	}))
}

// SyncKnowledgeVectorStream 全量同步知识库向量（SSE 流式）：每批推送进度，
// 知识条目量大时避免单次 HTTP 请求超时。GET /knowledge/vector-sync/stream
func (s *Service) SyncKnowledgeVectorStream(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
			"ready": false, "message": "RAG 未启用，无法同步向量库",
		}))
		return
	}
	items, err := s.DAO.Knowledge.ListAllContent(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	w := sse.NewWriter(c)
	defer func() { _ = w.Close() }()
	push := func(event string, data any) bool {
		b, err := json.Marshal(data)
		if err != nil {
			log.Warn("SSE 序列化失败", "event", event, "err", err)
			return false
		}
		return w.WriteEvent("", event, b) == nil
	}

	push("start", map[string]string{"status": "syncing"})

	const batchSize = 50
	synced, failed := 0, 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := make([]ragcaller.BatchItem, 0, end-i)
		for _, it := range items[i:end] {
			batch = append(batch, ragcaller.BatchItem{Tag: ragtag.Knowledge(it.ID), Text: it.Content})
		}
		resp, err := s.RAGClient.BatchUpsert(ctx, ragtag.ScoopKnowledge, batch)
		if err != nil {
			log.Warn("知识向量同步批次失败", "batch", i/batchSize+1, "err", err)
			failed += len(batch)
		} else {
			for _, r := range resp.Results {
				if r.Error != nil {
					failed++
				} else {
					synced++
				}
			}
		}
		// 每批推送进度；客户端断开即中止
		if !push("progress", map[string]int{"done": synced, "failed": failed}) || ctx.Err() != nil {
			return
		}
	}
	log.Info("知识库向量同步完成", "total", len(items), "synced", synced, "failed", failed)
	push("done", map[string]int{"ready": 1, "total": len(items), "synced": synced, "failed": failed})
}

// SyncMemoryRAG 手动全量同步长期记忆到 RAG 向量库（Memory 页按钮触发）：
// 全部 LongTermMemItem 按 50 条一批 BatchUpsert（幂等），补齐 Compact 双写之前
// 的历史记忆。RAG 未启用返回明确提示（不静默）。
func (s *Service) SyncMemoryRAG(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
			"ready": false, "synced": 0, "failed": 0, "total": 0,
			"message": "RAG 未启用，无法同步记忆向量",
		}))
		return
	}
	ids, err := s.DAO.LongTermMemItem.ListAllIDs(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	items, err := s.DAO.LongTermMemItem.GetByIDs(ctx, ids)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}

	const batchSize = 50
	synced, failed := 0, 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := make([]ragcaller.BatchItem, 0, end-i)
		for _, it := range items[i:end] {
			batch = append(batch, ragcaller.BatchItem{Tag: ragtag.Memory(it.ID), Text: it.Content})
		}
		resp, err := s.RAGClient.BatchUpsert(ctx, ragtag.ScoopMemory, batch)
		if err != nil {
			log.Warn("记忆向量同步批次失败", "batch", i/batchSize+1, "err", err)
			failed += len(batch)
			continue
		}
		for _, r := range resp.Results {
			if r.Error != nil {
				failed++
			} else {
				synced++
			}
		}
	}
	log.Info("长期记忆向量同步完成", "total", len(items), "synced", synced, "failed", failed)
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
		"ready": true, "synced": synced, "failed": failed, "total": len(items),
	}))
}

// SyncMemoryRAGStream 全量同步长期记忆向量（SSE 流式）：每批推送进度，
// 记忆量大时避免单次 HTTP 请求超时。GET /memory/sync-rag/stream
func (s *Service) SyncMemoryRAGStream(ctx context.Context, c *app.RequestContext) {
	if s.RAGClient == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, map[string]any{
			"ready": false, "message": "RAG 未启用，无法同步记忆向量",
		}))
		return
	}
	ids, err := s.DAO.LongTermMemItem.ListAllIDs(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	items, err := s.DAO.LongTermMemItem.GetByIDs(ctx, ids)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	w := sse.NewWriter(c)
	defer func() { _ = w.Close() }()
	push := func(event string, data any) bool {
		b, err := json.Marshal(data)
		if err != nil {
			log.Warn("SSE 序列化失败", "event", event, "err", err)
			return false
		}
		return w.WriteEvent("", event, b) == nil
	}

	push("start", map[string]string{"status": "syncing"})

	const batchSize = 50
	synced, failed := 0, 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := make([]ragcaller.BatchItem, 0, end-i)
		for _, it := range items[i:end] {
			batch = append(batch, ragcaller.BatchItem{Tag: ragtag.Memory(it.ID), Text: it.Content})
		}
		resp, err := s.RAGClient.BatchUpsert(ctx, ragtag.ScoopMemory, batch)
		if err != nil {
			log.Warn("记忆向量同步批次失败", "batch", i/batchSize+1, "err", err)
			failed += len(batch)
		} else {
			for _, r := range resp.Results {
				if r.Error != nil {
					failed++
				} else {
					synced++
				}
			}
		}
		// 每批推送进度；客户端断开即中止
		if !push("progress", map[string]int{"done": synced, "failed": failed}) || ctx.Err() != nil {
			return
		}
	}
	log.Info("长期记忆向量同步完成", "total", len(items), "synced", synced, "failed", failed)
	push("done", map[string]int{"ready": 1, "total": len(items), "synced": synced, "failed": failed})
}

// ---------- 图床 ----------

// maxImageSize 上传图片大小上限：1.5MB。
const maxImageSize = 1536 * 1024

// allowedImageMimes 允许的图片 MIME 白名单。
var allowedImageMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// normalizeImageFolder 归一化虚拟文件夹路径（空 → 根 /）。
func normalizeImageFolder(f string) string {
	if strings.TrimSpace(f) == "" || strings.TrimSpace(f) == "/" {
		return "/"
	}
	f = "/" + strings.Trim(strings.TrimSpace(f), "/")
	return f
}

// ListImages 按虚拟文件夹分页列出图床图片。
func (s *Service) ListImages(ctx context.Context, c *app.RequestContext) {
	folder := normalizeImageFolder(c.Query("folder"))
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 48
	}
	list, err := s.DAO.Image.List(ctx, folder, pageSize, (page-1)*pageSize)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	total, _ := s.DAO.Image.Count(ctx, folder)
	resp := make([]dto.ImageResp, 0, len(list))
	for i := range list {
		resp = append(resp, dto.RawImage2Resp(&list[i]))
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ImageListResp{Total: total, List: resp}))
}

// GetImage 获取单张图片元数据。
func (s *Service) GetImage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Image.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageNotExist, nil))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawImage2Resp(item)))
}

// GetImageFile 返回图片文件字节流（Web 预览用）。
func (s *Service) GetImageFile(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Image.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageNotExist, nil))
		return
	}
	if s.ImageStore == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "图床存储未初始化"}))
		return
	}
	data, err := s.ImageStore.Read(item.ID)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "图片文件读取失败"}))
		return
	}
	c.Data(consts.StatusOK, item.MimeType, data)
}

// UploadImage 上传图片到图床（multipart：file + name + folder）。
// 校验：大小 ≤ 1.5MB、MIME 白名单（jpg/png/gif/webp）。
func (s *Service) UploadImage(ctx context.Context, c *app.RequestContext) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.EmptyFileToUpload, nil))
		return
	}
	if file.Size > maxImageSize {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageTooLarge,
			dto.ErrorDetail{ErrorDetail: fmt.Sprintf("当前 %.1fMB", float64(file.Size)/1024/1024)}))
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "无法打开文件"}))
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "文件读取失败"}))
		return
	}

	// MIME 校验：以文件内容嗅探为准，不信任文件名/Content-Type
	mime := http.DetectContentType(data)
	if !allowedImageMimes[mime] {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageTypeNotAllowed, dto.ErrorDetail{ErrorDetail: mime}))
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = filepath.Base(file.Filename)
		if name == "" || name == "." {
			name = "未命名图片"
		}
	}
	folder := normalizeImageFolder(c.PostForm("folder"))

	// 目标文件夹必须存在（根 / 除外）
	if folder != "/" {
		folders, _ := s.DAO.Image.FolderList(ctx)
		found := false
		for _, f := range folders {
			if "/"+f.Name == folder {
				found = true
				break
			}
		}
		if !found {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageFolderNotExist, nil))
			return
		}
	}

	id := newUUID()
	item := &models.ImageAsset{
		ID:        id,
		Name:      name,
		FileName:  id + ".img",
		Folder:    folder,
		MimeType:  mime,
		SizeBytes: int64(len(data)),
	}
	if s.ImageStore == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "图床存储未初始化"}))
		return
	}
	if err := s.ImageStore.Save(id, data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "图片保存失败"}))
		return
	}
	if err := s.DAO.Image.Create(ctx, item); err != nil {
		_ = s.ImageStore.Delete(id) // 回滚文件
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawImage2Resp(item)))
}

// UpdateImage 编辑图床图片（重命名 / 移动虚拟文件夹）。
func (s *Service) UpdateImage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Image.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageNotExist, nil))
		return
	}
	var data dto.UpdateImageReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if data.Name != "" {
		item.Name = strings.TrimSpace(data.Name)
	}
	if data.Folder != "" {
		folder := normalizeImageFolder(data.Folder)
		if folder != "/" {
			folders, _ := s.DAO.Image.FolderList(ctx)
			found := false
			for _, f := range folders {
				if "/"+f.Name == folder {
					found = true
					break
				}
			}
			if !found {
				c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageFolderNotExist, nil))
				return
			}
		}
		item.Folder = folder
	}
	if err := s.DAO.Image.Update(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawImage2Resp(item)))
}

// DeleteImage 删除图床图片（DB 软删 + 删除磁盘文件）。
func (s *Service) DeleteImage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Image.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageNotExist, nil))
		return
	}
	if err := s.DAO.Image.Delete(ctx, item.ID); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.ImageStore != nil {
		_ = s.ImageStore.Delete(item.ID)
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ListImageFolders 列出图床虚拟文件夹。
func (s *Service) ListImageFolders(ctx context.Context, c *app.RequestContext) {
	folders, err := s.DAO.Image.FolderList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.ImageFolderResp, 0, len(folders))
	for i := range folders {
		resp = append(resp, dto.ImageFolderResp{ID: folders[i].ID, Name: folders[i].Name, CreatedAt: folders[i].CreatedAt})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// CreateImageFolder 创建图床虚拟文件夹（仅一层，name 不能含 /）。
func (s *Service) CreateImageFolder(ctx context.Context, c *app.RequestContext) {
	var data dto.CreateImageFolderReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	name := strings.TrimSpace(data.Name)
	if name == "" || strings.Contains(name, "/") {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "文件夹名不能为空且不能包含 /"}))
		return
	}
	folders, _ := s.DAO.Image.FolderList(ctx)
	for _, f := range folders {
		if f.Name == name {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageFolderExist, nil))
			return
		}
	}
	f, err := s.DAO.Image.FolderCreate(ctx, name)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ImageFolderResp{ID: f.ID, Name: f.Name, CreatedAt: f.CreatedAt}))
}

// DeleteImageFolder 删除图床虚拟文件夹（其下图片自动移到根 /）。
func (s *Service) DeleteImageFolder(ctx context.Context, c *app.RequestContext) {
	f, err := s.DAO.Image.FolderGetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageFolderNotExist, nil))
		return
	}
	if err := s.DAO.Image.MoveFolderToRoot(ctx, "/"+f.Name); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.DAO.Image.FolderDelete(ctx, f.ID); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ---------- 表情包库 ----------

// ListStickers 分页列出表情，支持标签过滤（tag）与名称/简介模糊匹配（keyword）。
func (s *Service) ListStickers(ctx context.Context, c *app.RequestContext) {
	tag := strings.TrimSpace(c.Query("tag"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 24
	}
	list, err := s.DAO.Sticker.List(ctx, tag, keyword, pageSize, (page-1)*pageSize)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	total, _ := s.DAO.Sticker.Count(ctx, tag, keyword)
	resp := make([]dto.StickerResp, 0, len(list))
	for i := range list {
		resp = append(resp, dto.RawSticker2Resp(&list[i]))
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.StickerListResp{Total: total, List: resp}))
}

// GetSticker 获取表情详情。
func (s *Service) GetSticker(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Sticker.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerNotExist, nil))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSticker2Resp(item)))
}

// CreateSticker 新建表情（引用图床图片，需校验图片存在且未被其他表情引用）。
func (s *Service) CreateSticker(ctx context.Context, c *app.RequestContext) {
	var data dto.CreateStickerReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	data.Name = strings.TrimSpace(data.Name)
	data.ImageID = strings.TrimSpace(data.ImageID)
	if data.Name == "" || data.ImageID == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "名称与图床图片均不能为空"}))
		return
	}
	// 图床图片必须存在
	if _, err := s.DAO.Image.GetByID(ctx, data.ImageID); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ImageNotExist, nil))
		return
	}
	// 同一张图不能重复引用
	if _, err := s.DAO.Sticker.GetByImageID(ctx, data.ImageID); err == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerImageExist, nil))
		return
	}
	item := &models.Sticker{
		ImageID: data.ImageID,
		Name:    data.Name,
		Desc:    strings.TrimSpace(data.Desc),
	}
	tags, err := s.validateStickerTags(ctx, data.Tags)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerTagNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	item.Tags = models.JSONSlice(tags)
	if err := s.DAO.Sticker.Create(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSticker2Resp(item)))
}

// UpdateSticker 编辑表情（名称/简介/标签）。
func (s *Service) UpdateSticker(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Sticker.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerNotExist, nil))
		return
	}
	var data dto.UpdateStickerReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if strings.TrimSpace(data.Name) != "" {
		item.Name = strings.TrimSpace(data.Name)
	}
	if data.Desc != "" {
		item.Desc = strings.TrimSpace(data.Desc)
	}
	tags, err := s.validateStickerTags(ctx, data.Tags)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerTagNotExist, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	item.Tags = models.JSONSlice(tags)
	if err := s.DAO.Sticker.Update(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawSticker2Resp(item)))
}

// DeleteSticker 删除表情（软删）。
func (s *Service) DeleteSticker(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.Sticker.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerNotExist, nil))
		return
	}
	if err := s.DAO.Sticker.Delete(ctx, item.ID); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ListStickerTags 列出全部表情标签。
func (s *Service) ListStickerTags(ctx context.Context, c *app.RequestContext) {
	tags, err := s.DAO.Sticker.TagList(ctx)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.StickerTagResp, 0, len(tags))
	for i := range tags {
		resp = append(resp, dto.StickerTagResp{ID: tags[i].ID, Name: tags[i].Name, CreatedAt: tags[i].CreatedAt})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// CreateStickerTag 创建表情标签（重名返回 40040）。
func (s *Service) CreateStickerTag(ctx context.Context, c *app.RequestContext) {
	var data dto.CreateStickerTagReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	name := strings.TrimSpace(data.Name)
	if name == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "标签名不能为空"}))
		return
	}
	tags, _ := s.DAO.Sticker.TagList(ctx)
	for _, t := range tags {
		if t.Name == name {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerTagExist, nil))
			return
		}
	}
	t, err := s.DAO.Sticker.TagCreate(ctx, name)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.StickerTagResp{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt}))
}

// DeleteStickerTag 删除标签（所有表情中的该标签一并移除）。
// 系统内置「常用」标签不可删除（每轮对话注入依赖它）。
func (s *Service) DeleteStickerTag(ctx context.Context, c *app.RequestContext) {
	t, err := s.DAO.Sticker.TagGetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerTagNotExist, nil))
		return
	}
	if t.Name == dao.CommonStickerTag {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.StickerTagSystem, dto.ErrorDetail{ErrorDetail: "系统内置标签「" + dao.CommonStickerTag + "」不可删除"}))
		return
	}
	if err := s.DAO.Sticker.RemoveTagFromAll(ctx, t.Name); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if err := s.DAO.Sticker.TagDelete(ctx, t.ID); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ---------- 摸鱼人日历 ----------

// GetFishCalendarConfig 读取摸鱼日历配置（未初始化则写入默认配置）。
func (s *Service) GetFishCalendarConfig(ctx context.Context, c *app.RequestContext) {
	cfg, err := s.DAO.FishCalendar.GetConfig(ctx)
	if err != nil {
		if initErr := s.DAO.FishCalendar.InitConfig(ctx); initErr != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.FishCalConfigNotExist, dto.ErrorDetail{ErrorDetail: initErr.Error()}))
			return
		}
		cfg, err = s.DAO.FishCalendar.GetConfig(ctx)
		if err != nil {
			c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
			return
		}
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.FishCalendarConfigResp{
		Enabled:      cfg.Enabled,
		CronExpr:     cfg.CronExpr,
		TargetGroups: cfg.TargetGroups,
		LastRunAt:    cfg.LastRunAt,
		LastError:    cfg.LastError,
	}))
}

// UpdateFishCalendarConfig 更新摸鱼日历配置并重新调度。
func (s *Service) UpdateFishCalendarConfig(ctx context.Context, c *app.RequestContext) {
	var data dto.UpdateFishCalendarConfigReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	cfg, err := s.DAO.FishCalendar.GetConfig(ctx)
	if err != nil {
		_ = s.DAO.FishCalendar.InitConfig(ctx)
		cfg, _ = s.DAO.FishCalendar.GetConfig(ctx)
	}
	if cfg == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.FishCalConfigNotExist, nil))
		return
	}
	if strings.TrimSpace(data.CronExpr) != "" {
		cfg.CronExpr = strings.TrimSpace(data.CronExpr)
	}
	cfg.Enabled = data.Enabled
	if len(data.TargetGroups) > 0 {
		cfg.TargetGroups = models.JSONSlice(cleanStickerTags(data.TargetGroups))
	}
	if err := s.DAO.FishCalendar.UpdateConfig(ctx, cfg); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnFishCalReload != nil {
		s.OnFishCalReload()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// TriggerFishCalendar 手动触发摸鱼日历立即生成并发送。
func (s *Service) TriggerFishCalendar(ctx context.Context, c *app.RequestContext) {
	if s.OnFishCalTrigger == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "摸鱼日历未初始化"}))
		return
	}
	if err := s.OnFishCalTrigger(ctx); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ListFishCalendarAffairs 列出某月（?month=YYYY-MM）已配置的群务。
func (s *Service) ListFishCalendarAffairs(ctx context.Context, c *app.RequestContext) {
	month := strings.TrimSpace(c.Query("month"))
	if !regexpFishCalMonth.MatchString(month) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "month 格式应为 YYYY-MM"}))
		return
	}
	list, err := s.DAO.FishCalendar.AffairListMonth(ctx, month)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	resp := make([]dto.FishCalendarAffairResp, 0, len(list))
	for i := range list {
		resp = append(resp, dto.FishCalendarAffairResp{Date: list[i].Date, Content: list[i].Content})
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}

// SetFishCalendarAffair 设置某天群务（content 为空则清除当天配置）。
func (s *Service) SetFishCalendarAffair(ctx context.Context, c *app.RequestContext) {
	var data dto.SetFishCalendarAffairReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	data.Date = strings.TrimSpace(data.Date)
	if !regexpFishCalDate.MatchString(data.Date) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "date 格式应为 YYYY-MM-DD"}))
		return
	}
	if err := s.DAO.FishCalendar.AffairUpsert(ctx, data.Date, data.Content); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ---------- 定时消息 ----------

// ListScheduledMessages 分页列出定时消息任务。
func (s *Service) ListScheduledMessages(ctx context.Context, c *app.RequestContext) {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	list, err := s.DAO.ScheduledMsg.List(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	total, _ := s.DAO.ScheduledMsg.Count(ctx)
	resp := make([]dto.ScheduledMessageResp, 0, len(list))
	for i := range list {
		resp = append(resp, dto.RawScheduledMsg2Resp(&list[i]))
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ScheduledMessageListResp{Total: total, List: resp}))
}

// GetScheduledMessage 定时消息任务详情。
func (s *Service) GetScheduledMessage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.ScheduledMsg.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ScheduledMsgNotExist, nil))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawScheduledMsg2Resp(item)))
}

// AddScheduledMessage 新建定时消息任务。
func (s *Service) AddScheduledMessage(ctx context.Context, c *app.RequestContext) {
	var data dto.AddScheduledMessageReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	segs, err := normalizeScheduledBlocks(data.Blocks)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if strings.TrimSpace(data.Name) == "" {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: "任务名不能为空"}))
		return
	}
	item := &models.ScheduledMessage{
		Name:       strings.TrimSpace(data.Name),
		Enabled:    data.Enabled,
		CronExpr:   strings.TrimSpace(data.CronExpr),
		TargetType: data.TargetType,
		TargetID:   data.TargetID,
		Blocks:     segs,
	}
	if item.TargetType == "" {
		item.TargetType = "group"
	}
	if err := s.DAO.ScheduledMsg.Create(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnSchedMsgReload != nil {
		s.OnSchedMsgReload()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawScheduledMsg2Resp(item)))
}

// UpdateScheduledMessage 编辑定时消息任务。
func (s *Service) UpdateScheduledMessage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.ScheduledMsg.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ScheduledMsgNotExist, nil))
		return
	}
	var data dto.UpdateScheduledMessageReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	segs, err := normalizeScheduledBlocks(data.Blocks)
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if strings.TrimSpace(data.Name) != "" {
		item.Name = strings.TrimSpace(data.Name)
	}
	item.Enabled = data.Enabled
	if strings.TrimSpace(data.CronExpr) != "" {
		item.CronExpr = strings.TrimSpace(data.CronExpr)
	}
	if data.TargetType != "" {
		item.TargetType = data.TargetType
	}
	if data.TargetID != 0 {
		item.TargetID = data.TargetID
	}
	item.Blocks = segs
	if err := s.DAO.ScheduledMsg.Update(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnSchedMsgReload != nil {
		s.OnSchedMsgReload()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawScheduledMsg2Resp(item)))
}

// DeleteScheduledMessage 删除定时消息任务。
func (s *Service) DeleteScheduledMessage(ctx context.Context, c *app.RequestContext) {
	if _, err := s.DAO.ScheduledMsg.GetByID(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ScheduledMsgNotExist, nil))
		return
	}
	if err := s.DAO.ScheduledMsg.Delete(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnSchedMsgReload != nil {
		s.OnSchedMsgReload()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// ToggleScheduledMessage 启停定时消息任务。
func (s *Service) ToggleScheduledMessage(ctx context.Context, c *app.RequestContext) {
	item, err := s.DAO.ScheduledMsg.GetByID(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ScheduledMsgNotExist, nil))
		return
	}
	var data struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	item.Enabled = data.Enabled
	if err := s.DAO.ScheduledMsg.Update(ctx, item); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	if s.OnSchedMsgReload != nil {
		s.OnSchedMsgReload()
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.RawScheduledMsg2Resp(item)))
}

// TriggerScheduledMessage 手动触发定时消息任务立即执行。
func (s *Service) TriggerScheduledMessage(ctx context.Context, c *app.RequestContext) {
	if s.OnSchedMsgTrigger == nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: "定时消息未初始化"}))
		return
	}
	if err := s.OnSchedMsgTrigger(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.ServerInternalErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, nil))
}

// normalizeScheduledSegments 校验并归一化消息块内的段（text / image / face）。
func normalizeScheduledSegments(segs []dto.ScheduledSegmentReq) (models.ScheduledSegments, error) {
	if len(segs) == 0 {
		return nil, errors.New("消息块至少需要一个段")
	}
	out := make(models.ScheduledSegments, 0, len(segs))
	for _, s := range segs {
		s.Type = strings.TrimSpace(s.Type)
		s.Source = strings.TrimSpace(s.Source)
		s.Content = strings.TrimSpace(s.Content)
		if s.Content == "" {
			return nil, fmt.Errorf("消息段内容不能为空")
		}
		switch s.Type {
		case "text", "face":
			// 内容即文本/CQ 码
		case "image":
			switch s.Source {
			case "url", "t2i", "imgstore":
			default:
				return nil, fmt.Errorf("图片段 source 必须是 t2i / url / imgstore")
			}
		default:
			return nil, fmt.Errorf("消息段 type 必须是 text / image / face")
		}
		out = append(out, models.ScheduledMessageSegment{Type: s.Type, Source: s.Source, Content: s.Content})
	}
	return out, nil
}

// normalizeScheduledBlocks 校验并归一化编排块链（message / delay）。
func normalizeScheduledBlocks(blocks []dto.ScheduledBlockReq) (models.ScheduledBlocks, error) {
	if len(blocks) == 0 {
		return nil, errors.New("至少需要一个编排块")
	}
	out := make(models.ScheduledBlocks, 0, len(blocks))
	for i, b := range blocks {
		switch strings.TrimSpace(b.Type) {
		case "message":
			segs, err := normalizeScheduledSegments(b.Segments)
			if err != nil {
				return nil, fmt.Errorf("第 %d 块（消息）: %w", i+1, err)
			}
			out = append(out, models.ScheduledBlock{Type: "message", Segments: segs})
		case "delay":
			if b.DelaySeconds <= 0 || b.DelaySeconds > 3600 {
				return nil, fmt.Errorf("第 %d 块（延时）: 延迟必须在 1~3600 秒之间", i+1)
			}
			out = append(out, models.ScheduledBlock{Type: "delay", DelaySeconds: b.DelaySeconds})
		default:
			return nil, fmt.Errorf("第 %d 块类型必须是 message / delay", i+1)
		}
	}
	return out, nil
}

// cleanStickerTags 清洗表情标签：去空白、去重。
// regexpFishCalMonth 月份格式 YYYY-MM。
var regexpFishCalMonth = regexp.MustCompile(`^\d{4}-\d{2}$`)

// regexpFishCalDate 日期格式 YYYY-MM-DD。
var regexpFishCalDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// validateStickerTags 校验标签均已提前创建（标签先建后选），返回去重清洗后的标签。
func (s *Service) validateStickerTags(ctx context.Context, tags []string) ([]string, error) {
	cleaned := cleanStickerTags(tags)
	if len(cleaned) == 0 {
		return cleaned, nil
	}
	registered := make(map[string]struct{})
	existing, err := s.DAO.Sticker.TagList(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range existing {
		registered[t.Name] = struct{}{}
	}
	for _, t := range cleaned {
		if _, ok := registered[t]; !ok {
			return nil, fmt.Errorf("标签尚未创建: %s", t)
		}
	}
	return cleaned, nil
}

// cleanStickerTags 清洗表情标签：去空白、去重。
func cleanStickerTags(tags []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// ---------- helpers ----------

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func ensureNonNilMap(m models.JSONMap) models.JSONMap {
	if m == nil {
		return make(models.JSONMap)
	}
	return m
}

func ensureNonNilSlice(s models.JSONSlice) models.JSONSlice {
	if s == nil {
		return make(models.JSONSlice, 0)
	}
	return s
}

// buildMcpSSEConfig 从 DB model 构建 MCP SSE 客户端配置。
func buildMcpSSEConfig(m *models.MCPServer) mcp.McpSSEConfig {
	headers := make(map[string]string)
	for k, v := range m.Headers {
		if str, ok := v.(string); ok {
			headers[k] = str
		}
	}
	return mcp.McpSSEConfig{
		ID:            m.ID,
		Name:          m.Name,
		ServerURL:     m.ServerURL,
		Headers:       headers,
		Timeout:       0,
		RetryCount:    m.RetryCount,
		ToolFilter:    m.ToolFilter,
		AutoReconnect: m.AutoReconnect,
	}
}

// notifyRebuild 通知 HagoCenter 重建 Eino Agent（MCP 变更后同步工具列表）。
func (s *Service) notifyRebuild() {
	if s.OnRebuildAgent != nil {
		s.OnRebuildAgent()
	}
}

// buildSkillConfig 从 DB model 构建 Skill 运行时配置。
func buildSkillConfig(s *models.Skill) *skill.SkillConfig {
	return &skill.SkillConfig{
		ID:           s.ID,
		Name:         s.Name,
		Description:  s.Description,
		Keywords:     s.Keywords,
		RegexPattern: s.RegexPattern,
		PromptRefs:   s.PromptRefs,
		ToolRefs:     s.ToolRefs,
		McpRefs:      s.McpRefs,
		IsActive:     s.IsActive,
		IsSystem:     s.IsSystem,
		Priority:     s.Priority,
	}
}
