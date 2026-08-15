<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useStream } from '../composables/useStream'

const { id } = defineProps<{ id: string }>()
const canvas = ref<HTMLCanvasElement | null>(null)
const stream = ref<ReturnType<typeof useStream> | null>(null)
const err = ref('')

onMounted(async () => {
  if (!canvas.value) return
  const s = useStream(id, canvas.value)
  stream.value = s
  try {
    await s.connect()
  } catch (e: any) {
    err.value = e.message
  }
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
    <canvas ref="canvas" style="width: 100%; max-width: 480px; background: #000" />
  </div>
</template>
