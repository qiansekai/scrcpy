<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useControl } from '../composables/useControl'
import { useKeyboard } from '../composables/useKeyboard'
import { setAudioBufMs, useStream } from '../composables/useStream'

const props = defineProps<{ device: { id: string; ip: string; name?: string } }>()
const canvas = ref<HTMLCanvasElement | null>(null)
const stream = ref<ReturnType<typeof useStream> | null>(null)
const control = ref<ReturnType<typeof useControl> | null>(null)
const keyboard = ref<ReturnType<typeof useKeyboard> | null>(null)
const bufMs = ref<number>(Number(localStorage.getItem('audioBufMs') || 60))
const deviceDup = ref(false)

onMounted(() => {
  if (!canvas.value) return
  const s = useStream(props.device.id, canvas.value, {
    onClipboard: (text) => navigator.clipboard.writeText(text).catch(() => {}),
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
    <div class="preview-body">
      <canvas ref="canvas" class="preview-canvas" />
    </div>
    <v-card-text class="pa-2">
      <div class="d-flex justify-space-between align-center ga-2 mb-1">
        <div class="text-truncate">
          <div class="font-weight-medium text-truncate">{{ device.name || device.ip }}</div>
          <div class="text-caption text-medium-emphasis text-truncate">{{ device.ip }}</div>
        </div>
        <v-btn size="x-small" variant="tonal" @click="$emit('close')">取消选中</v-btn>
      </div>
      <div class="d-flex ga-1 flex-wrap">
        <v-btn size="x-small" @click="shortcut('home')">HOME</v-btn>
        <v-btn size="x-small" @click="shortcut('back')">BACK</v-btn>
        <v-btn size="x-small" @click="shortcut('recents')">RECENTS</v-btn>
        <v-btn size="x-small" @click="shortcut('power')">电源</v-btn>
        <v-btn size="x-small" @click="shortcut('rotate')">旋转</v-btn>
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
    </v-card-text>
  </v-card>
</template>

<style scoped>
.preview {
  position: sticky;
  top: 0;
  /* 卡片贴合视频自身宽度，不撑满父容器，避免两侧大空白 */
  width: fit-content;
  max-width: 100%;
}
.preview-body {
  /* 收缩贴合视频自身比例，避免 contain 在容器内露黑边 */
  width: fit-content;
  max-width: 100%;
  margin: 0 auto;
  background: #000;
}
.preview-canvas {
  display: block;
  max-width: 100%;
  max-height: 78vh;
  width: auto;
  height: auto;
  touch-action: none;
}
</style>
