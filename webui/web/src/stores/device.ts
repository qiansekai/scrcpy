import { defineStore } from 'pinia'
import { listDevices, addDevice, removeDevice, type Device } from '../api'

export const useDeviceStore = defineStore('device', {
  state: () => ({ devices: [] as Device[] }),
  actions: {
    async refresh() {
      this.devices = await listDevices()
    },
    async add(ip: string) {
      await addDevice(ip)
      await this.refresh()
    },
    async remove(id: string) {
      await removeDevice(id)
      await this.refresh()
    },
  },
})
