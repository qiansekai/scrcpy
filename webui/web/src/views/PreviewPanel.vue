<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useStream } from '../composables/useStream'

const props = defineProps<{ device: { id: string; ip: string; name?: string } }>()
const router = useRouter()
const canvas = ref<HTMLCanvasElement | null>(null)
let stream: ReturnType<typeof useStream> | null = null

onMounted(() => {
  if (!canvas.value) return
  stream = useStream(props.device.id, canvas.value) // 全分辨率，无节流
  stream.connect().catch(() => {})
})

onBeforeUnmount(() => {
  stream?.disconnect()
})
</script>

<template>
  <v-card variant="outlined" class="preview">
    <div class="preview-body">
      <canvas ref="canvas" />
    </div>
    <v-card-text class="pa-2 d-flex justify-space-between align-center ga-2">
      <div class="text-truncate">
        <div class="font-weight-medium text-truncate">{{ device.name || device.ip }}</div>
        <div class="text-caption text-medium-emphasis text-truncate">{{ device.ip }}</div>
      </div>
      <v-btn size="small" color="primary" @click="router.push(`/devices/${device.id}`)">
        打开控制台
      </v-btn>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.preview {
  position: sticky;
  top: 0;
}
.preview-body {
  width: 100%;
  background: #000;
  min-height: 260px;
  max-height: 55vh;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}
.preview-body canvas {
  max-width: 100%;
  max-height: 55vh;
  object-fit: contain;
}
</style>
