<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useStream } from '../composables/useStream'

const props = defineProps<{ id: string; ip: string; name?: string; online?: boolean }>()
defineEmits<{ (e: 'delete'): void }>()
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

function open() {
  router.push(`/devices/${props.id}`)
}
</script>

<template>
  <v-card hover class="thumb-card w-100" @click="open">
    <div class="thumb-body">
      <canvas ref="canvas" width="270" height="600" />
      <v-chip v-if="!online" size="x-small" color="error" class="status-chip">离线</v-chip>
      <v-btn
        icon="mdi-delete"
        size="x-small"
        variant="tonal"
        class="delete-btn"
        @click.stop="$emit('delete')"
      />
    </div>
    <v-card-text class="pa-2 text-caption">
      <div class="text-truncate font-weight-medium">{{ name || ip }}</div>
      <div class="text-medium-emphasis text-truncate">{{ ip }}</div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.thumb-card {
  cursor: pointer;
}
.thumb-body {
  position: relative;
  aspect-ratio: 9 / 20;
  background: #000;
  overflow: hidden;
}
.thumb-body canvas {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.status-chip {
  position: absolute;
  top: 4px;
  left: 4px;
}
.delete-btn {
  position: absolute;
  top: 2px;
  right: 2px;
}
</style>
