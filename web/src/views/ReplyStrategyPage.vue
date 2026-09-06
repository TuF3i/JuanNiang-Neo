<template>
  <div>
    <div class="page-header">
      <div class="page-title"><v-icon class="me-2" color="primary">mdi-reply-circle</v-icon>系统回复设置</div>
      <div class="page-subtitle">控制机器人的回复行为与消息格式</div>
    </div>

    <v-row>
      <!-- 参与窗口 -->
      <v-col cols="12" md="7">
        <v-card rounded="lg" elevation="1" class="h-100">
          <v-card-item>
            <template #title><span class="text-h6 font-weight-bold">参与窗口</span></template>
            <template #subtitle>群聊消息攒窗后整窗参与，不逐条回复</template>
          </v-card-item>
          <v-card-text>
            <v-form @submit.prevent="handleSave">
              <v-alert type="info" variant="tonal" class="mb-4" density="comfortable">
                <div class="text-subtitle-2 font-weight-bold">参与模式（当前唯一策略）</div>
                <div class="text-caption">
                  被@、触发插件命令或提及机器人名字时必回（立即回复，不攒窗）；其余群聊消息攒进窗口，
                  等待「安静间隔」或攒够「插话计数」后，整窗一次交给 Agent，由 Agent 决定附和/接话或静默（__NO_REPLY__）。
                </div>
              </v-alert>

              <div class="text-subtitle-2 font-weight-bold mb-2">机器人名字</div>
              <v-text-field
                v-model="form.bot_name"
                label="提及该名字即必回，如「小卷」"
                placeholder="例如：小卷"
                density="comfortable" variant="outlined" hide-details clearable
              />

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">安静间隔（秒）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.quiet_gap_seconds"
                  :min="1" :max="30" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.quiet_gap_seconds"
                  type="number" :min="1" :max="30"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                消息停止后等待这么久才释放窗口；窗口内每来一条新消息都会重置该计时。
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">插话计数强发（条）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.force_count"
                  :min="2" :max="20" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.force_count"
                  type="number" :min="2" :max="20"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                一直有人插话导致窗口无法安静释放时，攒够该条数后忽略定时器强制开口（保证不被冷落）。
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">最迟必发（秒）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.max_age_seconds"
                  :min="5" :max="120" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.max_age_seconds"
                  type="number" :min="5" :max="120"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                窗口创建后最迟必发的硬上界；不参与随机，保证"攒的话必说出去"。
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">窗口消息数上限（条）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.window_max_msgs"
                  :min="5" :max="50" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.window_max_msgs"
                  type="number" :min="5" :max="50"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                超限丢弃最旧消息保留最新，防止刷屏时上下文爆炸。
              </div>

              <div class="d-flex align-center" style="gap: 12px">
                <v-btn type="submit" color="primary" variant="tonal" :loading="saving" :disabled="!loaded">
                  <v-icon class="me-1">mdi-content-save</v-icon> 保存
                </v-btn>
              </div>
            </v-form>
          </v-card-text>
        </v-card>
      </v-col>

      <!-- 随机性与其他设置 -->
      <v-col cols="12" md="5">
        <v-card rounded="lg" elevation="1" class="h-100 mb-4">
          <v-card-item>
            <template #title><span class="text-h6 font-weight-bold">随机性</span></template>
            <template #subtitle>让释放时机不那么"人机"；全部为 0/1 即确定性模式</template>
          </v-card-item>
          <v-card-text>
            <v-form @submit.prevent="handleSave">
              <div class="text-subtitle-2 font-weight-bold mb-2">释放时机抖动（秒）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.jitter_seconds"
                  :min="0" :max="10" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.jitter_seconds"
                  type="number" :min="0" :max="10"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                安静间隔取「间隔 ± 0~抖动」随机值；0=关闭随机。只影响时机，不会推迟最迟必发。
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">计数抖动（条）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.force_count_jitter"
                  :min="0" :max="5" :step="1"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.force_count_jitter"
                  type="number" :min="0" :max="5"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">插话计数 ± 0~抖动，0=关闭。</div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">参与概率</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.participate_probability"
                  :min="0" :max="1" :step="0.05"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.participate_probability"
                  type="number" :min="0" :max="1" :step="0.05"
                  density="compact" style="width: 90px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                安静释放时以该概率真的参与，1-p 静默放弃本窗；插话计数强发与最迟必发不受影响。当前: {{ (Number(form.participate_probability) || 0).toFixed(2) }}
              </div>

              <v-divider class="my-3" />

              <div class="text-subtitle-2 font-weight-bold mb-2">打字延迟上限（毫秒）</div>
              <div class="d-flex align-center" style="gap: 16px">
                <v-slider
                  v-model.number="form.typing_delay_max_ms"
                  :min="0" :max="5000" :step="100"
                  color="primary" thumb-label="always" hide-details style="flex: 1"
                />
                <v-text-field
                  v-model.number="form.typing_delay_max_ms"
                  type="number" :min="0" :max="5000" :step="100"
                  density="compact" style="width: 110px" hide-details variant="outlined"
                />
              </div>
              <div class="text-caption text-medium-emphasis mt-1">
                参与路径发送前随机等待 0~上限，模拟真人输入；0=关闭（必回路径不受影响）。
              </div>

              <v-btn type="submit" color="primary" variant="tonal" :loading="saving" :disabled="!loaded">
                <v-icon class="me-1">mdi-content-save</v-icon> 保存
              </v-btn>
            </v-form>
          </v-card-text>
        </v-card>

        <v-card rounded="lg" elevation="1" class="h-100">
          <v-card-item>
            <template #title><span class="text-h6 font-weight-bold">其他设置</span></template>
            <template #subtitle>消息格式与 Agent 行为</template>
          </v-card-item>
          <v-card-text>
            <v-form @submit.prevent="handleSave">
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

              <div class="d-flex align-start mb-4">
                <div class="flex-grow-1 me-3">
                  <div class="text-subtitle-2 font-weight-bold">去除 Markdown 格式</div>
                  <div class="text-caption text-medium-emphasis mt-1">
                    Agent 发送消息前去除加粗、斜体、代码块、链接等格式，发送纯文本到 QQ。
                  </div>
                </div>
                <v-switch v-model="form.strip_markdown" color="primary" hide-details density="compact" />
              </div>

              <v-btn type="submit" color="primary" variant="tonal" :loading="saving" :disabled="!loaded">
                <v-icon class="me-1">mdi-content-save</v-icon> 保存
              </v-btn>
            </v-form>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useToastStore } from '@/stores/toast'
