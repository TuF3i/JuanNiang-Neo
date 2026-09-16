<template>
  <div>
    <div class="page-header">
      <div class="page-title">Provider 管理</div>
      <div class="page-subtitle">管理 LLM 提供商配置（同类型仅一个 Active）</div>
    </div>
    <div class="d-flex justify-end mb-4">
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openChooser">新增 Provider</v-btn>
    </div>
    <v-data-table :headers="headers" :items="items" :loading="loading" items-per-page="20">
      <template #item.type="{ item }"><v-chip size="small" variant="tonal">{{ typeLabel(item.type) }}</v-chip></template>
      <template #item.api_mode="{ item }">
        <v-chip size="small" variant="tonal">{{ apiModeLabel(item.api_mode) }}</v-chip>
      </template>
      <template #item.enable_thinking="{ item }">
        <v-chip size="small" :color="thinkingOn(item) ? 'primary' : 'default'" variant="tonal">{{ thinkingOn(item) ? '开' : '关' }}</v-chip>
      </template>
      <template #item.is_active="{ item }">
        <v-switch :model-value="item.is_active" color="primary" density="compact" hide-details @update:model-value="(v) => toggle(item.id, !!v)" />
      </template>
      <template #item.actions="{ item }">
        <v-btn icon="mdi-sync" size="small" variant="text" color="secondary" title="测试连接" :loading="testingId === item.id" @click="testItem(item)" />
        <v-btn icon="mdi-pencil" size="small" variant="text" color="primary" @click="openEdit(item)" />
        <v-btn icon="mdi-delete" size="small" variant="text" color="error" @click="confirmDelete(item)" />
      </template>
    </v-data-table>

    <!-- ========== 第一步：选择提供商（卡片） ========== -->
    <v-dialog v-model="chooserDialog" max-width="820">
      <v-card rounded="lg">
        <v-card-title class="py-4">
          <v-icon class="me-2" color="primary">mdi-label-multiple-outline</v-icon>选择提供商
        </v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-3">
            选择厂商预设可自动填入协议与 API 地址（稍后可修改），或使用自定义 Provider 手动配置全部参数。
          </div>
          <v-row dense>
            <v-col v-for="p in presets" :key="p.key" cols="6" sm="4">
              <v-card variant="outlined" hover class="preset-card rounded-lg" @click="choosePreset(p)">
                <div class="d-flex align-center pa-4">
                  <v-avatar :color="presetLogo(p.key) && !logoMono(p.key) ? undefined : brandColor(p.key)" size="42" class="me-3" :class="{ 'logo-mono-bg': presetLogo(p.key) && logoMono(p.key) }">
                    <img v-if="presetLogo(p.key)" :src="presetLogo(p.key)" :alt="p.name" class="provider-logo" />
                    <span v-else class="text-h6 font-weight-bold text-white">{{ avatarText(p.name) }}</span>
                  </v-avatar>
                  <div class="min-w-0">
                    <div class="text-subtitle-1 font-weight-bold text-truncate">{{ p.name }}</div>
                    <div class="text-caption text-medium-emphasis">{{ p.protocols.length }} 种协议</div>
                  </div>
                </div>
              </v-card>
            </v-col>
            <v-col cols="6" sm="4">
              <v-card variant="outlined" hover class="preset-card rounded-lg custom-card" @click="chooseCustom">
                <div class="d-flex align-center pa-4">
                  <v-avatar color="surface-variant" size="42" class="me-3">
                    <v-icon icon="mdi-tune-variant" />
                  </v-avatar>
                  <div>
                    <div class="text-subtitle-1 font-weight-bold">自定义 Provider</div>
                    <div class="text-caption text-medium-emphasis">全部参数与协议</div>
                  </div>
                </div>
              </v-card>
            </v-col>
          </v-row>
        </v-card-text>
        <v-card-actions class="pa-4 pt-0">
          <v-spacer />
          <v-btn variant="text" @click="chooserDialog = false">取消</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- ========== 第二步：配置表单 ========== -->
    <v-dialog v-model="formDialog" max-width="860">
      <v-card rounded="lg">
        <v-card-title class="d-flex align-center py-4">
          <img
            v-if="formLogo"
            :src="formLogo.src"
            :alt="currentPreset?.name || form.provider_key"
            class="provider-logo me-2"
            :class="{ 'logo-mono-bg': logoMono(form.provider_key) }"
          />
          <span class="me-auto text-truncate">
            {{ editing ? '编辑 Provider' : (isCustom ? '自定义 Provider' : `配置 ${currentPreset?.name ?? ''}`) }}
          </span>
          <v-btn variant="tonal" color="secondary" prepend-icon="mdi-sync" :loading="testing" @click="testForm">测试连接</v-btn>
        </v-card-title>
        <v-card-text>
          <v-form ref="formRef">
            <!-- 基本信息 -->
            <div class="text-subtitle-2 mb-2">基本信息</div>
            <v-row>
              <v-col cols="12" sm="6">
                <v-text-field
                  v-model="form.name"
                  label="名称"
                  :rules="nameRules"
                  :hint="isCustom ? '输入常见厂商名可自动匹配思考参数矩阵' : ''"
                  persistent-hint
                  @update:model-value="onNameChange"
                />
              </v-col>
              <v-col cols="12" sm="6">
                <v-select v-model="form.type" :items="types" label="类型" />
              </v-col>
              <v-col cols="12">
                <v-switch
                  v-model="form.isActive"
                  label="激活"
                  color="primary"
                  density="compact"
                  hint="同类型仅一个 Active"
                  persistent-hint
                />
              </v-col>
            </v-row>

            <v-divider class="my-3" />
            <!-- 协议与连接 -->
            <div class="text-subtitle-2 mb-2">协议与连接</div>
            <v-row>
              <v-col cols="12" sm="6">
                <v-select
                  v-model="protocolModel"
                  :items="protocolItems"
                  label="协议"
                  @update:model-value="onProtocolChange"
                />
              </v-col>
              <v-col cols="12" sm="6">
                <v-select
                  v-model="form.auth_header"
                  :items="authHeaders"
                  label="认证头"
                  clearable
                  hint="空 = 按协议默认"
                  persistent-hint
                />
              </v-col>
            </v-row>
            <v-alert
              v-if="presetNote"
              type="warning"
              variant="tonal"
              density="compact"
              class="mb-3"
            >{{ presetNote }}</v-alert>
            <v-row>
              <v-col cols="12">
                <v-text-field
                  v-model="form.endpoint"
                  label="API 地址"
                  :rules="endpointRules"
                  hint="原样使用；填 base URL 时自动补协议后缀，以协议后缀结尾则按你填的原样请求"
                  persistent-hint
                />
              </v-col>
            </v-row>
            <v-row>
              <v-col cols="12">
                <v-text-field v-model="form.token" label="Api-Key" type="password" />
              </v-col>
            </v-row>

            <v-divider class="my-3" />
            <!-- 模型 -->
            <div class="text-subtitle-2 mb-2">模型</div>
            <v-row>
              <v-col cols="12" sm="9">
                <v-combobox
                  v-model="form.model"
                  :items="modelItems"
                  label="模型名称"
                  hint="可手动输入，或点击右侧「获取模型列表」从 API 拉取后选择；获取失败可留空稍后填写"
                  persistent-hint
                  density="comfortable"
                />
              </v-col>
              <v-col cols="12" sm="3" class="d-flex align-center">
                <v-btn
                  variant="tonal"
                  color="secondary"
                  prepend-icon="mdi-cloud-download-outline"
                  :loading="fetchingModels"
                  @click="fetchModels"
                >获取模型列表</v-btn>
              </v-col>
            </v-row>

            <v-expansion-panels v-model="expandedPanels" variant="accordion" multiple class="mt-1">
              <v-expansion-panel v-if="show.thinking" value="thinking">
                <v-expansion-panel-title>
                  <div class="d-flex align-center ga-2">
                    <span class="text-subtitle-2">思考（Thinking）</span>
                    <v-chip size="x-small" variant="tonal" :color="form.thinking_effort !== 'off' ? 'primary' : 'default'">
                      {{ thinkingEffortLabel }}
                    </v-chip>
                  </div>
                </v-expansion-panel-title>
                <v-expansion-panel-text>
                  <v-row>
                    <v-col cols="12" sm="6" md="4">
                      <v-select v-model="form.thinking_effort" :items="thinkingEfforts" label="思考强度" />
                    </v-col>
                    <v-col cols="12" sm="6" md="4">
                      <v-text-field v-model="form.thinking_budget" label="Thinking Budget（0=默认）" type="number" />
                    </v-col>
                  </v-row>
                  <div class="text-caption text-medium-emphasis">
                    思考强度支持 关/低/中/高，按厂商矩阵适配（DeepSeek/智谱/Kimi/通义/阶跃/MiniMax 等）。
                  </div>
                </v-expansion-panel-text>
              </v-expansion-panel>
              <v-expansion-panel value="advanced">
                <v-expansion-panel-title>
                  <div class="d-flex align-center ga-2">
                    <span class="text-subtitle-2">高级设置</span>
                    <span class="text-caption text-medium-emphasis">
                      温度 {{ form.temperature ?? 0.7 }}<template v-if="advancedCount"> · 已配置 {{ advancedCount }} 项采样参数</template>
                    </span>
                  </div>
                </v-expansion-panel-title>
                <v-expansion-panel-text>
                  <v-row>
                    <v-col cols="12" sm="6" md="4">
                      <v-text-field v-model="form.temperature" label="温度" type="number" step="0.1" />
                    </v-col>
                    <v-col cols="12" sm="6" md="4">
                      <v-text-field v-model="form.max_tokens" label="Max Tokens（0=默认）" type="number" />
                    </v-col>
                  </v-row>
                  <v-row>
                    <v-col v-if="show.topP" cols="12" sm="6" md="4">
                      <v-text-field v-model="form.top_p" label="Top P" type="number" step="0.05" />
                    </v-col>
                    <v-col v-if="show.topK" cols="12" sm="6" md="4">
                      <v-text-field v-model="form.top_k" label="Top K" type="number" />
                    </v-col>
                    <v-col v-if="show.freqPresence" cols="12" sm="6" md="4">
                      <v-text-field v-model="form.frequency_penalty" label="Frequency Penalty" type="number" step="0.1" />
                    </v-col>
                    <v-col v-if="show.freqPresence" cols="12" sm="6" md="4">
                      <v-text-field v-model="form.presence_penalty" label="Presence Penalty" type="number" step="0.1" />
                    </v-col>
                    <v-col v-if="show.repetition" cols="12" sm="6" md="4">
                      <v-text-field v-model="form.repetition_penalty" label="Repetition Penalty" type="number" step="0.1" />
                    </v-col>
                  </v-row>
                </v-expansion-panel-text>
              </v-expansion-panel>
            </v-expansion-panels>
          </v-form>

          <!-- 测试结果状态 -->
          <v-alert v-if="testResult !== null" :type="testResult.ok ? 'success' : 'error'" variant="tonal" class="mt-3" dense>
            <template #prepend><v-icon>{{ testResult.ok ? 'mdi-check-circle' : 'mdi-alert-circle' }}</v-icon></template>
            <div class="text-caption">{{ testResult.message }}</div>
          </v-alert>
        </v-card-text>
        <v-card-actions class="pa-4 pt-0">
          <v-btn v-if="cameFromChooser" variant="text" prepend-icon="mdi-arrow-left" @click="backToChooser">返回选择</v-btn>
          <v-spacer />
          <v-btn variant="text" @click="formDialog = false">取消</v-btn>
          <v-btn color="primary" variant="tonal" @click="handleSave" :loading="saving">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Delete confirm -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card rounded="lg">
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除此 Provider 吗？此操作不可撤销。</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" variant="tonal" @click="handleDelete" :loading="deleting">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { providerApi, providerPresetsApi, type ProviderResp, type ProviderPreset, type AddProviderReq, type TestProviderResp, type ProviderModelsResp } from '@/api'
import { useToastStore } from '@/stores/toast'

