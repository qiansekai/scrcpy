<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useControl } from '../composables/useControl'
import { useKeyboard } from '../composables/useKeyboard'
import { setAudioBufMs, useStream, type StreamAlert, type StreamStatus } from '../composables/useStream'
import ExecConsole from './ExecConsole.vue'
import type { DeviceStatus } from '../api'

const props = defineProps<{
  device: { id: string; ip: string; name?: string }
  isMaster?: boolean
  isSlave?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'set-master'): void
  (e: 'status', s: { battery: number; plugged: boolean; model: string; android: string }): void
  (e: 'alert', a: StreamAlert): void
}>()

const canvas = ref<HTMLCanvasElement | null>(null)
const stream = ref<ReturnType<typeof useStream> | null>(null)
const control = ref<ReturnType<typeof useControl> | null>(null)
const keyboard = ref<ReturnType<typeof useKeyboard> | null>(null)
const bufMs = ref<number>(Number(localStorage.getItem('audioBufMs') || 60))
const deviceDup = ref(false)

// 文本输入（A3）
const textInput = ref('')
// 设备状态（status 广播实时更新）
const status = ref<DeviceStatus>({ battery: -1, plugged: false, model: '', android: '' })

onMounted(() => {
  if (!canvas.value) return
  const s = useStream(props.device.id, canvas.value, {
    onClipboard: (text) => navigator.clipboard.writeText(text).catch(() => {}),
    onStatus: (st: StreamStatus) => {
      if (st.id !== props.device.id) return
      status.value = { battery: st.battery, plugged: st.plugged, model: st.model, android: st.android }
      emit('status', { battery: st.battery, plugged: st.plugged, model: st.model, android: st.android })
    },
    onAlert: (a: StreamAlert) => {
      emit('alert', a)
    },
  }) // 全分辨率，无节流
  stream.value = s
  control.value = useControl((m) => s.send(m), canvas, s.dims)
  keyboard.value = useKeyboard((m) => s.send(m))
  control.value.bind()
  keyboard.value.bind()
  s.connect().catch(() => {})
})

onBeforeUnmount(() => {
  control.value?.unbind()
  keyboard.value?.unbind()
  stream.value?.disconnect()
})

function shortcut(type: string) {
  stream.value?.send({ type })
}

// 发送文本：经 WS {type:"text"}，需设备启用 ADBKeyboard 输入法才支持中文
function sendText() {
  const t = textInput.value
  if (!t) return
  stream.value?.send({ type: 'text', text: t })
  textInput.value = '' // 发送后清空
}

function setMaster() {
  emit('set-master')
}

function onBuf(v: number | null) {
  if (v == null) return
  bufMs.value = v
  setAudioBufMs(v) // 实时生效并持久化
}

function onDup(v: boolean | null) {
  if (v == null) return
  stream.value?.send({ type: 'audioDup', on: v })
}
</script>

<template>
  <v-card variant="outlined" class="preview">
    <div class="preview-row">
      <div class="preview-body">
        <canvas ref="canvas" class="preview-canvas" />
      </div>
      <div class="preview-side pa-2">
        <div class="d-flex justify-space-between align-center ga-2 mb-1">
          <div class="text-truncate">
            <div class="font-weight-medium text-truncate">{{ device.name || device.ip }}</div>
            <div class="text-caption text-medium-emphasis text-truncate">{{ device.ip }}</div>
          </div>
          <div class="d-flex align-center ga-1">
            <v-chip v-if="isMaster" size="x-small" color="orange-darken-3">主控</v-chip>
            <v-chip v-else-if="isSlave" size="x-small" color="purple-darken-3">被控</v-chip>
            <v-btn size="x-small" variant="tonal" color="warning" @click="setMaster">
              {{ isMaster ? '取消主控' : '设为主控' }}
            </v-btn>
            <v-btn size="x-small" variant="tonal" @click="$emit('close')">取消选中</v-btn>
          </div>
        </div>

        <!-- 设备状态小字：battery/plugged/model -->
        <div class="text-caption text-medium-emphasis mb-1">
          <template v-if="status.battery >= 0">🔋 {{ status.battery }}%<span v-if="status.plugged"> ⚡</span></template>
          <template v-if="status.model"> · {{ status.model }}</template>
          <template v-if="status.android"> · Android {{ status.android }}</template>
        </div>

        <div class="d-flex ga-1 flex-wrap">
          <v-btn size="x-small" @click="shortcut('home')">HOME</v-btn>
          <v-btn size="x-small" @click="shortcut('back')">BACK</v-btn>
          <v-btn size="x-small" @click="shortcut('recents')">RECENTS</v-btn>
          <v-btn size="x-small" @click="shortcut('power')">电源</v-btn>
          <v-btn size="x-small" @click="shortcut('rotate')">旋转</v-btn>
        </div>

        <!-- A3 文本输入 -->
        <div class="d-flex ga-2 mt-2">
          <v-text-field
            v-model="textInput"
            label="发送文本"
            placeholder="发送文本（需设备启用 ADBKeyboard 输入法）"
            density="compact"
            variant="outlined"
            hide-details
            class="flex-grow-1"
            @keydown.enter.prevent="sendText"
          />
          <v-btn size="small" color="primary" :disabled="!textInput" @click="sendText">发送</v-btn>
        </div>

        <!-- A4 exec 控制台 -->
        <div class="mt-2">
          <ExecConsole :device-id="device.id" />
        </div>

        <div class="d-flex align-center ga-2 mt-1">
          <v-switch v-model="deviceDup" label="手机出声" density="compact" hide-details @update:model-value="onDup" />
        </div>
        <div class="d-flex align-center ga-2 mt-1">
          <v-slider
            v-model="bufMs"
            :min="10"
            :max="300"
            :step="10"
            density="compact"
            hide-details
            class="flex-grow-1"
            @update:model-value="onBuf"
          />
          <span class="text-caption text-medium-emphasis text-no-wrap">缓冲 {{ bufMs }}ms</span>
        </div>
      </div>
    </div>
  </v-card>
</template>

<style scoped>
.preview {
  position: sticky;
  top: 0;
  /* 卡片贴合内容宽度，不撑满父容器 */
  width: fit-content;
  max-width: 100%;
}
/* 左右分栏：视频在左，控件在右，整体限制在一屏内避免页面纵向滚动 */
.preview-row {
  display: flex;
  align-items: stretch;
  gap: 0;
}
.preview-body {
  flex: 0 1 auto;
  min-width: 0;
  background: #000;
  border-radius: 4px 0 0 4px;
  overflow: hidden;
  display: flex;
  align-items: center;
}
.preview-canvas {
  display: block;
  /* 视口内留出顶部工具栏（约 64px）+ 边距的空间，其余都给视频 */
  max-height: calc(100vh - 120px);
  max-width: 100%;
  width: auto;
  height: auto;
  touch-action: none;
}
.preview-side {
  width: 400px;
  max-width: 44vw;
  min-width: 300px;
  display: flex;
  flex-direction: column;
  /* 控件列极端情况下自身滚动，页面本身不滚动 */
  max-height: calc(100vh - 120px);
  overflow-y: auto;
}
</style>
