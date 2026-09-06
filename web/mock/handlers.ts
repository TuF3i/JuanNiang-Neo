import type { MockHandler } from './plugin'

// ============================================================
// Mock Data Store (in-memory, resets on HMR)
// ============================================================

const UUID = () =>
  'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0
    const v = c === 'x' ? r : (r & 0x3) | 0x8
    return v.toString(16)
  })

const now = () => new Date().toISOString()

// --- Providers ---
let providers = [
  { id: UUID(), created_at: now(), name: 'OpenAI GPT-4o', type: 'text_model', endpoint: 'https://api.openai.com/v1', token: 'sk-***', model: 'gpt-4o', temperature: 0.7, is_active: true },
  { id: UUID(), created_at: now(), name: 'DALL-E 3', type: 'image_model', endpoint: 'https://api.openai.com/v1', token: 'sk-***', model: 'dall-e-3', temperature: 0, is_active: true },
  { id: UUID(), created_at: now(), name: 'OpenAI Embedding', type: 'embedding_model', endpoint: 'https://api.openai.com/v1', token: 'sk-***', model: 'text-embedding-3-small', temperature: 0, is_active: false },
]

// --- MCP Servers ---
let mcpServers = [
  { id: UUID(), name: 'File System', server_url: 'http://localhost:9000/sse', headers: {}, timeout: 30000, retry_count: 3, tool_filter: [], auto_reconnect: true, is_active: true, created_at: now() },
  { id: UUID(), name: 'Web Search', server_url: 'http://localhost:9001/sse', headers: { 'X-API-Key': '***' }, timeout: 15000, retry_count: 2, tool_filter: ['search', 'fetch'], auto_reconnect: false, is_active: false, created_at: now() },
]

// --- Prompts ---
let prompts = [
  { id: UUID(), name: 'System Prompt', content: 'You are JuanNiang, a helpful AI assistant. You are polite, knowledgeable, and always ready to help.', type: 'system', is_active: true, variables: ['user_name', 'date'], created_at: now() },
  { id: UUID(), name: 'Personality: Tsundere', content: 'You have a tsundere personality. You act cold and dismissive but secretly care about the user.', type: 'personality', is_active: false, variables: [], created_at: now() },
  { id: UUID(), name: 'Custom: Code Review', content: 'You are a senior code reviewer. Review the following code and provide constructive feedback:\n\n{{code}}', type: 'custom', is_active: true, variables: ['code'], created_at: now() },
]

// --- Sessions ---
let sessions = [
  { id: UUID(), chat_area_id: UUID(), model: 'gpt-4o', token_usage: 45600, meta_data: { greeting: true }, created_at: now() },
  { id: UUID(), chat_area_id: UUID(), model: 'gpt-4o', token_usage: 12300, meta_data: {}, created_at: now() },
  { id: UUID(), chat_area_id: UUID(), model: 'gpt-4o', token_usage: 89200, meta_data: { memory_mode: 'long' }, created_at: now() },
]

// --- Skills ---
let skills = [
  { id: UUID(), name: 'Code Execute', description: 'Execute code in sandbox', keywords: ['run', 'execute', 'code'], regex_pattern: '', prompt_ref: prompts[2].id, tool_refs: [UUID()], mcp_refs: [], is_active: true, is_system: false, priority: 10, created_at: now() },
  { id: UUID(), name: 'File Read', description: 'Read file from filesystem via MCP', keywords: ['read', 'file', 'cat'], regex_pattern: '^/read\\s+', prompt_ref: '', tool_refs: [], mcp_refs: [mcpServers[0].id], is_active: true, is_system: false, priority: 5, created_at: now() },
  { id: UUID(), name: 'System Greeting', description: 'Auto greet new users', keywords: [], regex_pattern: '', prompt_ref: prompts[0].id, tool_refs: [], mcp_refs: [], is_active: true, is_system: true, priority: 0, created_at: now() },
]

// --- Tools ---
const tools = [
  { id: UUID(), name: 'web_search', description: 'Search the web for information', parameters: { type: 'object', properties: { query: { type: 'string', description: 'The search query' } }, required: ['query'] }, timeout: 30000, is_active: true, is_builtin: true, created_at: now() },
  { id: UUID(), name: 'code_execute', description: 'Execute code in a sandbox', parameters: { type: 'object', properties: { language: { type: 'string' }, code: { type: 'string' } }, required: ['language', 'code'] }, timeout: 60000, is_active: true, is_builtin: true, created_at: now() },
  { id: UUID(), name: 'image_generate', description: 'Generate an image using AI', parameters: { type: 'object', properties: { prompt: { type: 'string' }, size: { type: 'string' } }, required: ['prompt'] }, timeout: 120000, is_active: false, is_builtin: true, created_at: now() },
  { id: UUID(), name: 'send_qq_message', description: 'Send a message via QQ', parameters: { type: 'object', properties: { target_id: { type: 'number' }, message: { type: 'string' } }, required: ['target_id', 'message'] }, timeout: 10000, is_active: true, is_builtin: false, created_at: now() },
]

// --- RAG 向量检索 ---
let ragConfig = { base_url: 'http://localhost:3000', timeout: 30, is_active: true }
let ragHealthy = true
// 回复设置 mock 共享态：PUT 落盘，后续 GET 返回已保存的配置（而非始终回显默认值）
let replyStrategyState = {
  bot_name: '小卷',
  strip_markdown: false,
  agent_lite: false,
  quiet_gap_seconds: 5,
  force_count: 5,
  max_age_seconds: 20,
  window_max_msgs: 20,
  jitter_seconds: 2,
  force_count_jitter: 1,
  participate_probability: 0.8,
  typing_delay_max_ms: 1500,
}
// GET /info 参考 JuanNiang-RAG-Service API 文档示例（scoops: 分库名 → tag/块数量）
let ragInfo = {
  status: 'ok',
  model: { ready: true, model_name: 'bge-small-zh-v1.5', dim: 512, n_params: 23691264, n_threads: 4, n_ctx: 4096, error: null },
  memory: { rss_kb: 81244, vsize_kb: 1915772 },
  scoops: {
    knowledge: { tags: 96, chunks: 260 },
    groupmgr: { tags: 32, chunks: 80 },
  },
}