// 厂商 Logo（@lobehub/icons-static-svg 本地打包，无运行时 CDN 依赖）
import deepseekLogo from '@lobehub/icons-static-svg/icons/deepseek-color.svg'
import zhipuLogo from '@lobehub/icons-static-svg/icons/zhipu-color.svg'
import kimiLogo from '@lobehub/icons-static-svg/icons/kimi-color.svg'
import alibabaLogo from '@lobehub/icons-static-svg/icons/alibaba-color.svg'
import volcengineLogo from '@lobehub/icons-static-svg/icons/volcengine-color.svg'
import minimaxLogo from '@lobehub/icons-static-svg/icons/minimax-color.svg'
import xiaomimimoLogo from '@lobehub/icons-static-svg/icons/xiaomimimo.svg'
import stepfunLogo from '@lobehub/icons-static-svg/icons/stepfun-color.svg'
import hunyuanLogo from '@lobehub/icons-static-svg/icons/hunyuan-color.svg'
import baiduLogo from '@lobehub/icons-static-svg/icons/baidu-color.svg'
import siliconcloudLogo from '@lobehub/icons-static-svg/icons/siliconcloud-color.svg'
import openaiLogo from '@lobehub/icons-static-svg/icons/openai.svg'
import claudeLogo from '@lobehub/icons-static-svg/icons/claude-color.svg'
import geminiLogo from '@lobehub/icons-static-svg/icons/gemini-color.svg'

