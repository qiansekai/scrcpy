<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useDeviceStore } from '../stores/device'

const store = useDeviceStore()
const dialog = ref(false)
const ip = ref('')
const error = ref('')

onMounted(() => store.refresh())

async function submit() {
  error.value = ''
  try {
    await store.add(ip.value)
    ip.value = ''
    dialog.value = false
  } catch (e: any) {
    error.value = e.message
  }
}
</script>

<template>
  <v-container>
    <v-row justify="space-between" align="center">
      <v-col><v-btn color="primary" @click="dialog = true">添加设备</v-btn></v-col>
    </v-row>
    <v-list>
      <v-list-item
        v-for="d in store.devices"
        :key="d.id"
        :to="`/devices/${d.id}`"
        :title="d.name || d.ip"
        :subtitle="d.ip"
      >
        <template #append>
          <v-chip :color="d.online ? 'success' : 'error'" size="small">
            {{ d.online ? '在线' : '离线' }}
          </v-chip>
          <v-btn icon="mdi-delete" variant="text" @click.stop="store.remove(d.id)" />
        </template>
      </v-list-item>
    </v-list>

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
