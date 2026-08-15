<script setup lang="ts">
import { onBeforeUnmount, onMounted, onUnmounted, ref } from 'vue'
import { useStream } from '../composables/useStream'

const { id } = defineProps<{ id: string }>()
const canvas = ref<HTMLCanvasElement | null>(null)
const stream = ref<ReturnType<typeof useStream> | null>(null)
const err = ref('')
let pointerDown = false

function sendTouch(action: number, e: PointerEvent) {
  const s = stream.value
  const el = canvas.value
  if (!s || !el) return
  const dims = s.dims
  if (!dims.width || !dims.height) return
  const rect = el.getBoundingClientRect()
  const x = ((e.clientX - rect.left) / rect.width) * dims.width
  const y = ((e.clientY - rect.top) / rect.height) * dims.height
  s.send({
    type: 'touch',
    action,
    x,
    y,
    screenW: dims.width,
    screenH: dims.height,
  })
}

function onPointerDown(e: PointerEvent) {
  pointerDown = true
  canvas.value?.setPointerCapture(e.pointerId)
  sendTouch(0, e)
}

function onPointerMove(e: PointerEvent) {
  if (pointerDown) sendTouch(2, e)
}

function onPointerUp(e: PointerEvent) {
  pointerDown = false
  sendTouch(1, e)
}

function onPointerCancel(e: PointerEvent) {
  pointerDown = false
  sendTouch(1, e)
}

const KEYMAP: Record<string, number> = {
  Enter: 66, Backspace: 67, Tab: 61, Space: 62,
  ArrowUp: 19, ArrowDown: 20, ArrowLeft: 21, ArrowRight: 22,
  Home: 122, End: 123, PageUp: 92, PageDown: 93,
  Delete: 67, Escape: 111,
  ShiftLeft: 59, ShiftRight: 59, ControlLeft: 113, ControlRight: 113, AltLeft: 57, AltRight: 57,
}

const LETTERS: Record<string, number> = {
  a: 29, b: 30, c: 31, d: 32, e: 33, f: 34, g: 35, h: 36, i: 37,
  j: 38, k: 39, l: 40, m: 41, n: 42, o: 43, p: 44, q: 45, r: 46,
  s: 47, t: 48, u: 49, v: 50, w: 51, x: 52, y: 53, z: 54,
}

const DIGITS: Record<string, number> = {
  '0': 7, '1': 8, '2': 9, '3': 10, '4': 11, '5': 12, '6': 13, '7': 14, '8': 15, '9': 16,
}

function keyToKeycode(e: KeyboardEvent): number | null {
  if (e.code.startsWith('Key')) return LETTERS[e.code.slice(3).toLowerCase()] ?? null
  if (e.code.startsWith('Digit')) return DIGITS[e.code.slice(5)] ?? null
  return KEYMAP[e.code] ?? null
}

function sendKey(action: number, e: KeyboardEvent) {
  const code = keyToKeycode(e)
  if (code == null) return
  e.preventDefault()
  stream.value?.send({ type: 'key', action, keycode: code })
}

function onKeyDown(e: KeyboardEvent) {
  sendKey(0, e)
}

function onKeyUp(e: KeyboardEvent) {
  sendKey(1, e)
}

onMounted(async () => {
  if (!canvas.value) return
  const s = useStream(id, canvas.value)
  stream.value = s
  const el = canvas.value
  el.addEventListener('pointerdown', onPointerDown)
  el.addEventListener('pointermove', onPointerMove)
  el.addEventListener('pointerup', onPointerUp)
  el.addEventListener('pointercancel', onPointerCancel)
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  try {
    await s.connect()
  } catch (e: any) {
    err.value = e.message
  }
})

onBeforeUnmount(() => {
  if (canvas.value) {
    canvas.value.removeEventListener('pointerdown', onPointerDown)
    canvas.value.removeEventListener('pointermove', onPointerMove)
    canvas.value.removeEventListener('pointerup', onPointerUp)
    canvas.value.removeEventListener('pointercancel', onPointerCancel)
  }
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
})

onUnmounted(() => {
  stream.value?.disconnect()
})
</script>

<template>
  <div class="pa-4">
    <v-alert v-if="err" type="error">{{ err }}</v-alert>
    <v-card v-if="!err" variant="outlined" density="compact" class="mb-2">
      <v-card-text class="text-caption">
        {{ stream?.connected ? '已连接' : '连接中...' }}
      </v-card-text>
    </v-card>
    <div class="d-flex ga-2 my-2">
      <v-btn size="small" @click="stream?.send({ type: 'home' })">HOME</v-btn>
      <v-btn size="small" @click="stream?.send({ type: 'back' })">BACK</v-btn>
      <v-btn size="small" @click="stream?.send({ type: 'recents' })">RECENTS</v-btn>
      <v-btn size="small" @click="stream?.send({ type: 'power' })">电源</v-btn>
      <v-btn size="small" @click="stream?.send({ type: 'rotate' })">旋转</v-btn>
    </div>
    <canvas ref="canvas" style="width: 100%; max-width: 480px; background: #000; touch-action: none" />
  </div>
</template>