const toastStore = useToastStore()
const loading = ref(true)
const items = ref<ProviderResp[]>([])
const chooserDialog = ref(false)
const formDialog = ref(false)
const cameFromChooser = ref(false)
const currentPreset = ref<ProviderPreset | null>(null)
const isCustom = ref(false)
const presetNote = ref('')
// 协议选择：预设 = protocols 下标；自定义 = api_mode 字符串
const protocolModel = ref<number | string | null>(null)
const deleteDialog = ref(false)
const editing = ref<string | null>(null)
const saving = ref(false)
const deleting = ref(false)
const testing = ref(false)
const testingId = ref<string | null>(null)
const testResult = ref<TestProviderResp | null>(null)
const deleteTarget = ref<ProviderResp | null>(null)
const formRef = ref()

const presets = ref<ProviderPreset[]>([])

// 模型列表：fetch 结果 + 预设常用模型建议合并为 combobox 候选
const modelOptions = ref<string[]>([])
const fetchingModels = ref(false)

const headers = [
  { title: '名称', key: 'name' },
  { title: '类型', key: 'type' },
  { title: '模型', key: 'model' },
  { title: '协议', key: 'api_mode', align: 'center' as const },
  { title: '端点', key: 'endpoint' },
  { title: '思考', key: 'enable_thinking', align: 'center' as const },
  { title: 'Active', key: 'is_active', align: 'center' as const },
  { title: '操作', key: 'actions', align: 'center' as const, sortable: false },
]

