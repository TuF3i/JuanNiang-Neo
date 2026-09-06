import client from './client'

// ======== Types ========
export interface LoginReq { username: string; password: string }
export interface ChangePasswordReq { old_password: string; new_password: string }
export interface TokenResp { token: string }

export interface AdapterConnDetail { id: number; ip: string; self_id: number }
export interface AdapterStatus { running: boolean; listen_addr: string; self_id: number; conn_count: number; conn_ids: number[]; conns: AdapterConnDetail[] }
export interface UpdateAdapterConfigReq { addr: string; port: number; token: string; admin_qq_numbers: string[]; enabled: boolean }

export interface ProviderResp { id: string; created_at: string; name: string; type: string; endpoint: string; token: string; model: string; temperature: number; is_active: boolean; enable_thinking: boolean; api_mode: string; thinking_effort: string; thinking_budget: number; max_tokens: number; top_p: number | null; top_k: number | null; frequency_penalty: number | null; presence_penalty: number | null; repetition_penalty: number | null; provider_key: string; auth_header: string; url_mode: string }
export interface ProviderPresetProtocol { api_mode: string; base_url: string; auth_header: string; note?: string }
export interface ProviderPreset { key: string; name: string; protocols: ProviderPresetProtocol[] }
export interface AddProviderReq { name: string; type: string; endpoint: string; token: string; model: string; temperature?: number; isActive: boolean; enable_thinking: boolean; api_mode: string; thinking_effort: string; thinking_budget: number; max_tokens: number; top_p: number | null; top_k: number | null; frequency_penalty: number | null; presence_penalty: number | null; repetition_penalty: number | null; provider_key: string; auth_header: string; url_mode: string }

export interface MCPServerResp { id: string; name: string; server_url: string; headers: Record<string, any>; timeout: number; retry_count: number; tool_filter: string[]; auto_reconnect: boolean; is_active: boolean; created_at: string }
export interface AddMCPServerReq { name: string; server_url: string; headers?: Record<string, any>; timeout?: number; retry_count?: number; tool_filter?: string[]; auto_reconnect?: boolean; is_active: boolean }

export interface ShortTermMemoryResp { id: string; chat_area_id: string; window_size: number; auto_compact: boolean; created_at: string }
export interface LongTermMemoryResp { id: string; chat_area_id: string; hot_area_size: number; hot_memory_ttl: number; gc_interval_days: number; created_at: string }

export interface PromptResp { id: string; name: string; content: string; type: string; is_active: boolean; is_system: boolean; created_at: string }
export interface AddPromptReq { name: string; content: string; type: string; is_active: boolean }

export interface SessionResp { id: string; chat_area_id: string; model: string; token_usage: number; meta_data: Record<string, any>; created_at: string }

export interface SkillResp { id: string; name: string; description: string; keywords: string[]; regex_pattern: string; prompt_refs: string[]; tool_refs: string[]; mcp_refs: string[]; is_active: boolean; is_system: boolean; priority: number; created_at: string }
export interface AddSkillReq { name: string; description?: string; keywords?: string[]; regex_pattern?: string; prompt_refs?: string[]; tool_refs?: string[]; mcp_refs?: string[]; is_active: boolean; is_system?: boolean; priority?: number }

export interface ToolConfigResp { id: string; name: string; description: string; parameters: Record<string, any>; timeout: number; is_active: boolean; is_builtin: boolean; admin_only: boolean; created_at: string }

export interface PluginResp { id: string; name: string; version: string; path: string; config: Record<string, any>; is_active: boolean; created_at: string }

export interface ACLRuleResp { id: number; chat_area_id: string; scope: string; permission: string; target_type: string; user_ids: string[]; tool_ids: string[]; mcp_ids: string[]; created_at: string }
export interface AddACLRuleReq { chat_area_id: string; scope: string; permission: string; target_type: string; user_ids?: string[]; tool_ids?: string[]; mcp_ids?: string[] }

export interface ChatAreaResp { id: string; area_type: string; target_id: number; created_at: string }

export interface ChatRecordResp { id: number; chat_area_id: string; user_id: number; role: string; content: string; token_count: number; tool_calls: any; created_at: string }
export interface ChatRecordListResp { total: number; list: ChatRecordResp[] }

