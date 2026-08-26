<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useStream, type StreamAlert, type StreamStatus } from '../composables/useStream'
import type { DeviceStatus } from '../api'

const props = defineProps<{
  id: string
  ip: string
  name?: string
  online?: boolean
  selected?: boolean
  checked?: boolean
  isMaster?: boolean
  isSlave?: boolean
  status?: DeviceStatus
}>()

const emit = defineEmits<{
  (e: 'select'): void
  (e: 'delete'): void
  (e: 'toggle-check'): void
  (e: 'alert', a: StreamAlert): void
}>()

const canvas = ref<HTMLCanvasElement | null>(null)
let stream: ReturnType<typeof useStream> | null = null

// 优先用 props.status（列表刷新 / status 广播），缩略图 WS 也会实时推送 status 兜底。
// 电量未知（-1 / undefined）不显示百分比。
const batteryText = ref<string>('')

function applyStatus(s?: DeviceStatus) {
  if (s && typeof s.battery === 'number' && s.battery >= 0) {
    batteryText.value = `${s.battery}%`
  } else if (props.status && typeof props.status.battery === 'number' && props.status.battery >= 0) {
    batteryText.value = `${props.status.battery}%`
  } else {
    batteryText.value = ''
  }
}

function onStatus(s: StreamStatus) {
  if (s.id === props.id) {
    // 仅当后端推来有效电量时才更新本地展示
    if (typeof s.battery === 'number' && s.battery >= 0) batteryText.value = `${s.battery}%`
  }
}

// 告警回调：缩略图流（noAudio 节流流）与音视频无关，收到 {type:"alert"}
// 直接向上透传给 DeviceGrid 汇合进告警 snackbar，保证未选中设备也能弹提示。
function onAlert(a: StreamAlert) {
  if (a.id === props.id) emit('alert', a)
}

onMounted(() => {
  applyStatus(props.status)
  if (!canvas.value) return
  stream = useStream(props.id, canvas.value, {
    throttle: 5,
    fixedCanvas: true,
    noAudio: true,
    onStatus,
    onAlert,
  })
  stream.connect().catch(() => {})
})

onBeforeUnmount(() => {
  stream?.disconnect()
})

function select() {
  emit('select')
}
</script>

<template>
  <div class="thumb-item" :class="{ selected, 'is-master': isMaster, 'is-slave': isSlave }" @click="select">
    <!-- 多选 checkbox：点击不冒泡触发选中预览 -->
    <v-checkbox
      :model-value="checked"
      density="compact"
      hide-details
      class="thumb-check"
      @click.stop
      @update:model-value="emit('toggle-check')"
    />
    <!-- 主控/被控徽标 -->
    <span v-if="isMaster" class="role-badge master">主控</span>
    <span v-else-if="isSlave" class="role-badge slave">被控</span>
    <div class="thumb-body">
      <canvas ref="canvas" width="135" height="240" />
      <div class="thumb-actions">
        <v-btn size="x-small" icon="mdi-delete" variant="tonal" @click.stop="$emit('delete')" />
      </div>
    </div>
    <!-- 状态行：在线点 + 名称 + 电量（移出视频叠层，避免与勾选框/徽标打架） -->
    <div class="thumb-name-row">
      <span class="status-dot" :class="online ? 'online' : 'offline'" />
      <span class="thumb-name text-caption text-truncate">{{ name || ip }}</span>
      <span v-if="batteryText" class="battery-text text-caption">
        🔋{{ batteryText }}<span v-if="status?.plugged" class="plug">⚡</span>
      </span>
    </div>
    <div v-if="status?.model" class="thumb-model text-caption text-medium-emphasis text-truncate">{{ status.model }}</div>
  </div>
</template>

<style scoped>
.thumb-item {
  position: relative;
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
.thumb-item.is-master {
  border-color: #e65100;
}
.thumb-item.is-slave {
  border-color: #7b1fa2;
}
.thumb-item:hover .thumb-actions {
  opacity: 1;
}
.thumb-check {
  position: absolute;
  top: 2px;
  left: 2px;
  z-index: 2;
  margin: 0;
}
.role-badge {
  position: absolute;
  top: 2px;
  right: 2px;
  z-index: 2;
  font-size: 10px;
  line-height: 1;
  padding: 2px 4px;
  border-radius: 4px;
  color: #fff;
}
.role-badge.master {
  background: #e65100;
}
.role-badge.slave {
  background: #7b1fa2;
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
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.status-dot.online {
  background: #4caf50;
}
.status-dot.offline {
  background: #9e9e9e;
}
.thumb-name-row {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 2px;
  min-width: 0;
}
.thumb-name {
  text-align: left;
  min-width: 0;
}
.battery-text {
  margin-left: auto;
  white-space: nowrap;
  color: rgba(0, 0, 0, 0.6);
}
.battery-text .plug {
  color: #f9a825;
}
.thumb-model {
  text-align: center;
  font-size: 10px !important;
}
.thumb-actions {
  position: absolute;
  bottom: 4px;
  right: 4px;
  display: flex;
  gap: 4px;
  opacity: 0;
}
</style>
