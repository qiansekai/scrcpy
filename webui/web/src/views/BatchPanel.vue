<script setup lang="ts">
// 批量操作面板：对已勾选设备执行命令 / 装 APK / 推文件，结果统一表格展示。
import { computed, ref } from 'vue'
import { batchExec, batchInstall, batchPush, type BatchItemResult, type Device } from '../api'

const props = defineProps<{ devices: Device[] }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const mode = ref<'exec' | 'install' | 'push'>('exec')
const cmd = ref('')
const remote = ref('')
const file = ref<File | null>(null)
const fileError = ref('')
const running = ref(false)
const results = ref<(BatchItemResult & { label: string })[] | null>(null)

const title = computed(() => {
  switch (mode.value) {
    case 'exec':
      return '批量执行命令'
    case 'install':
      return '批量安装 APK'
    case 'push':
      return '批量推送文件'
  }
})

// 显示名：优先 name，否则 ip
function labelOf(id: string) {
  const d = props.devices.find((x) => x.id === id)
  return d?.name || id
}

function resetBatch() {
  results.value = null
  fileError.value = ''
}

function switchMode(m: 'exec' | 'install' | 'push') {
  mode.value = m
  resetBatch()
}

function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  file.value = input.files?.[0] ?? null
  fileError.value = ''
  resetBatch()
}

async function run() {
  if (running.value) return
  fileError.value = ''
  results.value = null

  if (mode.value === 'install') {
    if (!file.value) {
      fileError.value = '请先选择 APK 文件'
      return
    }
    if (!file.value.name.toLowerCase().endsWith('.apk')) {
      fileError.value = '请选择 .apk 文件'
      return
    }
  }
  if (mode.value === 'push') {
    if (!file.value) {
      fileError.value = '请先选择要推送的文件'
      return
    }
    if (!remote.value.trim()) {
      fileError.value = '请填写目标路径（如 /sdcard/Download/x.apk）'
      return
    }
  }
  if (mode.value === 'exec' && !cmd.value.trim()) {
    fileError.value = '请输入要执行的命令'
    return
  }

  running.value = true
  try {
    let r: BatchItemResult[]
    if (mode.value === 'exec') {
      const res = await batchExec(props.devices.map((d) => d.id), cmd.value.trim())
      r = res.results
    } else if (mode.value === 'install') {
      const res = await batchInstall(props.devices.map((d) => d.id), file.value!)
      r = res.results
    } else {
      const res = await batchPush(props.devices.map((d) => d.id), file.value!, remote.value.trim())
      r = res.results
    }
    results.value = r.map((x) => ({ ...x, label: labelOf(x.id) }))
  } catch (e: any) {
    fileError.value = e?.message ?? '批量操作失败'
  } finally {
    running.value = false
  }
}
</script>

<template>
  <v-card>
    <v-card-title>{{ title }}</v-card-title>
    <v-card-text>
      <div class="text-caption text-medium-emphasis mb-2">
        目标设备 {{ devices.length }} 台：{{ devices.map((d) => d.name || d.id).join('、') }}
      </div>

      <v-tabs v-model="mode" density="compact" class="mb-2" @update:model-value="switchMode">
        <v-tab value="exec">执行命令</v-tab>
        <v-tab value="install">装 APK</v-tab>
        <v-tab value="push">推文件</v-tab>
      </v-tabs>

      <v-text-field
        v-if="mode === 'exec'"
        v-model="cmd"
        label="命令"
        placeholder="如 pm list packages"
        density="compact"
        variant="outlined"
        hide-details
        class="mb-2"
        @keydown.enter.prevent="run"
      />

      <template v-if="mode === 'install'">
        <input type="file" accept=".apk" class="file-input" @change="onFile" />
        <div class="text-caption text-medium-emphasis">{{ file ? file.name : '未选择文件' }}</div>
      </template>

      <template v-if="mode === 'push'">
        <input type="file" class="file-input" @change="onFile" />
        <div class="text-caption text-medium-emphasis mb-2">{{ file ? file.name : '未选择文件' }}</div>
        <v-text-field
          v-model="remote"
          label="目标路径"
          placeholder="/sdcard/Download/x.apk"
          density="compact"
          variant="outlined"
          hide-details
        />
      </template>

      <v-alert v-if="fileError" type="error" density="compact" class="mt-2" closable @update:model-value="fileError = ''">
        {{ fileError }}
      </v-alert>

      <!-- 结果表格 -->
      <div v-if="results" class="mt-3">
        <v-table density="compact">
          <thead>
            <tr>
              <th>设备</th>
              <th>状态</th>
              <th>exitCode</th>
              <th>输出</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in results" :key="r.id">
              <td class="text-truncate">{{ r.label }}</td>
              <td>
                <span v-if="r.ok" class="text-success">成功</span>
                <span v-else class="text-error">{{ r.error || '失败' }}</span>
              </td>
              <td>{{ r.exitCode ?? '-' }}</td>
              <td>
                <v-expansion-panels v-if="(r.stdout || r.stderr || r.remote)" variant="accordion">
                  <v-expansion-panel>
                    <v-expansion-panel-title density="compact">详情</v-expansion-panel-title>
                    <v-expansion-panel-text>
                      <pre class="result-pre">stdout:\n{{ r.stdout || '(空)' }}\nstderr:\n{{ r.stderr || '(空)' }}<template v-if="r.remote">\nremote: {{ r.remote }}</template></pre>
                    </v-expansion-panel-text>
                  </v-expansion-panel>
                </v-expansion-panels>
                <span v-else class="text-caption text-medium-emphasis">—</span>
              </td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn @click="emit('close')">关闭</v-btn>
      <v-btn color="primary" :loading="running" @click="run">{{ results ? '重新运行' : '运行' }}</v-btn>
    </v-card-actions>
  </v-card>
</template>

<style scoped>
.file-input {
  display: block;
  margin-bottom: 4px;
}
.result-pre {
  font-family: 'Consolas', 'Menlo', 'Courier New', monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