const types = [
  { title: 'Text Model', value: 'text_model' },
  { title: 'Image Model', value: 'image_model' },
  { title: 'Embedding Model', value: 'embedding_model' },
]

const apiModes = [
  { title: 'OpenAI 兼容 (chat_completions)', value: 'chat_completions' },
  { title: 'Anthropic Messages', value: 'anthropic_messages' },
  { title: 'OpenAI Responses', value: 'openai_responses' },
  { title: 'Gemini Native', value: 'gemini_native' },
]

const authHeaders = [
  { title: 'Bearer', value: 'bearer' },
  { title: 'x-api-key', value: 'x-api-key' },
  { title: 'api-key', value: 'api-key' },
]

const thinkingEfforts = [
  { title: '关闭', value: 'off' },
  { title: '低', value: 'low' },
  { title: '中', value: 'medium' },
  { title: '高', value: 'high' },
]

// 厂商品牌色（预设卡片 Avatar；无图标的轻量方案）
const brandColors: Record<string, string> = {
  deepseek: '#4D6BFE',
  zhipu: '#2454FF',
  kimi: '#2A2F3A',
  alibaba: '#615CEF',
  volcengine: '#1664FF',
  minimax: '#E1483F',
  xiaomi: '#FF6900',
  stepfun: '#0F7B6C',
  tencent: '#0052D9',
  baidu: '#2932E1',
  siliconflow: '#7C3AED',
  openai: '#10A37F',
  anthropic: '#D97757',
  gemini: '#4285F4',
}
const brandColor = (key: string) => brandColors[key] || '#607D8B'
const avatarText = (name: string) => name.trim().charAt(0).toUpperCase() || '?'