// --- 群管理 ---
let groupMgrDefaultConfig = {
  enabled: true,
  llm_review: true,
  black_min_score: 0.7,
  white_min_score: 0.75,
  llm_batch_window: 3,
  img_spam_window: 2,
  img_spam_threshold: 3,
  img_mute_duration: 60,
  enable_copy_check: true,
  copy_threshold: 3,
  violation_mute_seconds: 1800,
  exclude_groups: [],
  llm_prompt: '',
  llm_criteria: '',
  llm_gray_prompt: '',
  llm_high_risk_prompt: '',
  white_gc_interval_days: 7,
}
let groupMgrConfig: any = null
let groupMgrWordSeed = 100
let groupMgrWords = [
  { id: 1, word: '办校园卡', category: 'black', source: 'system', rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b001' },
  { id: 2, word: '贷款提额', category: 'black', source: 'system', rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b002' },
  { id: 3, word: '校园卡', category: 'gray', source: 'system', rag_synced: false, rag_tag: '' },
  { id: 4, word: '考研机构', category: 'gray', source: 'system', rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b003' },
  { id: 5, word: '兼职刷单', category: 'sensitive', source: 'import', rag_synced: false, rag_tag: '' },
]
let groupMgrSampleSeed = 100
let groupMgrSamples = [
  { id: 1, word_id: 0, list_type: 'black', text: '办卡加群办套餐，低价流量卡', category: 'ad', source: 'learn', hit_count: 3, rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b011', last_used_at: now(), created_at: now() },
  { id: 2, word_id: 0, list_type: 'black', text: '0元购送福利，加我微信领流量卡', category: 'ad', source: 'seed', hit_count: 1, rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b012', last_used_at: null, created_at: now() },
  { id: 3, word_id: 0, list_type: 'white', text: '明天一起食堂吃饭吗', category: 'ok', source: 'seed', hit_count: 5, rag_synced: true, rag_tag: '3af2b489-b13a-42e4-af98-fe89d0e6b013', last_used_at: now(), created_at: now() },
  { id: 4, word_id: 0, list_type: 'white', text: '周末去爬山吗，新版本出了', category: 'ok', source: 'learn', hit_count: 0, rag_synced: false, rag_tag: '', last_used_at: null, created_at: now() },
]
let groupMgrViolations = [
  { id: 1, group_id: 10001, user_id: 20001, username: '张三', count: 1, detection_path: 'rag', llm_reason: '' },
  { id: 2, group_id: 10001, user_id: 20002, username: '李四', count: 3, detection_path: 'llm', llm_reason: '明确广告引流：低价流量卡 + 加裙号，判 ad' },
  { id: 3, group_id: 10002, user_id: 20003, username: '王五', count: 2, detection_path: 'keyword', llm_reason: '' },
]
let groupMgrWhitelist: number[] = [30001]
let groupMgrAdmins: number[] = [30002]

// --- 知识库 ---
let knowledgeItems = [
  { id: UUID(), title: '红岩网校介绍', content: '红岩网校是重庆邮电大学的互联网团队，负责学校的网络信息化建设。', keywords: ['红岩网校', '重邮'], keyword_status: 'ready', created_at: now(), updated_at: now() },
  { id: UUID(), title: '群规', content: '禁止广告、刷屏、侮辱谩骂；进群请修改群名片。', keywords: ['群规'], keyword_status: 'pending', created_at: now(), updated_at: now() },
]

// --- Plugins ---
let plugins = [
  { id: 'weather-plugin', name: 'weather-plugin', version: '1.2.0', path: 'data/pluggins/weather-plugin/', config: { api_key: '***', default_city: 'Beijing' }, is_active: true, supports_cronjob: true, created_at: now() },
  { id: 'translate-plugin', name: 'translate-plugin', version: '0.5.0', path: 'data/pluggins/translate-plugin/', config: {}, is_active: false, supports_cronjob: false, created_at: now() },
  { id: 'scheduler-plugin', name: 'scheduler-plugin', version: '1.0.1', path: 'data/pluggins/scheduler-plugin/', config: { timezone: 'Asia/Shanghai' }, is_active: true, supports_cronjob: true, created_at: now() },
]

// --- ACL Rules ---
let aclRules = [
  { id: 1, chat_area_id: UUID(), scope: 'chat', permission: 'deny', target_type: 'all', user_ids: [], created_at: now() },
  { id: 2, chat_area_id: UUID(), scope: 'chat', permission: 'deny', target_type: 'list', user_ids: ['123456', '789012'], created_at: now() },
]

// --- CronJobs ---
let cronJobs: any[] = []
let cronJobIdCounter = 1
let aclIdCounter = 4

// --- Chat Areas ---
const chatAreas = [
  { id: UUID(), area_type: 'private', target_id: 123456789, created_at: now() },
  { id: UUID(), area_type: 'private', target_id: 987654321, created_at: now() },
  { id: UUID(), area_type: 'group', target_id: 555666777, created_at: now() },
  { id: UUID(), area_type: 'group', target_id: 888999000, created_at: now() },
  { id: UUID(), area_type: 'private', target_id: 111222333, created_at: now() },
]

// --- Chat Records ---
function generateChatRecords(chatAreaId: string, count: number) {
  const roles = ['user', 'assistant', 'user', 'assistant', 'tool']
  const messages = [
    '你好！今天天气怎么样？',
    '你好！根据最新的气象数据，今天北京天气晴朗，温度在 22°C 到 30°C 之间，适合户外活动。',
    '帮我查一下最新的新闻',
    '好的，我通过搜索工具为您找到了以下新闻...',
    '',
    '请帮我生成一张猫咪的图片',
    '抱歉，图片生成功能当前未启用。您可以先在 T2I 设置中配置图片生成服务。',
  ]
  const records: any[] = []
  for (let i = 0; i < count; i++) {
    const role = roles[i % roles.length]
    records.push({
      id: i + 1,
      chat_area_id: chatAreaId,
      user_id: role === 'user' ? 123456789 : 0,
      role,
      content: messages[i % messages.length] || `Message ${i + 1}`,
      token_count: Math.floor(Math.random() * 500) + 10,
      tool_calls: role === 'tool' ? { name: 'web_search', result: '...' } : null,
      created_at: new Date(Date.now() - (count - i) * 60000).toISOString(),
    })
  }
  return records
}

// --- Adapter ---
let adapterState = {
  running: true,
  listen_addr: '0.0.0.0:8080',
  self_id: 1234567890,
  conn_count: 2,
  conn_ids: [111222333, 444555666],
}

// --- Adapter Config ---
let adapterConfig = {
  addr: '0.0.0.0',
  port: 8080,
  token: 'onebot-token-123',
  admin_qq_numbers: ['123456789'],
  enabled: true,
}

// --- Memory configs cache ---
const memoryConfigs: Record<string, any> = {}

// --- T2I ---
let t2iConfig = { base_url: 'http://localhost:7860', timeout: 120000, is_active: false, healthy: false }

// --- Sandbox ---
let sandboxConfig = { base_url: 'http://localhost:8888', api_key: '***', timeout: 60000, is_active: true, healthy: true }

// --- Webhook ---
let webhookConfig = { addr: '0.0.0.0', port: 8099, token: 'webhook-token', enabled: false, running: false }

// --- Auth mock ---
const VALID_TOKEN = 'mock-jwt-token-juanniang'

// ============================================================
// Response helpers
// ============================================================
const ok = (data: any = null) => ({ status: 0, info: 'OK', data })
const err = (status: number, info: string) => ({ status, info, data: null })

// ---- 严格数值解析（与服务端 BindJSON + normalizeInt 语义对齐） ----
// 未提供(undefined)/null → 0（Go 零值，走 0=默认 / 0=关闭 语义）；
// 其余仅接受 typeof === 'number' 的有限值，整数参数额外要求 Number.isInteger——
// 字符串/浮点/非有限值对应服务端 BindJSON 失败 → 40001 参数格式错误；
// 负数/越界 → 40032 范围错误。不做 Number() 隐式转换、不做 Math.trunc 截断。

// intVal 严格整数解析：未提供/null → 0；类型/非整数/非有限 → 40001。
const intVal = (v: any): number | ReturnType<typeof err> => {
  if (v === undefined || v === null) return 0
  if (typeof v !== 'number' || !Number.isFinite(v) || !Number.isInteger(v)) {
    return err(40001, '参数格式错误')
  }
  return v
}

// floatVal 严格浮点解析：未提供/null → 0；类型/非有限 → 40001。
const floatVal = (v: any): number | ReturnType<typeof err> => {
  if (v === undefined || v === null) return 0
  if (typeof v !== 'number' || !Number.isFinite(v)) return err(40001, '参数格式错误')
  return v
}

// normInt 整数参数归一化（0=默认 def）：类型/非整数 → 40001；负数/越界 → 40032。
const normInt = (v: any, min: number, max: number, def: number, msg: string): number | ReturnType<typeof err> => {
  const n = intVal(v)
  if (typeof n !== 'number') return n
  if (n < 0) return err(40032, msg)
  if (n === 0) return def
  if (n < min || n > max) return err(40032, msg)
  return n
}

// rngInt 整数范围校验（0=关闭合法值，无 0=默认）：类型/非整数 → 40001；越界 → 40032。
const rngInt = (v: any, min: number, max: number, msg: string): number | ReturnType<typeof err> => {
  const n = intVal(v)
  if (typeof n !== 'number') return n
  if (n < min || n > max) return err(40032, msg)
  return n
}

// rngFloat 浮点范围校验（0=关闭合法值）：类型 → 40001；越界 → 40032。
const rngFloat = (v: any, min: number, max: number, msg: string): number | ReturnType<typeof err> => {
  const n = floatVal(v)
  if (typeof n !== 'number') return n
  if (n < min || n > max) return err(40032, msg)
  return n
}

// ============================================================
// Route Handlers
// ============================================================
export const mockHandlers: MockHandler[] = [
  // ============ Auth ============
  {
    method: 'POST', path: '/login',
    handler({ body }) {
      if (body?.username === 'admin' && body?.password === 'Admin123') {
        return ok({ token: VALID_TOKEN })
      }
      return err(40002, '用户名或密码错误')
    }
  },
  {
    method: 'POST', path: '/change-password',
    handler({ body }) {
      if (body?.old_password === 'Admin123') {
        return ok(null)
      }
      return err(40005, '原密码错误')
    }
  },

  // ============ Adapter ============
  {
    method: 'GET', path: '/adapter',
    handler() { return ok({ ...adapterState }) }
  },
  {
    method: 'PUT', path: '/adapter',
    handler({ body }) {
      adapterConfig = { ...adapterConfig, ...body }
      adapterState = { ...adapterState, listen_addr: `${body.addr}:${body.port}`, running: body.enabled }
      return ok(null)
    }
  },
  {
    method: 'GET', path: '/adapter/config',
    handler() { return ok(adapterConfig) }
  },
  {
    method: 'POST', path: '/adapter/restart',
    handler() {
      adapterState.running = true
      return ok(null)
    }
  },

  // ============ Providers ============
  {
    method: 'GET', path: '/providers',
    handler() { return ok(providers) }
  },
  {
    method: 'GET', path: '/providers/:id',
    handler({ params }) {
      const p = providers.find((p) => p.id === params.id)
      return p ? ok(p) : err(40009, 'provider 不存在')
    }
  },
  {
    method: 'POST', path: '/providers',
    handler({ body }) {
      if (body.isActive) {
        providers.forEach((p) => { if (p.type === body.type) p.is_active = false })
      }
      const p = { id: UUID(), created_at: now(), name: body.name, type: body.type, endpoint: body.endpoint, token: body.token, model: body.model, temperature: body.temperature ?? 0.7, is_active: body.isActive }
      providers.push(p)
      return ok(p)
    }
  },
  {
    method: 'PUT', path: '/providers/:id',
    handler({ params, body }) {
      const idx = providers.findIndex((p) => p.id === params.id)
      if (idx === -1) return err(40009, 'provider 不存在')
      if (body.isActive) {
        providers.forEach((p) => { if (p.id !== params.id && p.type === providers[idx].type) p.is_active = false })
      }
      providers[idx] = { ...providers[idx], ...body, id: params.id, created_at: providers[idx].created_at }
      return ok(providers[idx])
    }
  },
  {
    method: 'DELETE', path: '/providers/:id',
    handler({ params }) {
      providers = providers.filter((p) => p.id !== params.id)
      return ok(null)
    }
  },
  {
    method: 'PUT', path: '/providers/:id/toggle',
    handler({ params, body }) {
      const idx = providers.findIndex((p) => p.id === params.id)
      if (idx === -1) return err(40009, 'provider 不存在')
      if (body.is_active) {
        providers.forEach((p) => { if (p.id !== params.id && p.type === providers[idx].type) p.is_active = false })
      }
      providers[idx].is_active = body.is_active
      return ok(null)
    }
  },

  // ============ MCP ============
  {
    method: 'GET', path: '/mcp',
    handler() { return ok(mcpServers) }
  },
  {
    method: 'GET', path: '/mcp/:id',
    handler({ params }) {
      const m = mcpServers.find((m) => m.id === params.id)
      return m ? ok(m) : err(40010, 'MCP 服务器不存在')
    }
  },
  {
    method: 'POST', path: '/mcp',
    handler({ body }) {
      const m = { id: UUID(), name: body.name, server_url: body.server_url, headers: body.headers || {}, timeout: body.timeout || 30000, retry_count: body.retry_count || 3, tool_filter: body.tool_filter || [], auto_reconnect: body.auto_reconnect ?? true, is_active: body.is_active, created_at: now() }
      mcpServers.push(m)
      return ok(m)
    }
  },
  {
    method: 'PUT', path: '/mcp/:id',
    handler({ params, body }) {
      const idx = mcpServers.findIndex((m) => m.id === params.id)
      if (idx === -1) return err(40010, 'MCP 服务器不存在')
      mcpServers[idx] = { ...mcpServers[idx], ...body, id: params.id, created_at: mcpServers[idx].created_at }
      return ok(mcpServers[idx])
    }
  },
  {
    method: 'DELETE', path: '/mcp/:id',
    handler({ params }) {
      mcpServers = mcpServers.filter((m) => m.id !== params.id)
      return ok(null)
    }
  },
  {
    method: 'PUT', path: '/mcp/:id/toggle',
    handler({ params, body }) {
      const idx = mcpServers.findIndex((m) => m.id === params.id)
      if (idx === -1) return err(40010, 'MCP 服务器不存在')
      mcpServers[idx].is_active = body.is_active
      return ok(null)
    }
  },

  // ============ Memory ============
  {
    method: 'GET', path: '/memory/:chatAreaID/short-term',
    handler({ params }) {
      if (!memoryConfigs[`st_${params.chatAreaID}`]) {
        memoryConfigs[`st_${params.chatAreaID}`] = { id: UUID(), chat_area_id: params.chatAreaID, window_size: 20, auto_compact: false, created_at: now() }
      }
      return ok(memoryConfigs[`st_${params.chatAreaID}`])
    }
  },
  {
    method: 'PUT', path: '/memory/:chatAreaID/short-term',
    handler({ params, body }) {
      memoryConfigs[`st_${params.chatAreaID}`] = { id: UUID(), chat_area_id: params.chatAreaID, window_size: body.window_size, auto_compact: body.auto_compact, created_at: now() }
      return ok(memoryConfigs[`st_${params.chatAreaID}`])
    }
  },
  {
    method: 'GET', path: '/memory/:chatAreaID/long-term',
    handler({ params }) {
      if (!memoryConfigs[`lt_${params.chatAreaID}`]) {
        memoryConfigs[`lt_${params.chatAreaID}`] = { id: UUID(), chat_area_id: params.chatAreaID, hot_area_size: 10, hot_memory_ttl: 86400, gc_interval_days: 7, created_at: now() }
      }
      return ok(memoryConfigs[`lt_${params.chatAreaID}`])
    }
  },
  {
    method: 'PUT', path: '/memory/:chatAreaID/long-term',
    handler({ params, body }) {
      memoryConfigs[`lt_${params.chatAreaID}`] = { id: UUID(), chat_area_id: params.chatAreaID, hot_area_size: body.hot_area_size, hot_memory_ttl: body.hot_memory_ttl, gc_interval_days: body.gc_interval_days ?? 7, created_at: now() }
      return ok(memoryConfigs[`lt_${params.chatAreaID}`])
    }
  },

  // ============ Prompts ============
  {
    method: 'GET', path: '/prompts',
    handler() { return ok(prompts) }
  },
  {
    method: 'POST', path: '/prompts',
    handler({ body }) {
      const p = { id: UUID(), name: body.name, content: body.content, type: body.type, is_active: body.is_active, variables: body.variables || [], created_at: now() }
      prompts.push(p)
      return ok(p)
    }
  },
  {
    method: 'PUT', path: '/prompts/:id',
    handler({ params, body }) {
      const idx = prompts.findIndex((p) => p.id === params.id)
      if (idx === -1) return err(40019, 'Prompt 不存在')
      prompts[idx] = { ...prompts[idx], ...body, id: params.id, created_at: prompts[idx].created_at }
      return ok(prompts[idx])
    }
  },
  {
    method: 'DELETE', path: '/prompts/:id',
    handler({ params }) {
      prompts = prompts.filter((p) => p.id !== params.id)
      return ok(null)
    }
  },
  {
    method: 'PUT', path: '/prompts/:id/toggle',
    handler({ params, body }) {
      const idx = prompts.findIndex((p) => p.id === params.id)
      if (idx === -1) return err(40019, 'Prompt 不存在')
      prompts[idx].is_active = body.is_active
      return ok(null)
    }
  },

  // ============ Sessions ============
  {
    method: 'GET', path: '/sessions',
    handler() { return ok(sessions) }
  },
  {
    method: 'GET', path: '/sessions/:id',
    handler({ params }) {
      const s = sessions.find((s) => s.id === params.id)
      return s ? ok(s) : err(40011, 'Session 不存在')
    }
  },
  {
    method: 'DELETE', path: '/sessions/:id',
    handler({ params }) {
      sessions = sessions.filter((s) => s.id !== params.id)
      return ok(null)
    }
  },

  // ============ Skills ============
  {
    method: 'GET', path: '/skills',
    handler() { return ok(skills) }
  },
  {
    method: 'POST', path: '/skills',
    handler({ body }) {
      const s = { id: UUID(), name: body.name, description: body.description || '', keywords: body.keywords || [], regex_pattern: body.regex_pattern || '', prompt_ref: body.prompt_ref || '', tool_refs: body.tool_refs || [], mcp_refs: body.mcp_refs || [], is_active: body.is_active, is_system: body.is_system ?? false, priority: body.priority ?? 0, created_at: now() }
      skills.push(s)
      return ok(s)
    }
  },
  {
    method: 'PUT', path: '/skills/:id',
    handler({ params, body }) {
      const idx = skills.findIndex((s) => s.id === params.id)
      if (idx === -1) return err(40018, 'Skill 不存在')
      skills[idx] = { ...skills[idx], ...body, id: params.id, created_at: skills[idx].created_at }
      return ok(skills[idx])
    }
  },
  {
    method: 'DELETE', path: '/skills/:id',
    handler({ params }) {
      skills = skills.filter((s) => s.id !== params.id)
      return ok(null)
    }
  },

  // ============ Tools ============
  {
    method: 'GET', path: '/tools',
    handler() { return ok(tools) }
  },
  {
    method: 'PUT', path: '/tools/:id/toggle',
    handler({ params, body }) {
      const t = tools.find((t) => t.id === params.id)
      if (!t) return err(40020, 'Tool 不存在')
      t.is_active = body.is_active
      return ok(null)
    }
  },

  // ============ Plugins ============
  {
    method: 'GET', path: '/plugins',
    handler() { return ok(plugins) }
  },
  {
    method: 'POST', path: '/plugins/upload',
    handler() { return ok({ name: 'new-plugin', status: 'loaded' }) }
  },
  {
    method: 'PUT', path: '/plugins/:id/toggle',
    handler({ params, body }) {
      const idx = plugins.findIndex((p) => p.id === params.id)
      if (idx === -1) return err(40021, 'Plugin 不存在')
      plugins[idx].is_active = body.is_active
      return ok(null)
    }
  },
  {
    method: 'DELETE', path: '/plugins/:id',
    handler({ params }) {
      plugins = plugins.filter((p) => p.id !== params.id)
      return ok(null)
    }
  },

  // ============ ACL ============
  {
    method: 'GET', path: '/acl',
    handler() { return ok(aclRules) }
  },
  {
    method: 'POST', path: '/acl',
    handler({ body }) {
      const existing = aclRules.findIndex((r) => r.chat_area_id === body.chat_area_id && r.scope === body.scope)
      const rule = { id: aclIdCounter++, chat_area_id: body.chat_area_id, scope: body.scope, permission: body.permission, target_type: body.target_type, user_ids: body.user_ids || [], created_at: now() }
      if (existing !== -1) {
        aclRules[existing] = { ...rule, id: aclRules[existing].id }
        return ok(aclRules[existing])
      }
      aclRules.push(rule)
      return ok(rule)
    }
  },
  {
    method: 'DELETE', path: '/acl/:id',
    handler({ params }) {
      aclRules = aclRules.filter((r) => r.id !== Number(params.id))
      return ok(null)
    }
  },

  // ============ CronJobs ============
  {
    method: 'GET', path: '/cronjobs',
    handler() { return ok(cronJobs) }
  },
  {
    method: 'POST', path: '/cronjobs',
    handler({ body }: any) {
      const job = { id: `cron-${cronJobIdCounter++}`, created_at: now(), updated_at: now(), last_run_at: null, last_error: '', ...body }
      cronJobs.unshift(job)
      return ok(job)
    }
  },
  {
    method: 'PUT', path: '/cronjobs/:id',
    handler({ params, body }: any) {
      const idx = cronJobs.findIndex((j) => j.id === params.id)
      if (idx === -1) return ok(null)
      cronJobs[idx] = { ...cronJobs[idx], ...body, id: params.id, updated_at: now() }
      return ok(cronJobs[idx])
    }
  },
  {
    method: 'DELETE', path: '/cronjobs/:id',
    handler({ params }: any) {
      cronJobs = cronJobs.filter((j) => j.id !== params.id)
      return ok(null)
    }
  },
  {
    method: 'PUT', path: '/cronjobs/:id/toggle',
    handler({ params, body }: any) {
      const idx = cronJobs.findIndex((j) => j.id === params.id)
      if (idx === -1) return ok(null)
      cronJobs[idx].is_active = body.is_active
      return ok(cronJobs[idx])
    }
  },

  // ============ Chat Areas ============
  {
    method: 'GET', path: '/chat-areas',
    handler() { return ok(chatAreas) }
  },

  // ============ Chat Records ============
  {
    method: 'GET', path: '/chat-records/:chatAreaID',
    handler({ params, query }) {
      const all = generateChatRecords(params.chatAreaID, 50)
      const limit = Number(query.limit) || 20
      const offset = Number(query.offset) || 0
      const role = query.role
      let filtered = role ? all.filter((r) => r.role === role) : all
      const total = filtered.length
      const list = filtered.slice(offset, offset + limit)
      return ok({ total, list })
    }
  },
  {
    method: 'GET', path: '/chat-records/:chatAreaID/token-usage',
    handler({ params }) {
      const s = sessions.find((s) => s.chat_area_id === params.chatAreaID)
      return ok(s || { id: UUID(), chat_area_id: params.chatAreaID, model: 'gpt-4o', token_usage: 0, meta_data: {}, created_at: now() })
    }
  },

  // ============ Overview ============
  {
    method: 'GET', path: '/overview',
    handler() {
      return ok({
        chat_area_count: chatAreas.length,
        mcp_count: mcpServers.length,
        adapter_count: 1,
        plugin_count: plugins.length,
        provider_count: providers.length,
        skill_count: skills.length,
        session_count: sessions.length,
        total_token_usage: sessions.reduce((sum, s) => sum + s.token_usage, 0),
        cpu_count: 8,
        goroutine_num: 47,
        mem_alloc_bytes: 33554432,
        mem_sys_bytes: 67108864,
        mem_heap_inuse_bytes: 16777216,
        go_version: 'go1.25',
        t2i_active: t2iConfig.is_active,
        t2i_healthy: t2iConfig.healthy,
        sandbox_active: sandboxConfig.is_active,
        sandbox_healthy: sandboxConfig.healthy,
        rag_active: ragConfig.is_active,
        rag_healthy: ragHealthy,
      })
    }
  },

  // ============ T2I ============
  {
    method: 'GET', path: '/t2i/config',
    handler() { return ok({ ...t2iConfig }) }
  },
  {
    method: 'PUT', path: '/t2i/config',
    handler({ body }) {
      t2iConfig = { ...t2iConfig, ...body }
      return ok({ ...t2iConfig })
    }
  },
  {
    method: 'GET', path: '/t2i/health',
    handler() { return ok({ healthy: t2iConfig.healthy }) }
  },

  // ============ Sandbox ============
  {
    method: 'GET', path: '/sandbox/config',
    handler() { return ok({ ...sandboxConfig }) }
  },
  {
    method: 'PUT', path: '/sandbox/config',
    handler({ body }) {
      sandboxConfig = { ...sandboxConfig, ...body }
      return ok({ ...sandboxConfig })
    }
  },
  {
    method: 'GET', path: '/sandbox/health',
    handler() { return ok({ healthy: sandboxConfig.healthy }) }
  },

  // ============ Webhook ============
  {
    method: 'GET', path: '/webhook/config',
    handler() { return ok({ ...webhookConfig }) }
  },
  {
    method: 'PUT', path: '/webhook/config',
    handler({ body }) {
      webhookConfig = { ...webhookConfig, ...body, running: body.enabled }
      return ok({ ...webhookConfig })
    }
  },

  // ============ Reply Strategy ============
  {
    method: 'GET', path: '/reply-strategy',
    handler() {
      return ok({ ...replyStrategyState })
    }
  },
  {
    method: 'PUT', path: '/reply-strategy',
    handler({ body }) {
      // 与服务端行为对齐：把请求绑定到零值策略上——只取请求里显式给出的字段，
      // 未提供的字段落回默认值，而不是浅合并旧状态（否则删掉的字段会残留）。
      // 数值字段严格解析：类型/非整数 → 40001（BindJSON），负数/越界 → 40032（范围）。
      const b = body || {}
      const quietGap = normInt(b.quiet_gap_seconds, 1, 30, 5, '安静间隔必须在 1-30 秒之间（0=默认 5s）')
      if (typeof quietGap !== 'number') return quietGap
      const forceCount = normInt(b.force_count, 2, 20, 5, '插话计数强发必须在 2-20 条之间（0=默认 5）')
      if (typeof forceCount !== 'number') return forceCount
      const maxAge = normInt(b.max_age_seconds, 5, 120, 20, '最迟必发必须在 5-120 秒之间（0=默认 20s）')
      if (typeof maxAge !== 'number') return maxAge
      const windowMax = normInt(b.window_max_msgs, 5, 50, 20, '窗口消息数上限必须在 5-50 条之间（0=默认 20）')
      if (typeof windowMax !== 'number') return windowMax
      // 随机抖动/概率/打字延迟：0 为合法值（关闭），仅做范围校验（与服务端一致）
      const jitter = rngInt(b.jitter_seconds, 0, 10, '随机抖动幅度必须在 0-10 秒之间')
      if (typeof jitter !== 'number') return jitter
      const forceJitter = rngInt(b.force_count_jitter, 0, 5, '计数抖动幅度必须在 0-5 之间')
      if (typeof forceJitter !== 'number') return forceJitter
      const prob = rngFloat(b.participate_probability, 0, 1, '参与概率必须在 0-1 之间')
      if (typeof prob !== 'number') return prob
      const typingDelay = rngInt(b.typing_delay_max_ms, 0, 5000, '打字延迟必须在 0-5000 毫秒之间')
      if (typeof typingDelay !== 'number') return typingDelay
      // 全量替换持久化状态：未提供的字段（bot_name/strip_markdown/agent_lite）落回零值，
      // 参与窗口整数参数 0=默认已由上方归一化。
      replyStrategyState = {
        bot_name: typeof b.bot_name === 'string' ? b.bot_name : '',
        strip_markdown: !!b.strip_markdown,
        agent_lite: !!b.agent_lite,
        quiet_gap_seconds: quietGap,
        force_count: forceCount,
        max_age_seconds: maxAge,
        window_max_msgs: windowMax,
        jitter_seconds: jitter,
        force_count_jitter: forceJitter,
        participate_probability: prob,
        typing_delay_max_ms: typingDelay,
      }
      return ok({ ...replyStrategyState })
    }
  },

  // ============ Logs ============
  {
    method: 'GET', path: '/logs',
    handler() {
      const levels = ['INFO', 'INFO', 'INFO', 'WARN', 'INFO', 'ERROR', 'INFO', 'DEBUG', 'WARN', 'INFO']
      const messages = [
        'Adapter started successfully on 0.0.0.0:8080',
        'WebSocket client connected: QQ=123456789',
        'Provider "OpenAI GPT-4o" health check passed',
        'Token usage approaching limit for session abc-123',
        'MCP server "File System" connected',
        'Failed to connect to sandbox: connection refused',
        'Plugin "weather-plugin" loaded successfully (v1.2.0)',
        'Memory compaction triggered for chat_area=def-456',
        'Rate limit warning: 80% of quota used',
        'Session expired, cleaning up Redis cache',
      ]
      return ok(levels.map((level, i) => ({
        time: new Date(Date.now() - (levels.length - i) * 30000).toISOString(),
        level,
        message: messages[i],
        attrs: i === 5 ? { error: 'ECONNREFUSED', address: 'localhost:8888' } : {},
      })))
    }
  },

  // ============ RAG 向量检索 ============
  {
    method: 'GET', path: '/rag/config',
    handler() {
      return ok({ base_url: ragConfig.base_url, timeout: ragConfig.timeout, is_active: ragConfig.is_active, healthy: ragHealthy })
    }
  },
  {
    method: 'PUT', path: '/rag/config',
    handler({ body }) {
      ragConfig = { base_url: body.base_url || 'http://localhost:3000', timeout: body.timeout || 30, is_active: body.is_active }
      ragHealthy = ragConfig.is_active
      return ok({ ...ragConfig, healthy: ragHealthy })
    }
  },
  {
    method: 'GET', path: '/rag/health',
    handler() { return ok({ healthy: ragHealthy }) }
  },
  {
    method: 'GET', path: '/rag/info',
    handler() {
      if (!ragHealthy) return ok({ ready: false, error: 'RAG 服务未启用' })
      return ok(ragInfo)
    }
  },

  // ============ 群管理 ============
  {
    method: 'GET', path: '/group-mgr/config',
    handler() {
      if (!groupMgrConfig) groupMgrConfig = { ...groupMgrDefaultConfig }
      return ok(groupMgrConfig)
    }
  },
  {
    method: 'PUT', path: '/group-mgr/config',
    handler({ body }) {
      groupMgrConfig = { ...groupMgrDefaultConfig, ...body }
      return ok(groupMgrConfig)
    }
  },
  {
    method: 'GET', path: '/group-mgr/words',
    handler({ query }) {
      const list = query.category
        ? groupMgrWords.filter((w) => w.category === query.category)
        : groupMgrWords
      return ok(list)
    }
  },
  {
    method: 'POST', path: '/group-mgr/words',
    handler({ body }) {
      const w = { id: ++groupMgrWordSeed, word: String(body.word).toLowerCase(), category: body.category, source: 'import', rag_synced: true, rag_tag: UUID() }
      groupMgrWords.push(w)
      return ok(null)
    }
  },
  {
    method: 'DELETE', path: '/group-mgr/words/:id',
    handler({ params }) {
      groupMgrWords = groupMgrWords.filter((w) => w.id !== Number(params.id))
      return ok(null)
    }
  },
  {
    method: 'POST', path: '/group-mgr/words/import',
    handler() { return ok({ imported: 3, skipped: 2 }) }
  },
  {
    method: 'POST', path: '/group-mgr/sync-rag',
    handler() { return ok({ total: groupMgrWords.length + groupMgrSamples.length, failed: 0 }) }
  },
  {
    // SSE 流式同步进度：逐批推 {done, failed}，结束推 {total, failed}
    method: 'GET', path: '/group-mgr/sync-rag/stream',
    handler() {
      if (!ragHealthy) return ok({ message: 'RAG-Service 未配置或不可达，同步失败' })
      const total = groupMgrWords.length + groupMgrSamples.length
      return {
        __sse: true,
        events: [
          { done: Math.min(5, total), failed: 0 },
          ...(total > 5 ? [{ done: total, failed: 0 }] : []),
          { total, failed: 0 },
        ],
      }
    }
  },
  {
    method: 'GET', path: '/group-mgr/samples',
    handler({ query }) {
      if (query.list_type === 'white') return ok(groupMgrSamples.filter((s) => s.list_type === 'white'))
      if (query.list_type === 'black') return ok(groupMgrSamples.filter((s) => s.list_type !== 'white'))
      return ok(groupMgrSamples)
    }
  },
  {
    method: 'DELETE', path: '/group-mgr/samples/:id',
    handler({ params }) {
      groupMgrSamples = groupMgrSamples.filter((s) => s.id !== Number(params.id))
      return ok(null)
    }
  },
  {
    method: 'POST', path: '/group-mgr/phrases',
    handler({ body }) {
      const listType = body.list_type === 'white' ? 'white' : 'black'
      const s = {
        id: ++groupMgrSampleSeed,
        word_id: 0,
        list_type: listType,
        text: String(body.text || ''),
        category: body.category === 'sensitive' ? 'sensitive' : 'ad',
        source: 'import',
        hit_count: 0,
        rag_synced: ragHealthy,
        rag_tag: ragHealthy ? UUID() : '',
        last_used_at: null,
        created_at: now(),
      }
      groupMgrSamples.push(s)
      return ok(null)
    }
  },
  {
    method: 'POST', path: '/group-mgr/phrases/import',
    handler({ query }) {
      const listType = query.list_type === 'white' ? 'white' : 'black'
      const lines = ['新导入语录A', '新导入语录B', '新导入语录C']
      for (const t of lines) {
        groupMgrSamples.push({
          id: ++groupMgrSampleSeed, word_id: 0, list_type: listType, text: t,
          category: 'ad', source: 'import', hit_count: 0,
          rag_synced: ragHealthy, rag_tag: ragHealthy ? UUID() : '',
          last_used_at: null, created_at: now(),
        })
      }
      return ok({ imported: 3, skipped: 2 })
    }
  },
  {
    method: 'GET', path: '/group-mgr/violations',
    handler() { return ok(groupMgrViolations) }
  },
  {
    method: 'DELETE', path: '/group-mgr/violations/:id',
    handler({ params }) {
      groupMgrViolations = groupMgrViolations.filter((v) => v.id !== Number(params.id))
      return ok(null)
    }
  },
  {
    method: 'GET', path: '/group-mgr/whitelist',
    handler() { return ok({ qq_list: groupMgrWhitelist }) }
  },
  {
    method: 'PUT', path: '/group-mgr/whitelist',
    handler({ body }) { groupMgrWhitelist = (body.qq_list || []).map(Number); return ok(null) }
  },
  {
    method: 'GET', path: '/group-mgr/admins',
    handler() { return ok({ qq_list: groupMgrAdmins }) }
  },
  {
    method: 'PUT', path: '/group-mgr/admins',
    handler({ body }) { groupMgrAdmins = (body.qq_list || []).map(Number); return ok(null) }
  },
  {
    method: 'POST', path: '/group-mgr/admins/sync-from-adapter',
    handler() {
      const adapterAdmins = [10001, 20000]
      let added = 0
      for (const qq of adapterAdmins) {
        if (!groupMgrAdmins.includes(qq)) { groupMgrAdmins.push(qq); added++ }
      }
      return ok({ added })
    }
  },
  {
    method: 'GET', path: '/group-mgr/stats',
    handler({ query }) {
      return ok({ group_id: Number(query.group_id) || 0, date: '2026-08-24', join_today: 3, warns: 2, mutes: 1, copy_warns: 0, ad: 5, sensitive: 1, kicks: 0 })
    }
  },
  {
    method: 'POST', path: '/group-mgr/test',
    handler({ body }) {
      const text = String(body.text || '')
      const keyword = ['卡', '群', '微信', '流量', '兼职', '贷款'].some((k) => text.includes(k))
      const hardSignal = keyword || text.includes('com.tencent.troopsharecard')
      // 黑白双集合判定：仿真实链路
      const blackPhrase = ['办卡', '流量卡', '0元购', '加微信'].find((p) => text.includes(p))
      const whitePhrase = ['食堂', '明天', '爬山'].find((p) => text.includes(p))
      const blackScore = blackPhrase ? 0.88 : null
      const whiteScore = whitePhrase ? 0.82 : null
      let verdict = 'pass', reason = ''
      if (ragHealthy && (blackScore ?? 0) >= (groupMgrConfig?.black_min_score ?? 0.7)) {
        verdict = 'punish'
        reason = `RAG 黑名单命中（分数 ${blackScore} ≥ ${groupMgrConfig?.black_min_score ?? 0.7}）→ 直接处罚`
      } else if (ragHealthy && (whiteScore ?? 0) >= (groupMgrConfig?.white_min_score ?? 0.75)) {
        verdict = 'pass'
        reason = `RAG 白名单命中（分数 ${whiteScore} ≥ ${groupMgrConfig?.white_min_score ?? 0.75}）→ 放行`
      } else if (ragHealthy) {
        verdict = 'review'
        reason = '未命中黑白名单 → LLM 统一判定（3s 批窗口，逐条独立）'
      } else if (hardSignal) {
        verdict = 'review'
        reason = 'RAG 不可用 → 关键词兜底（高危复核）'
      } else {
        verdict = 'pass'
        reason = 'RAG 不可用且无关键词命中 → 放行'
      }
      return ok({
        text,
        card: text.includes('com.tencent.troopsharecard'),
        word: keyword ? (text.match(/[卡群微信流量兼职贷款]/)?.[0] ?? '') : '',
        word_cat: keyword ? 'gray' : '',
        rag_ok: ragHealthy,
        black_score: blackScore,
        black_phrase: blackPhrase ?? '',
        white_score: whiteScore,
        white_phrase: whitePhrase ?? '',
        verdict,
        reason,
      })
    }
  },

  // ============ 知识库 ============
  {
    method: 'GET', path: '/knowledge',
    handler({ query }) {
      const list = knowledgeItems.slice(0, Number(query.page_size) || 20)
      return ok({ total: knowledgeItems.length, list })
    }
  },
  {
    method: 'POST', path: '/knowledge',
    handler({ body }) {
      const item = { id: UUID(), title: body.title, content: body.content, keywords: [], keyword_status: 'pending', created_at: now(), updated_at: now() }
      knowledgeItems.unshift(item)
      return ok(item)
    }
  },
  {
    method: 'PUT', path: '/knowledge/:id',
    handler({ params, body }) {
      const idx = knowledgeItems.findIndex((k) => k.id === params.id)
      if (idx === -1) return err(40400, '知识条目不存在')
      knowledgeItems[idx] = { ...knowledgeItems[idx], ...body, id: params.id, updated_at: now() }
      return ok(knowledgeItems[idx])
    }
  },
  {
    method: 'DELETE', path: '/knowledge/:id',
    handler({ params }) {
      knowledgeItems = knowledgeItems.filter((k) => k.id !== params.id)
      return ok(null)
    }
  },
  {
    method: 'POST', path: '/knowledge/vector-sync',
    handler() { return ok({ ready: ragHealthy, synced: knowledgeItems.length, failed: 0, total: knowledgeItems.length, message: ragHealthy ? '' : 'RAG 未启用，无法同步向量库' }) }
  },
  {
    // SSE 流式同步进度：逐批推 {done, failed}，结束推 {total, synced, failed}
    method: 'GET', path: '/knowledge/vector-sync/stream',
    handler() {
      if (!ragHealthy) return ok({ message: 'RAG 未启用，无法同步向量库' })
      const total = knowledgeItems.length
      return {
        __sse: true,
        events: [
          { done: Math.min(5, total), failed: 0 },
          ...(total > 5 ? [{ done: total, failed: 0 }] : []),
          { total, synced: total, failed: 0 },
        ],
      }
    }
  },

  // ============ 记忆同步 ============
  {
    method: 'POST', path: '/memory/sync-rag',
    handler() { return ok({ ready: ragHealthy, synced: 42, failed: 0, total: 42, message: ragHealthy ? '' : 'RAG 未启用，无法同步记忆向量' }) }
  },
  {
    // SSE 流式同步进度：逐批推 {done, failed}，结束推 {total, synced, failed}
    method: 'GET', path: '/memory/sync-rag/stream',
    handler() {
      if (!ragHealthy) return ok({ message: 'RAG 未启用，无法同步记忆向量' })
      const total = 42
      return {
        __sse: true,
        events: [
          { done: 10, failed: 0 },
          { done: 30, failed: 0 },
          { done: 42, failed: 0 },
          { total, synced: 42, failed: 0 },
        ],
      }
    }
  },
]