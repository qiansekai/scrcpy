<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { addDevice, listDevices, removeDevice, type Device } from '../api'
import DeviceThumb from './DeviceThumb.vue'

const devices = ref<Device[]>([])
const dialog = ref(false)
const ip = ref('')
const error = ref('')
let timer: number | null = null

async function refresh() {
  try {
    devices.value = await listDevices()
  } catch {
    // backend down: keep last list, next poll retries
  }
}

onMounted(() => {
  refresh()
  timer = window.setInterval(refresh, 5000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

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
        {{ devices.length }} 台设备
      </v-col>
    </v-row>

    <v-row dense>
      <v-col
        v-for="d in devices"
        :key="d.id"
        cols="6" sm="4" md="3" lg="2"
      >
        <DeviceThumb :id="d.id" :ip="d.ip" :name="d.name" :online="d.online" @delete="del(d)" />
      </v-col>
    </v-row>

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
