<template>
  <div>
    <div class="page-header">
      <div class="page-title"><v-icon class="me-2" color="primary">mdi-reply-circle</v-icon>系统回复设置</div>
      <div class="page-subtitle">控制机器人的回复行为与消息格式</div>
    </div>

    <v-row>
      <!-- 回复策略 -->
      <v-col cols="12" md="7">
        <v-card rounded="lg" elevation="1" class="h-100">
          <v-card-item>
            <template #title><span class="text-h6 font-weight-bold">回复策略</span></template>
            <template #subtitle>群聊/私聊消息路由方式</template>
          </v-card-item>
          <v-card-text>
            <v-form @submit.prevent="handleSave">
              <!-- 回复策略已收敛为仅支持按相关性回复：@/命令/提及名字必回，其余由 LLM 判断 -->
              <v-alert type="info" variant="tonal" class="mb-4" density="comfortable">
                <div class="text-subtitle-2 font-weight-bold">按相关性回复（当前唯一策略）</div>
                <div class="text-caption">
                  被@、触发插件命令或提及机器人名字时必回（规则快路径，不消耗 LLM 判断）；
                  其余群聊消息由 LLM 按相关性分数决定是否回复，判断结果带 Redis 缓存与冷却。
                </div>
              </v-alert>

              <div class="text-subtitle-2 font-weight-bold mb-2">相关性阈值</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model="form.relevance_threshold"
                  :min="0" :max="1" :step="0.05"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.relevance_threshold"
                  type="number" :min="0" :max="1" :step="0.05"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-2">
                Agent 判断消息相关性 &ge; 阈值时才会回复。被@时自动绕过此判断。
                <br />当前阈值: {{ form.relevance_threshold.toFixed(2) }}
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">机器人名字</div>
              <v-text-field
                v-model="form.bot_name"
                label="用于相关性判断，如「小卷」"
                placeholder="例如：小卷"
                density="comfortable" variant="outlined" hide-details clearable
              />

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">相关性检测模型</div>
              <v-select
                v-model="form.relevance_model"
                :items="textModelOptions"
                item-title="label"
                item-value="value"
                label="选择相关性判断使用的 Text 模型"
                placeholder="（默认 Text 模型）"
                density="comfortable" variant="outlined" clearable hide-details
              />

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">相关性判断超时（秒）</div>
              <v-text-field
                v-model.number="form.relevance_timeout"
                type="number" :min="1" :max="120"
                label="相关性判断 LLM 调用总超时（含等待，默认 10s）"
                density="comfortable" variant="outlined" hide-details
              />
              <div class="text-caption text-medium-emphasis mt-1">
                慢速提供商可调大到 15-30s；信号量等待与 LLM 调用共享该预算。
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">判断失败策略</div>
              <v-select
                v-model="form.judge_fail_policy"
                :items="[
                  { label: '不回复 — 判断失败时保持沉默（默认）', value: 'drop' },
                  { label: '照常回复 — 判断失败时交给 Agent 回复', value: 'reply' },
                ]"
                item-title="label"
                item-value="value"
                label="相关性判断 LLM 调用失败时的处理"
                density="comfortable" variant="outlined" hide-details
              />
              <div class="text-caption text-medium-emphasis mt-1">
                LLM 接口超时/限流等瞬态故障时生效；未配置模型不算失败。
              </div>

              <div class="d-flex align-center" style="gap: 12px">
                <v-btn type="submit" color="primary" variant="tonal" :loading="saving">
                  <v-icon class="me-1">mdi-content-save</v-icon> 保存
                </v-btn>
                <v-btn variant="tonal" color="info" @click="openPromptDialog">
                  <v-icon class="me-1">mdi-text-box-edit-outline</v-icon> 自定义判断提示词
                </v-btn>
              </div>
            </v-form>
          </v-card-text>
        </v-card>
      </v-col>

      <!-- 其他设置 -->
      <v-col cols="12" md="5">
        <v-card rounded="lg" elevation="1" class="h-100">
          <v-card-item>
            <template #title><span class="text-h6 font-weight-bold">其他设置</span></template>
            <template #subtitle>消息格式与 Agent 行为</template>
          </v-card-item>
          <v-card-text>
            <v-form @submit.prevent="handleSave">
              <!-- AgentLite -->
              <div class="d-flex align-start mb-4">
                <div class="flex-grow-1 me-3">
                  <div class="text-subtitle-2 font-weight-bold">AgentLite 模式</div>
                  <div class="text-caption text-medium-emphasis mt-1">
                    与正常模式一致，保留 ReAct 循环；仅禁用 MCP、沙箱和文生图工具。<br />
                    记忆、提示词、Skill 行为不受影响。
                  </div>
                </div>
                <v-switch v-model="form.agent_lite" color="primary" hide-details density="compact" />
              </div>

              <v-divider class="mb-4" />

              <!-- Strip Markdown -->
              <div class="d-flex align-start mb-4">
                <div class="flex-grow-1 me-3">
                  <div class="text-subtitle-2 font-weight-bold">去除 Markdown 格式</div>
                  <div class="text-caption text-medium-emphasis mt-1">
                    Agent 发送消息前去除加粗、斜体、代码块、链接等格式，发送纯文本到 QQ。
                  </div>
                </div>
                <v-switch v-model="form.strip_markdown" color="primary" hide-details density="compact" />
              </div>

              <v-btn type="submit" color="primary" variant="tonal" :loading="saving">
                <v-icon class="me-1">mdi-content-save</v-icon> 保存
              </v-btn>
            </v-form>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
    <!-- 自定义判断提示词弹窗 -->
    <v-dialog v-model="promptDialog" max-width="720">
      <v-card rounded="lg">
        <v-card-title class="d-flex align-center justify-space-between pa-4">
          <span>自定义判断提示词</span>
          <v-btn icon="mdi-close" size="small" variant="text" @click="promptDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4">
          <v-textarea
            v-model="promptDraft"
            label="留空使用默认规则；填写后替换默认的「回复规则」"
            rows="8"
            density="comfortable" variant="outlined"
            placeholder="例如：&#10;- 只有直接@机器人或请求机器人办事的消息才算相关&#10;- 群友闲聊一律不回复（相关度 < 0.1）"
          />
          <div class="text-caption text-medium-emphasis">
            自定义提示词将替换相关性判断的「回复规则」部分，消息上下文仍会自动附加。
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-4">
          <v-spacer />
          <v-btn variant="text" @click="promptDialog = false">取消</v-btn>
          <v-btn color="primary" variant="tonal" @click="savePrompt">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useToastStore } from '@/stores/toast'