// provider_key → Logo（mono=单色 SVG，暗色主题下垫白色底避免不可见）
const logoSrc: Record<string, { src: string; mono?: boolean }> = {
  deepseek: { src: deepseekLogo },
  zhipu: { src: zhipuLogo },
  kimi: { src: kimiLogo },
  alibaba: { src: alibabaLogo },
  volcengine: { src: volcengineLogo },
  minimax: { src: minimaxLogo },
  xiaomi: { src: xiaomimimoLogo, mono: true },
  stepfun: { src: stepfunLogo },
  tencent: { src: hunyuanLogo },
  baidu: { src: baiduLogo },
  siliconflow: { src: siliconcloudLogo },
  openai: { src: openaiLogo, mono: true },
  anthropic: { src: claudeLogo },
  gemini: { src: geminiLogo },
}
const presetLogo = (key: string) => logoSrc[key]?.src || ''
const logoMono = (key: string) => !!logoSrc[key]?.mono

// 各厂商常用模型（获取失败时的下拉候选，可手动输入任意值）
const suggestModels: Record<string, string[]> = {
  deepseek: ['deepseek-chat', 'deepseek-reasoner'],
  zhipu: ['glm-4.7', 'glm-4.7-air', 'glm-4.5'],
  kimi: ['kimi-k2-0905-preview', 'kimi-k2-turbo-preview', 'moonshot-v1-128k'],
  alibaba: ['qwen3-max', 'qwen-plus', 'qwen-turbo'],
  volcengine: ['doubao-seed-1-6', 'doubao-1-5-pro-32k-250715'],
  minimax: ['MiniMax-M2', 'abab6.5s-chat'],
  xiaomi: ['MiMo-V2'],
  stepfun: ['step-3', 'step-2-16k'],
  tencent: ['hunyuan-turbos-latest', 'hunyuan-large'],
  baidu: ['ernie-5.0', 'ernie-4.5-turbo'],
  siliconflow: ['Qwen/Qwen3-32B', 'deepseek-ai/DeepSeek-V3.2'],
  openai: ['gpt-4o', 'gpt-4o-mini'],
  anthropic: ['claude-sonnet-4-5', 'claude-opus-4-1'],
  gemini: ['gemini-2.5-pro', 'gemini-2.5-flash'],
}

const typeLabel = (t: string) => ({ text_model: 'Text', image_model: 'Image', embedding_model: 'Embedding' }[t] || t)
const apiModeLabel = (m: string) => ({ chat_completions: 'OpenAI', anthropic_messages: 'Anthropic', openai_responses: 'Responses', gemini_native: 'Gemini' }[m] || m || 'OpenAI')
const thinkingOn = (item: ProviderResp) => item.thinking_effort === 'off' ? false : (item.enable_thinking || item.thinking_effort !== '')