export interface OverviewResp { chat_area_count: number; mcp_count: number; adapter_count: number; plugin_count: number; provider_count: number; skill_count: number; session_count: number; total_token_usage: number; cpu_count: number; goroutine_num: number; mem_alloc_bytes: number; mem_sys_bytes: number; mem_heap_inuse_bytes: number; go_version: string; t2i_active: boolean; t2i_healthy: boolean; sandbox_active: boolean; sandbox_healthy: boolean; rag_active: boolean; rag_healthy: boolean }

export interface DailyTokenUsageResp { date: string; token_count: number }

export interface T2IConfigResp { base_url: string; timeout: number; is_active: boolean; healthy: boolean; selected_style: string }
export interface UpdateT2IConfigReq { base_url: string; timeout?: number; is_active: boolean; selected_style?: string }
export interface T2IStyleItem { name: string; category: string; tags: string[]; vibe: string }

export interface SandboxConfigResp { base_url: string; api_key: string; timeout: number; is_active: boolean; healthy: boolean }
export interface UpdateSandboxConfigReq { base_url: string; api_key: string; timeout?: number; is_active: boolean }

export interface WebhookConfigResp { addr: string; port: number; token: string; enabled: boolean; running: boolean }
export interface UpdateWebhookConfigReq { addr: string; port: number; token: string; enabled: boolean }

export interface LogEntryResp { time: string; level: string; message: string; attrs: Record<string, any> }

// ======== Auth ========
export const authApi = {
  login: (data: LoginReq) => client.post('/login', data),
  changePassword: (data: ChangePasswordReq) => client.post('/change-password', data),
}

// ======== Adapter ========
export const adapterApi = {
  getStatus: () => client.get('/adapter'),
  getConfig: () => client.get('/adapter/config'),
  updateConfig: (data: UpdateAdapterConfigReq) => client.put('/adapter', data),
  restart: () => client.post('/adapter/restart'),
}

// ======== Providers ========
export const providerApi = {
  list: () => client.get('/providers'),
  get: (id: string) => client.get(`/providers/${id}`),
  create: (data: AddProviderReq) => client.post('/providers', data),
  update: (id: string, data: AddProviderReq) => client.put(`/providers/${id}`, data),
  delete: (id: string) => client.delete(`/providers/${id}`),
  toggle: (id: string, is_active: boolean) => client.put(`/providers/${id}/toggle`, { is_active }),
  test: (data: AddProviderReq) => client.post('/providers/test', data),
}

export interface TestProviderResp { ok: boolean; message: string }

export const providerPresetsApi = {
  list: () => client.get('/providers/presets'),
}

// ======== MCP ========
export const mcpApi = {
  list: () => client.get('/mcp'),
  get: (id: string) => client.get(`/mcp/${id}`),
  create: (data: AddMCPServerReq) => client.post('/mcp', data),
  update: (id: string, data: AddMCPServerReq) => client.put(`/mcp/${id}`, data),
  delete: (id: string) => client.delete(`/mcp/${id}`),
  toggle: (id: string, is_active: boolean) => client.put(`/mcp/${id}/toggle`, { is_active }),
  check: (id: string) => client.get(`/mcp/${id}/check`),
}

// ======== Memory ========
export const memoryApi = {
  getShortTerm: (chatAreaID: string) => client.get(`/memory/${chatAreaID}/short-term`),
  updateShortTerm: (chatAreaID: string, data: { window_size: number; auto_compact: boolean }) => client.put(`/memory/${chatAreaID}/short-term`, data),
  getLongTerm: (chatAreaID: string) => client.get(`/memory/${chatAreaID}/long-term`),
  updateLongTerm: (chatAreaID: string, data: { hot_area_size: number; hot_memory_ttl: number; gc_interval_days?: number }) => client.put(`/memory/${chatAreaID}/long-term`, data),
  syncRAG: () => client.post('/memory/sync-rag'),
}

// ======== Prompts ========
export const promptApi = {
  list: () => client.get('/prompts'),
  create: (data: AddPromptReq) => client.post('/prompts', data),
  update: (id: string, data: AddPromptReq) => client.put(`/prompts/${id}`, data),
  delete: (id: string) => client.delete(`/prompts/${id}`),
  toggle: (id: string, is_active: boolean) => client.put(`/prompts/${id}/toggle`, { is_active }),
}

// ======== Sessions ========
export const sessionApi = {
  list: () => client.get('/sessions'),
  get: (id: string) => client.get(`/sessions/${id}`),
  delete: (id: string) => client.delete(`/sessions/${id}`),
}

