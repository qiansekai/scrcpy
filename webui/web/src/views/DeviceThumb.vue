<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useStream } from '../composables/useStream'

const props = defineProps<{ id: string; ip: string; name?: string; online?: boolean; selected?: boolean }>()
const emit = defineEmits<{ (e: 'select'): void; (e: 'delete'): void }>()
const router = useRouter()
const canvas = ref<HTMLCanvasElement | null>(null)
let stream: ReturnType<typeof useStream> | null = null

onMounted(() => {
  if (!canvas.value) return
  stream = useStream(props.id, canvas.value, { throttle: 5, fixedCanvas: true })
  stream.connect().catch(() => {})
})

onBeforeUnmount(() => {
  stream?.disconnect()
})

function select() {
  emit('select')
}

function open() {
  router.push(`/devices/${props.id}`)
}
</script>

<template>
  <div class="thumb-item" :class="{ selected }" @click="select">
    <div class="thumb-body">
      <canvas ref="canvas" width="135" height="240" />
      <span v-if="!online" class="offline-dot" />
      <div class="thumb-actions">
        <v-btn size="x-small" icon="mdi-open-in-new" variant="tonal" @click.stop="open" />
        <v-btn size="x-small" icon="mdi-delete" variant="tonal" @click.stop="$emit('delete')" />
      </div>
    </div>
    <div class="thumb-name text-caption text-truncate">{{ name || ip }}</div>
  </div>
</template>

<style scoped>
.thumb-item {
  width: 130px;
  border-radius: 8px;
  padding: 4px;
  cursor: pointer;
  border: 2px solid transparent;
  transition: border-color 0.15s;
}
.thumb-item.selected {
  border-color: #1976d2;
}
.thumb-item:hover .thumb-actions {
  opacity: 1;
}
.thumb-body {
  position: relative;
  aspect-ratio: 9 / 16;
  background: #000;
  border-radius: 6px;
  overflow: hidden;
}
.thumb-body canvas {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.offline-dot {
  position: absolute;
  top: 4px;
  left: 4px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #f44336;
}
.thumb-name {
  margin-top: 2px;
  text-align: center;
}
.thumb-actions {
  position: absolute;
  top: 4px;
  right: 4px;
  display: flex;
  gap: 4px;
  opacity: 0;
}
</style>