import { replyStrategyApi, providerApi, type ProviderResp } from '@/api'

const toastStore = useToastStore()
const saving = ref(false)
const promptDialog = ref(false)
const promptDraft = ref('')

const form = ref({
  relevance_threshold: 0.5,
  bot_name: '',
  strip_markdown: false,
  agent_lite: false,
  relevance_prompt: '',
  relevance_model: '',
  relevance_timeout: 10,
  judge_fail_policy: 'drop',
})

// 相关性检测可选的 Text 模型列表（仅 text_model 类型）
const textModelOptions = ref<{ label: string; value: string }[]>([])

async function loadModels() {
  try {
    const list = (await providerApi.list()).data.data || []
    textModelOptions.value = list
      .filter((p: ProviderResp) => p.type === 'text_model')
      .map((p: ProviderResp) => ({ label: `${p.name} · ${p.model}`, value: p.id }))
  } catch { /* ignore */ }
}

async function load() {
  try {
    const res = await replyStrategyApi.get()
    const d = (res.data as any)?.data
    if (d) {
      form.value.relevance_threshold = d.relevance_threshold ?? 0.5
      form.value.bot_name = d.bot_name || ''
      form.value.strip_markdown = d.strip_markdown || false
      form.value.agent_lite = d.agent_lite || false
      form.value.relevance_prompt = d.relevance_prompt || ''
      form.value.relevance_model = d.relevance_model || ''
      form.value.relevance_timeout = d.relevance_timeout || 10
      form.value.judge_fail_policy = d.judge_fail_policy || 'drop'
    }
  } catch (_e: any) {}
}

function openPromptDialog() {
  promptDraft.value = form.value.relevance_prompt
  promptDialog.value = true
}

function savePrompt() {
  form.value.relevance_prompt = promptDraft.value
  promptDialog.value = false
  toastStore.success('提示词已更新，点击「保存」生效')
}

async function handleSave() {
  saving.value = true
  try {
    await replyStrategyApi.update({
      relevance_threshold: form.value.relevance_threshold,
      bot_name: form.value.bot_name,
      strip_markdown: form.value.strip_markdown,
      agent_lite: form.value.agent_lite,
      relevance_prompt: form.value.relevance_prompt,
      relevance_model: form.value.relevance_model,
      relevance_timeout: form.value.relevance_timeout || 10,
      judge_fail_policy: form.value.judge_fail_policy,
    })
    toastStore.success('回复设置已保存')
  } catch (e: any) {
    toastStore.error(e?.response?.data?.info || e?.message || '保存失败')
  } finally { saving.value = false }
}

onMounted(() => { load(); loadModels() })
</script>