import { replyStrategyApi } from '@/api'

const toastStore = useToastStore()
const saving = ref(false)
// loaded：回复设置是否已成功加载。加载失败时禁用保存，避免把默认表单值当成真实配置写回。
const loaded = ref(false)

const form = ref({
  bot_name: '',
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
})

async function load() {
  try {
    const res = await replyStrategyApi.get()
    const d = (res.data as any)?.data
    if (d) {
      form.value.bot_name = d.bot_name || ''
      form.value.strip_markdown = d.strip_markdown || false
      form.value.agent_lite = d.agent_lite || false
      form.value.quiet_gap_seconds = d.quiet_gap_seconds ?? 5
      form.value.force_count = d.force_count ?? 5
      form.value.max_age_seconds = d.max_age_seconds ?? 20
      form.value.window_max_msgs = d.window_max_msgs ?? 20
      form.value.jitter_seconds = d.jitter_seconds ?? 2
      form.value.force_count_jitter = d.force_count_jitter ?? 1
      form.value.participate_probability = d.participate_probability ?? 0.8
      form.value.typing_delay_max_ms = d.typing_delay_max_ms ?? 1500
    }
    loaded.value = true
  } catch (e: any) {
    loaded.value = false
    toastStore.error(e?.response?.data?.info || e?.message || '加载回复设置失败')
  }
}

async function handleSave() {
  saving.value = true
  try {
    await replyStrategyApi.update({
      bot_name: form.value.bot_name,
      strip_markdown: form.value.strip_markdown,
      agent_lite: form.value.agent_lite,
      quiet_gap_seconds: form.value.quiet_gap_seconds || 5,
      force_count: form.value.force_count || 5,
      max_age_seconds: form.value.max_age_seconds || 20,
      window_max_msgs: form.value.window_max_msgs || 20,
      jitter_seconds: Number(form.value.jitter_seconds ?? 0),
      force_count_jitter: Number(form.value.force_count_jitter ?? 0),
      participate_probability: Number(form.value.participate_probability ?? 0),
      typing_delay_max_ms: Number(form.value.typing_delay_max_ms ?? 0),
    })
    toastStore.success('回复设置已保存')
  } catch (e: any) {
    toastStore.error(e?.response?.data?.info || e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
