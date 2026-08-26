<script setup lang="ts">
// exec 控制台：对单台设备执行 shell 命令，追加历史输出（本次会话内保留）。
import { ref } from 'vue'
import { execCommand } from '../api'

const props = defineProps<{ deviceId: string }>()

const cmd = ref('')
const running = ref(false)
const error = ref('')

interface Line {
  kind: 'cmd' | 'out' | 'err' | 'exit'
  text: string
}

const lines = ref<Line[]>([])

async function run() {
  const c = cmd.value.trim()
  if (!c || running.value) return
  error.value = ''
  running.value = true
  lines.value.push({ kind: 'cmd', text: `$ ${c}` })
  cmd.value = ''
  try {
    const r = await execCommand(props.deviceId, c)
    if (r.stdout) lines.value.push({ kind: 'out', text: r.stdout })
    if (r.stderr) lines.value.push({ kind: 'err', text: r.stderr })
    lines.value.push({ kind: 'exit', text: `exitCode=${r.exitCode}` })
  } catch (e: any) {
    lines.value.push({ kind: 'err', text: e?.message ?? '执行失败' })
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div class="exec-console">
    <div class="d-flex ga-2">
      <v-text-field
        v-model="cmd"
        label="执行命令"
        placeholder="如 pm list packages 或 adb 风格命令"
        density="compact"
        hide-details
        variant="outlined"
        class="flex-grow-1"
        @keydown.enter.prevent="run"
      />
      <v-btn color="primary" size="small" :loading="running" @click="run">执行</v-btn>
    </div>
    <v-alert v-if="error" type="error" density="compact" class="mt-1" closable @update:model-value="error = ''">
      {{ error }}
    </v-alert>
    <pre class="console-out"><template v-for="(line, i) in lines" :key="i"><span :class="line.kind">{{ line.text }}</span>\n</template></pre>
  </div>
</template>

<style scoped>
.exec-console {
  display: flex;
  flex-direction: column;
}
.console-out {
  margin: 4px 0 0;
  padding: 6px 8px;
  max-height: 180px;
  overflow: auto;
  background: #1e1e1e;
  color: #d4d4d4;
  border-radius: 4px;
  font-family: 'Consolas', 'Menlo', 'Courier New', monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.console-out .cmd {
  color: #4fc1ff;
}
.console-out .err {
  color: #f14c4c;
}
.console-out .exit {
  color: #6a9955;
}
</style>
