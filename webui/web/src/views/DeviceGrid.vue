<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { addDevice, listDevices, removeDevice, type Device } from '../api'
import DeviceThumb from './DeviceThumb.vue'
import PreviewPanel from './PreviewPanel.vue'

const devices = ref<Device[]>([])
const selectedId = ref<string | null>(null)
const dialog = ref(false)
const ip = ref('')
const error = ref('')
let timer: number | null = null

const onlineCount = computed(() => devices.value.filter((d) => d.online).length)
const selected = computed(() => devices.value.find((d) => d.id === selectedId.value) ?? null)

async function refresh() {
  try {
    devices.value = await listDevices()
  } catch {
    // backend down: keep last list, next poll retries
  }
  if (selectedId.value && !devices.value.some((d) => d.id === selectedId.value)) {
    selectedId.value = null
  }
}

onMounted(() => {
  refresh()
  timer = window.setInterval(refresh, 5000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

function toggle(id: string) {
  selectedId.value = selectedId.value === id ? null : id
}

async function submit() {
  error.value = ''
  try {
    await addDevice(ip.value)
    ip.value = ''
    dialog.value = false
    await refresh()
  } catch (e: any) {
    error.value = e.message
  }
}

async function del(dev: Device) {
  await removeDevice(dev.id)
  if (selectedId.value === dev.id) selectedId.value = null
  await refresh()
}
</script>

<template>
  <v-container fluid>
    <v-row class="mb-2" justify="space-between" align="center">
      <v-col cols="auto">
        <v-btn color="primary" @click="dialog = true">添加设备</v-btn>
      </v-col>
      <v-col cols="auto" class="text-caption text-medium-emphasis">
        {{ devices.length }} 台设备 · {{ onlineCount }} 在线
      </v-col>
    </v-row>

    <div class="layout-row" :class="{ 'has-preview': !!selected }">
      <PreviewPanel v-if="selected" :device="selected" @close="selectedId = null" />
      <div class="grid-slot">
        <div class="thumb-wall">
          <DeviceThumb
            v-for="d in devices"
            :key="d.id"
            :id="d.id"
            :ip="d.ip"
            :name="d.name"
            :online="d.online"
            :selected="selectedId === d.id"
            @select="toggle(d.id)"
            @delete="del(d)"
          />
        </div>
      </div>
    </div>

    <v-dialog v-model="dialog" width="400">
      <v-card>
        <v-card-title>添加设备</v-card-title>
        <v-card-text>
          <v-text-field v-model="ip" label="设备 IP" placeholder="192.168.1.50" />
          <v-alert v-if="error" type="error" density="compact">{{ error }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="submit">添加</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.layout-row {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  flex-wrap: wrap;
}
.grid-slot {
  flex: 1 1 auto;
  min-width: 0;
}
.thumb-wall {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-content: flex-start;
}
</style>