// 协议下拉：预设 = 该厂商的协议列表（下标为值）；自定义 = 全部协议
const protocolItems = computed(() => {
  if (isCustom.value || !currentPreset.value) return apiModes
  return currentPreset.value.protocols.map((p, i) => ({ title: apiModeLabel(p.api_mode), value: i }))
})

// 按协议模式 + 厂商参数化各配置项是否适用；思考区仅 Text 模型显示
const show = computed(() => {
  const mode = form.value.api_mode
  const pk = form.value.provider_key
  const anthro = mode === 'anthropic_messages'
  const gemini = mode === 'gemini_native'
  const resp = mode === 'openai_responses'
  const chat = mode === 'chat_completions'
  return {
    thinking: form.value.type === 'text_model',
    topP: !resp,
    topK: anthro || gemini || pk === 'minimax',
    freqPresence: chat || resp,
    repetition: gemini || pk === 'minimax' || pk === 'xiaomi',
  }
})

const modelItems = computed(() =>
  Array.from(new Set([...modelOptions.value, ...(suggestModels[form.value.provider_key] || [])])),
)

// 配置表单标题 Logo：预设厂商或自定义时已识别的 provider_key
const formLogo = computed(() => {
  const key = currentPreset.value?.key || form.value.provider_key
  return key && presetLogo(key) ? { src: presetLogo(key) } : null
})

const requiredRule = (v: unknown) => (v !== null && v !== undefined && String(v).trim() !== '') || '必填项'
const nameRules = [requiredRule]
const endpointRules = [requiredRule]

// 折叠面板：思考/高级设置默认收起；编辑已开启思考的 Provider 时自动展开思考面板
const expandedPanels = ref<string[]>([])
const thinkingEffortLabel = computed(
  () => thinkingEfforts.find((t) => t.value === form.value.thinking_effort)?.title ?? form.value.thinking_effort,
)
const advancedCount = computed(
  () => [form.value.top_p, form.value.top_k, form.value.frequency_penalty, form.value.presence_penalty, form.value.repetition_penalty]
    .filter((v) => v !== null && v !== undefined).length,
)

const defaultForm = (): AddProviderReq => ({
  name: '', type: 'text_model', endpoint: '', token: '', model: '', temperature: 0.7,
  isActive: false, enable_thinking: false, api_mode: 'chat_completions', thinking_effort: 'off',
  thinking_budget: 0, max_tokens: 0, top_p: null, top_k: null, frequency_penalty: null,
  presence_penalty: null, repetition_penalty: null, provider_key: '', auth_header: '', url_mode: 'auto',
})
const form = ref<AddProviderReq>(defaultForm())

async function fetch() {
  loading.value = true
  try { items.value = (await providerApi.list()).data.data } catch (e: any) { toastStore.error('获取列表失败') } finally { loading.value = false }
}

async function fetchPresets() {
  try { presets.value = (await providerPresetsApi.list()).data.data } catch (e: any) { /* 预设可选，失败静默 */ }
}

// ---------- 打开流程 ----------

function openChooser() {
  editing.value = null
  currentPreset.value = null
  isCustom.value = false
  form.value = defaultForm()
  protocolModel.value = null
  presetNote.value = ''
  modelOptions.value = []
  testResult.value = null
  chooserDialog.value = true
}

function choosePreset(p: ProviderPreset) {
  editing.value = null
  isCustom.value = false
  currentPreset.value = p
  expandedPanels.value = []
  form.value = { ...defaultForm(), provider_key: p.key }
  modelOptions.value = []
  testResult.value = null
  chooserDialog.value = false
  cameFromChooser.value = true
  formDialog.value = true
  // 默认带出第一个协议（填协议/地址/认证头）
  if (p.protocols.length) {
    protocolModel.value = 0
    onProtocolChange()
  } else {
    protocolModel.value = null
  }
}