// ======== Skills ========
export const skillApi = {
  list: () => client.get('/skills'),
  create: (data: AddSkillReq) => client.post('/skills', data),
  update: (id: string, data: AddSkillReq) => client.put(`/skills/${id}`, data),
  delete: (id: string) => client.delete(`/skills/${id}`),
}

// ======== Tools ========
export const toolApi = {
  list: () => client.get('/tools'),
  toggle: (id: string, is_active: boolean) => client.put(`/tools/${id}/toggle`, { is_active }),
  updateAdminOnly: (id: string, admin_only: boolean) => client.put(`/tools/${id}/admin-only`, { admin_only }),
}

// ======== Plugins ========
export const pluginApi = {
  list: () => client.get('/plugins'),
  upload: (file: File) => { const fd = new FormData(); fd.append('file', file); return client.post('/plugins/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } }) },
  reloadAll: () => client.post('/plugins/reload'),
  reload: (id: string) => client.post(`/plugins/${id}/reload`),
  toggle: (id: string, is_active: boolean) => client.put(`/plugins/${id}/toggle`, { is_active }),
  delete: (id: string) => client.delete(`/plugins/${id}`),
  config: (id: string) => client.get(`/plugins/${id}/config`),
  saveConfig: (id: string, values: Record<string, any>) => client.put(`/plugins/${id}/config`, { values }),
  readme: (id: string) => client.get(`/plugins/${id}/readme`),
  avatar: (id: string, v?: number) => client.get(`/plugins/${id}/avatar`, { params: { v: v ?? Date.now() }, responseType: 'blob' }),
}

export const storeApi = {
  list: () => client.get('/plugin-store'),
  readme: (path: string) => client.get('/plugin-store/readme', { params: { path } }),
  avatar: (path: string) => client.get('/plugin-store/avatar', { params: { path }, responseType: 'blob' }),
  install: (path: string) => client.post('/plugin-store/install', null, { params: { path } }),
  config: () => client.get('/plugin-store/config'),
  saveConfig: (cfg: any) => client.put('/plugin-store/config', cfg),
  addMirror: (mirror: string) => client.post('/plugin-store/mirror', { mirror }),
  testMirror: (mirror: string) => client.post('/plugin-store/mirror/test', { mirror }),
  selectMirror: (mirror: string) => client.post('/plugin-store/mirror/select', { mirror }),
  removeMirror: (mirror: string) => client.delete('/plugin-store/mirror', { data: { mirror } }),
}

// ======== ACL ========
export const aclApi = {
  list: () => client.get('/acl'),
  create: (data: AddACLRuleReq) => client.post('/acl', data),
  delete: (id: number) => client.delete(`/acl/${id}`),
}

// ======== Chat Areas ========
export const chatAreaApi = {
  list: () => client.get('/chat-areas'),
}

// ======== Chat Records ========
export const chatRecordApi = {
  list: (chatAreaID: string, params?: { limit?: number; offset?: number; role?: string }) => client.get(`/chat-records/${chatAreaID}`, { params }),
  tokenUsage: (chatAreaID: string) => client.get(`/chat-records/${chatAreaID}/token-usage`),
}

// ======== Overview ========
export const overviewApi = {
  get: () => client.get('/overview'),
  dailyTokenUsage: (days: number = 7) => client.get('/overview/daily-token-usage', { params: { days } }),
}

// ======== T2I ========
export const t2iApi = {
  getConfig: () => client.get('/t2i/config'),
  updateConfig: (data: UpdateT2IConfigReq) => client.put('/t2i/config', data),
  health: () => client.get('/t2i/health'),
  listStyles: () => client.get('/t2i/styles'),
}

// ======== Sandbox ========
export const sandboxApi = {
  getConfig: () => client.get('/sandbox/config'),
  updateConfig: (data: UpdateSandboxConfigReq) => client.put('/sandbox/config', data),
  health: () => client.get('/sandbox/health'),
}

// ======== RAG（向量检索服务） ========

export interface RAGConfigResp {
  base_url: string
  timeout: number
  is_active: boolean
  healthy: boolean
}

export interface UpdateRAGConfigReq {
  base_url: string
  timeout: number
  is_active: boolean
}

export interface RAGInfoResp {
  status?: string
  model?: {
    ready: boolean
    model_name?: string
    dim?: number
    n_params?: number
    n_threads?: number
    n_ctx?: number
    error?: string
  }
  memory?: { rss_kb: number; vsize_kb: number }
  // 各分库规模（scoop 名 → tag/块数量）；Tag/Chunk 总数由前端汇总
  scoops?: Record<string, { tags: number; chunks: number }>
  error?: string
}

export const ragApi = {
  getConfig: () => client.get('/rag/config'),
  updateConfig: (data: UpdateRAGConfigReq) => client.put('/rag/config', data),
  health: () => client.get('/rag/health'),
  info: () => client.get('/rag/info'),
}

// ======== Webhook ========
export const webhookApi = {
  getConfig: () => client.get('/webhook/config'),
  updateConfig: (data: UpdateWebhookConfigReq) => client.put('/webhook/config', data),
}

// ======== Logs ========
export const logApi = {
  list: () => client.get('/logs'),
}

// ======== Agent 活跃循环 ========
export interface AgentLoopResp {
  id: string
  chat_area_id: string
  message_type: string
  target_id: number
  user_id: number
  user_msg: string
  current_tool: string
  started_at: string
}

export const agentLoopApi = {
  list: () => client.get('/agent/loops'),
}

// ======== CronJob ========
export interface CronJobResp {
  id: string; name: string; cron_expr: string; message: string; message_type: string
  target_id: number; is_active: boolean
  plugin_ids: string[]; payload: string
  last_run_at?: string; last_error: string
  created_at: string; updated_at: string
}
export interface AddCronJobReq {
  name: string; cron_expr: string; is_active: boolean
  plugin_ids: string[]; payload: string
  // CronJob 仅进入 Plugin 链路，以下 Agent 相关字段已废弃
  message?: string; message_type?: string; target_id?: number
}

export const cronJobApi = {
  list: () => client.get('/cronjobs'),
  get: (id: string) => client.get(`/cronjobs/${id}`),
  create: (data: AddCronJobReq) => client.post('/cronjobs', data),
  update: (id: string, data: AddCronJobReq) => client.put(`/cronjobs/${id}`, data),
  delete: (id: string) => client.delete(`/cronjobs/${id}`),
  toggle: (id: string, is_active: boolean) => client.put(`/cronjobs/${id}/toggle`, { is_active }),
}

// ======== Reply Strategy ========
// 参与窗口已取代旧 relevance 相关性判断：@/命令/提及名字/私聊必回，
// 其余群聊消息攒窗后整窗一次喂给 Agent，由 LLM 自门控（__NO_REPLY__）决定参与或静默。

export interface ReplyStrategyResp {
  bot_name: string
  strip_markdown: boolean
  agent_lite: boolean
  quiet_gap_seconds: number
  force_count: number
  max_age_seconds: number
  window_max_msgs: number
  jitter_seconds: number
  force_count_jitter: number
  participate_probability: number
  typing_delay_max_ms: number
}

export interface UpdateReplyStrategyReq {
  bot_name: string
  strip_markdown: boolean
  agent_lite: boolean
  quiet_gap_seconds: number
  force_count: number
  max_age_seconds: number
  window_max_msgs: number
  jitter_seconds: number
  force_count_jitter: number
  participate_probability: number
  typing_delay_max_ms: number
}

export const replyStrategyApi = {
  get: () => client.get('/reply-strategy'),
  update: (data: UpdateReplyStrategyReq) => client.put('/reply-strategy', data),
}

export interface KnowledgeResp {
  id: string
  title: string
  content: string
  keywords: string[]
  keyword_status: string // pending / ready / failed
  created_at: string
  updated_at: string
}
export interface AddKnowledgeReq { title: string; content: string }
export interface KnowledgeListResp { total: number; list: KnowledgeResp[] }

export const knowledgeApi = {
  list: (page = 1, page_size = 20) => client.get('/knowledge', { params: { page, page_size } }),
  get: (id: string) => client.get(`/knowledge/${id}`),
  create: (data: AddKnowledgeReq) => client.post('/knowledge', data),
  update: (id: string, data: AddKnowledgeReq) => client.put(`/knowledge/${id}`, data),
  delete: (id: string) => client.delete(`/knowledge/${id}`),
  reExtract: (id: string) => client.post(`/knowledge/${id}/re-extract`),
  syncVector: () => client.post('/knowledge/vector-sync'),
}

// ======== 图床 ========
export interface ImageResp {
  id: string
  name: string
  folder: string // 虚拟文件夹路径，/ 表示根
  mime_type: string
  size_bytes: number
  created_at: string
  updated_at: string
}
export interface ImageFolderResp { id: string; name: string; created_at: string }
export interface ImageListResp { total: number; list: ImageResp[] }

// 图片文件访问（Web 预览，不走 axios 以便直接作为 <img> src）。
// <img> 无法携带 Authorization header，通过 ?token= 查询参数传递 JWT。
export const imageFileUrl = (id: string) => {
  const token = localStorage.getItem('token')
  const q = token ? `?token=${encodeURIComponent(token)}` : ''
  return `/api/v1/images/${id}/file${q}`
}

export const imageApi = {
  list: (params?: { folder?: string; page?: number; page_size?: number }) => client.get('/images', { params }),
  get: (id: string) => client.get(`/images/${id}`),
  upload: (file: File, name: string, folder: string) => {
    const fd = new FormData()
    fd.append('file', file)
    if (name.trim()) fd.append('name', name.trim())
    fd.append('folder', folder)
    return client.post('/images', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  update: (id: string, data: { name?: string; folder?: string }) => client.put(`/images/${id}`, data),
  remove: (id: string) => client.delete(`/images/${id}`),
}

export const imageFolderApi = {
  list: () => client.get('/image-folders'),
  create: (name: string) => client.post('/image-folders', { name }),
  remove: (id: string) => client.delete(`/image-folders/${id}`),
}

// ======== 表情包库 ========
export interface StickerResp {
  id: string // 短 UUID（发送时用）
  image_id: string // 图床图片长 UUID
  name: string
  desc: string
  tags: string[]
  created_at: string
  updated_at: string
}
export interface StickerTagResp { id: string; name: string; created_at: string }
export interface StickerListResp { total: number; list: StickerResp[] }

export const stickerApi = {
  list: (params?: { tag?: string; keyword?: string; page?: number; page_size?: number }) => client.get('/stickers', { params }),
  get: (id: string) => client.get(`/stickers/${id}`),
  create: (data: { image_id: string; name: string; desc?: string; tags?: string[] }) => client.post('/stickers', data),
  update: (id: string, data: { name?: string; desc?: string; tags?: string[] }) => client.put(`/stickers/${id}`, data),
  remove: (id: string) => client.delete(`/stickers/${id}`),
}

export const stickerTagApi = {
  list: () => client.get('/sticker-tags'),
  create: (name: string) => client.post('/sticker-tags', { name }),
  remove: (id: string) => client.delete(`/sticker-tags/${id}`),
}

// ======== 摸鱼人日历 ========
export interface FishCalendarConfigResp {
  enabled: boolean
  cron_expr: string
  target_groups: string[]
  last_run_at?: string | null
  last_error: string
}
export interface UpdateFishCalendarConfigReq {
  enabled: boolean
  cron_expr: string
  target_groups: string[]
}
export interface FishCalendarAffairResp { date: string; content: string }

export const fishCalendarApi = {
  get: () => client.get('/fish-calendar/config'),
  update: (data: UpdateFishCalendarConfigReq) => client.put('/fish-calendar/config', data),
  trigger: () => client.post('/fish-calendar/trigger'),
  affairs: (month: string) => client.get('/fish-calendar/affairs', { params: { month } }),
  setAffair: (date: string, content: string) => client.put('/fish-calendar/affairs', { date, content }),
}

// ======== 定时消息 ========
export interface ScheduledSegment {
  type: string // text / image / face
  source?: string // image: t2i / url / imgstore
  content: string
}
export interface ScheduledBlock {
  type: string // message / delay
  segments?: ScheduledSegment[]
  delay_seconds?: number
}
export interface ScheduledMessageResp {
  id: string
  name: string
  enabled: boolean
  cron_expr: string
  target_type: string // group / private
  target_id: number
  blocks: ScheduledBlock[]
  last_run_at?: string | null
  last_error: string
  created_at: string
  updated_at: string
}
export interface AddScheduledMessageReq {
  name: string
  enabled: boolean
  cron_expr: string
  target_type: string
  target_id: number
  blocks: ScheduledBlock[]
}

export const scheduledMessageApi = {
  list: (params?: { page?: number; page_size?: number }) => client.get('/scheduled-messages', { params }),
  get: (id: string) => client.get(`/scheduled-messages/${id}`),
  create: (data: AddScheduledMessageReq) => client.post('/scheduled-messages', data),
  update: (id: string, data: AddScheduledMessageReq) => client.put(`/scheduled-messages/${id}`, data),
  remove: (id: string) => client.delete(`/scheduled-messages/${id}`),
  toggle: (id: string, enabled: boolean) => client.put(`/scheduled-messages/${id}/toggle`, { enabled }),
  trigger: (id: string) => client.post(`/scheduled-messages/${id}/trigger`),
}

// ======== 群管理 ========
export interface GroupMgrConfigResp {
  enabled: boolean
  llm_review: boolean
  black_min_score: number
  white_min_score: number
  llm_batch_window: number
  img_spam_window: number
  img_spam_threshold: number
  img_mute_duration: number
  enable_copy_check: boolean
  copy_threshold: number
  violation_mute_seconds: number
  exclude_groups: string[]
  llm_prompt: string
  llm_criteria: string
  llm_gray_prompt: string
  llm_high_risk_prompt: string
  white_gc_interval_days: number
}

export interface UpdateGroupMgrConfigReq {
  enabled: boolean
  llm_review: boolean
  black_min_score: number
  white_min_score: number
  llm_batch_window: number
  img_spam_window: number
  img_spam_threshold: number
  img_mute_duration: number
  enable_copy_check: boolean
  copy_threshold: number
  violation_mute_seconds: number
  exclude_groups: string[]
  llm_prompt: string
  llm_criteria: string
  llm_gray_prompt: string
  llm_high_risk_prompt: string
  white_gc_interval_days: number
}
export interface GroupMgrWordResp { id: number; word: string; category: string; source: string; rag_synced: boolean; rag_tag: string }
export interface GroupMgrSampleResp {
  id: number
  word_id: number
  list_type: string
  text: string
  category: string
  source: string
  hit_count: number
  rag_synced: boolean
  rag_tag: string
  last_used_at: string | null
  created_at: string
}
export interface GroupMgrViolationResp { id: number; group_id: number; user_id: number; username: string; count: number; detection_path: string; llm_reason: string }
export interface GroupMgrStatsResp {
  group_id: number
  date: string
  join_today: number
  warns: number
  mutes: number
  copy_warns: number
  ad: number
  sensitive: number
  kicks: number
}
export interface GroupMgrTestResp {
  text: string
  card: boolean
  word: string
  word_cat: string
  rag_ok: boolean
  black_score: number | null
  black_phrase: string
  white_score: number | null
  white_phrase: string
  verdict: string
  reason: string
}

export const groupMgrApi = {
  getConfig: () => client.get('/group-mgr/config'),
  updateConfig: (data: UpdateGroupMgrConfigReq) => client.put('/group-mgr/config', data),
  words: (category?: string) => client.get('/group-mgr/words', { params: { category } }),
  addWord: (word: string, category: string) => client.post('/group-mgr/words', { word, category }),
  deleteWord: (id: number) => client.delete(`/group-mgr/words/${id}`),
  importWords: (file: File, category: string) => {
    const form = new FormData()
    form.append('file', file)
    return client.post(`/group-mgr/words/import?category=${category}`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  syncRAG: () => client.post('/group-mgr/sync-rag'),
  samples: (listType?: string) => client.get('/group-mgr/samples', { params: { list_type: listType } }),
  deleteSample: (id: number) => client.delete(`/group-mgr/samples/${id}`),
  addPhrase: (text: string, listType: string, category?: string) =>
    client.post('/group-mgr/phrases', { text, list_type: listType, category }),
  importPhrases: (file: File, listType: string, category?: string) => {
    const form = new FormData()
    form.append('file', file)
    return client.post(`/group-mgr/phrases/import?list_type=${listType}&category=${category ?? 'ad'}`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  violations: () => client.get('/group-mgr/violations'),
  deleteViolation: (id: number) => client.delete(`/group-mgr/violations/${id}`),
  whitelist: () => client.get('/group-mgr/whitelist'),
  updateWhitelist: (qq_list: number[]) => client.put('/group-mgr/whitelist', { qq_list }),
  admins: () => client.get('/group-mgr/admins'),
  updateAdmins: (qq_list: number[]) => client.put('/group-mgr/admins', { qq_list }),
  syncAdminsFromAdapter: () => client.post('/group-mgr/admins/sync-from-adapter'),
  stats: (group_id: number) => client.get('/group-mgr/stats', { params: { group_id } }),
  test: (text: string) => client.post('/group-mgr/test', { text }),
}