function chooseCustom() {
  editing.value = null
  isCustom.value = true
  currentPreset.value = null
  expandedPanels.value = []
  form.value = defaultForm()
  modelOptions.value = []
  presetNote.value = ''
  protocolModel.value = form.value.api_mode
  testResult.value = null
  chooserDialog.value = false
  cameFromChooser.value = true
  formDialog.value = true
}

function openEdit(item: ProviderResp) {
  editing.value = item.id
  cameFromChooser.value = false
  form.value = {
    name: item.name, type: item.type, endpoint: item.endpoint, token: item.token, model: item.model,
    temperature: item.temperature, isActive: item.is_active, enable_thinking: item.enable_thinking,
    api_mode: item.api_mode || 'chat_completions', thinking_effort: item.thinking_effort || 'off',
    thinking_budget: item.thinking_budget || 0, max_tokens: item.max_tokens || 0,
    top_p: item.top_p, top_k: item.top_k, frequency_penalty: item.frequency_penalty,
    presence_penalty: item.presence_penalty, repetition_penalty: item.repetition_penalty,
    provider_key: item.provider_key || '', auth_header: item.auth_header || '', url_mode: item.url_mode || 'auto',
  }
  currentPreset.value = presets.value.find((p) => p.key === item.provider_key) || null
  isCustom.value = !currentPreset.value
  if (currentPreset.value) {
    const idx = currentPreset.value.protocols.findIndex((p) => p.api_mode === (item.api_mode || 'chat_completions'))
    protocolModel.value = idx >= 0 ? idx : null
    presetNote.value = idx >= 0 ? (currentPreset.value.protocols[idx].note || '') : ''
  } else {
    protocolModel.value = item.api_mode || 'chat_completions'
    presetNote.value = ''
  }
  modelOptions.value = item.model ? [item.model] : []
  expandedPanels.value = (item.thinking_effort || 'off') !== 'off' ? ['thinking'] : []
  testResult.value = null
  formDialog.value = true
}

function backToChooser() {
  formDialog.value = false
  openChooser()
}

// ---------- 表单交互 ----------

// 名称关键词命中预设厂商时自动带出 provider_key（仅自定义模式，便于思考矩阵生效）。
function onNameChange(v: string | null) {
  if (!v || editing.value || !isCustom.value) return
  const n = v.toLowerCase()
  const match = presets.value.find((p) => n.includes(p.key) || n.includes(p.name.toLowerCase()))
  if (match) form.value.provider_key = match.key
}

// 协议切换：预设自动回填 api_mode / API 地址 / 认证头（均可再手动覆盖）；自定义仅切换协议
function onProtocolChange() {
  if (!isCustom.value && currentPreset.value && typeof protocolModel.value === 'number') {
    const proto = currentPreset.value.protocols[protocolModel.value]
    if (proto) {
      form.value.api_mode = proto.api_mode
      form.value.endpoint = proto.base_url
      form.value.auth_header = proto.auth_header
      presetNote.value = proto.note || ''
    }
    return
  }
  if (typeof protocolModel.value === 'string') {
    form.value.api_mode = protocolModel.value
    presetNote.value = ''
  }
}

// 拉取模型列表：走后端代理 POST /providers/models（绕开浏览器 CORS，无需暴露 Key 给浏览器跨域请求）
async function fetchModels() {
  const ep = (form.value.endpoint || '').trim()
  if (!ep) { toastStore.warning('请先填写 API 地址'); return }
  fetchingModels.value = true
  try {
    const res = (await providerApi.listModels({
      endpoint: ep,
      token: form.value.token || '',
      auth_header: form.value.auth_header || '',
      api_mode: form.value.api_mode,
    })).data.data as ProviderModelsResp
    if (res.ok && res.models.length) {
      modelOptions.value = res.models
      toastStore.success(res.message || `已获取 ${res.models.length} 个模型`)
    } else {
      toastStore.warning(res.message || 'API 未返回模型列表，可手动输入模型名')
    }
  } catch (e: any) {
    toastStore.warning(e?.message || '获取模型列表失败，可手动输入模型名')
  } finally {
    fetchingModels.value = false
  }
}

// ---------- 列表操作 ----------

async function toggle(id: string, v: boolean) {
  try {
    await providerApi.toggle(id, v)
    toastStore.success(v ? '已启用' : '已停用')
    await fetch()
  } catch (e: any) { toastStore.error('操作失败') }
}

async function handleSave() {
  const valid = await formRef.value?.validate?.()
  if (valid && valid.valid === false) return
  saving.value = true
  try {
    // 旧开关由思考强度推导，UI 不再单独暴露
    form.value.enable_thinking = form.value.thinking_effort !== 'off'
    if (editing.value) { await providerApi.update(editing.value, form.value) } else { await providerApi.create(form.value) }
    toastStore.success(editing.value ? '已更新' : '已创建')
    formDialog.value = false
    await fetch()
  } catch (e: any) { toastStore.error(e?.message || '保存失败') } finally { saving.value = false }
}

function reqFromItem(item: ProviderResp): AddProviderReq {
  return {
    name: item.name, type: item.type, endpoint: item.endpoint, token: item.token, model: item.model,
    temperature: item.temperature, isActive: item.is_active, enable_thinking: item.enable_thinking,
    api_mode: item.api_mode || 'chat_completions', thinking_effort: item.thinking_effort || 'off',
    thinking_budget: item.thinking_budget || 0, max_tokens: item.max_tokens || 0,
    top_p: item.top_p, top_k: item.top_k, frequency_penalty: item.frequency_penalty,
    presence_penalty: item.presence_penalty, repetition_penalty: item.repetition_penalty,
    provider_key: item.provider_key || '', auth_header: item.auth_header || '', url_mode: item.url_mode || 'auto',
  }
}

async function runTest(payload: AddProviderReq) {
  testing.value = true
  try {
    const res = (await providerApi.test(payload)).data.data as TestProviderResp
    testResult.value = res
    if (res.ok) {
      toastStore.success('连接成功')
    } else {
      toastStore.error(res.message || '连接失败')
    }
  } catch (e: any) {
    testResult.value = { ok: false, message: e?.message || '测试失败' }
    toastStore.error('测试失败')
  } finally { testing.value = false }
}

// 测试新增/编辑表单中的当前配置（不落库）。
function testForm() { runTest({ ...form.value }) }

// 测试列表中已保存的 Provider。
async function testItem(item: ProviderResp) {
  testingId.value = item.id
  try {
    const res = (await providerApi.test(reqFromItem(item))).data.data as TestProviderResp
    if (res.ok) {
      toastStore.success('连接成功')
    } else {
      toastStore.error(res.message || '连接失败')
    }
  } catch (e: any) {
    toastStore.error(e?.message || '测试失败')
  } finally { testingId.value = null }
}

function confirmDelete(item: ProviderResp) { deleteTarget.value = item; deleteDialog.value = true }
async function handleDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try { await providerApi.delete(deleteTarget.value.id); toastStore.success('已删除'); deleteDialog.value = false; await fetch() } catch (e: any) { toastStore.error('删除失败') } finally { deleting.value = false }
}

onMounted(() => { fetch(); fetchPresets() })
</script>

<style scoped>
.provider-logo {
  width: 26px;
  height: 26px;
  object-fit: contain;
}
.logo-mono-bg {
  background: #ffffff;
}
.preset-card {
  cursor: pointer;
  transition: border-color 0.15s ease;
}
.preset-card:hover {
  border-color: rgb(var(--v-theme-primary));
}
.custom-card {
  border-style: dashed;
  border-width: 1.5px;
}
.min-w-0 {
  min-width: 0;
}
</style>
