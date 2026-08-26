<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  addDevice,
  discover,
  listDevices,
  removeDevice,
  getStatus,
  type Device,
} from '../api'
import DeviceThumb from './DeviceThumb.vue'
import PreviewPanel from './PreviewPanel.vue'
import BatchPanel from './BatchPanel.vue'

const devices = ref<Device[]>([])
const selectedId = ref<string | null>(null)
const dialog = ref(false)
const ip = ref('')
const error = ref('')
const batchDialog = ref(false)

// 多选：勾选的设备 id 集合
const checkedIds = ref<Set<string>>(new Set())
// 主控-被控：主控设备 id；被控集合是"非主控设备上的勾选"
const masterId = ref<string | null>(null)

// 告警 snackbar：可堆叠（数组）
interface AlertItem {
  id: string
  level: 'offline' | 'lowbattery'
  msg: string
  timestamp: number
}
const snackbarQueue = ref<AlertItem[]>([])

let timer: number | null = null

const onlineCount = computed(() => devices.value.filter((d) => d.online).length)
const selected = computed(() => devices.value.find((d) => d.id === selectedId.value) ?? null)

// 已勾选设备（有顺序，供批量结果按提交顺序展示）
const checkedDevices = computed(() => devices.value.filter((d) => checkedIds.value.has(d.id)))

// 被控集合 = 勾选的非主控设备
const slaveIds = computed<string[]>(() =>
  devices.value.filter((d) => d.id !== masterId.value && checkedIds.value.has(d.id)).map((d) => d.id),
)

// 设备显示名
function labelOf(id: string) {
  return devices.value.find((d) => d.id === id)?.name || id
}

async function refresh() {
  try {
    devices.value = await listDevices()
  } catch {
    // backend down: keep last list, next poll retries
  }
  // 清理已不存在的勾选 / 主控
  const exist = new Set(devices.value.map((d) => d.id))
  for (const id of [...checkedIds.value]) {
    if (!exist.has(id)) {
      checkedIds.value.delete(id)
      checkedIds.value = new Set(checkedIds.value)
    }
  }
  if (masterId.value && !exist.has(masterId.value)) masterId.value = null
  if (selectedId.value && !exist.has(selectedId.value)) selectedId.value = null
}

// 定时刷新：列表 + 每台在线设备状态。状态写入 d.status 供缩略图角标使用。
async function refreshStatus() {
  await refresh()
  await Promise.all(
    devices.value
      .filter((d) => d.online)
      .map(async (d) => {
        try {
          const s = await getStatus(d.id)
          d.status = { battery: s.battery, plugged: s.plugged, model: s.model, android: s.android }
        } catch {
          // 单台失败不影响其它
        }
      }),
  )
}

onMounted(() => {
  refreshStatus()
  timer = window.setInterval(refreshStatus, 5000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  clearMaster()
})

function toggle(id: string) {
  selectedId.value = selectedId.value === id ? null : id
}

// 多选切换
function toggleCheck(id: string) {
  checkedIds.value = new Set(checkedIds.value)
  if (checkedIds.value.has(id)) checkedIds.value.delete(id)
  else checkedIds.value.add(id)
  // 勾选变化会影响被控集合（勾选的非主控设备），需重新广播给主控
  broadcastSlaves()
}

// 主控设备的持久 WS：用于发送 setMaster / setSlaves 控制消息。
// 与被控/缩略图的流相互独立，仅承载控制面。
let masterWs: WebSocket | null = null

function masterSend(msg: Record<string, unknown>) {
  if (masterWs && masterWs.readyState === WebSocket.OPEN) {
    masterWs.send(JSON.stringify(msg))
  }
}

// 主控-被控：设置主控（再次点击同一台取消主控）
function setMaster(id: string) {
  if (masterId.value === id) {
    // 取消当前主控：先显式发空串让后端清掉主控路由，再断开控制 WS
    clearMaster()
    return
  }
  masterId.value = id
  connectMaster()
  broadcastSlaves()
}

// 取消主控：先向后端发 {type:"setMaster", master:""}，再断开控制连接。
// 后端 master 状态不会随 WS 断开自动清理，必须显式发空串，否则残留主控路由导致误同步。
function clearMaster() {
  if (masterWs && masterWs.readyState === WebSocket.OPEN) {
    masterWs.send(JSON.stringify({ type: 'setMaster', master: '' }))
  }
  masterWs?.close()
  masterWs = null
  masterId.value = null
}

// 建立/重建主控控制连接（主控变化或离线重连时调用）
function connectMaster() {
  masterWs?.close()
  masterWs = null
  if (!masterId.value) return
  const ws = new WebSocket(`ws://${location.host}/ws/${masterId.value}`)
  masterWs = ws
  ws.onopen = () => {
    masterSend({ type: 'setMaster', master: masterId.value })
    broadcastSlaves()
  }
}

// 把被控集合发给主控（空串 master 取消由 setMaster 的 toggle 语义掩盖，这里只发非空）
function broadcastSlaves() {
  if (!masterId.value) return
  if (masterWs && masterWs.readyState === WebSocket.OPEN) {
    masterSend({ type: 'setSlaves', slaves: slaveIds.value })
  }
}

async function submit() {
  error.value = ''
  try {
    await addDevice(ip.value)
    ip.value = ''
    dialog.value = false
    await refreshStatus()
  } catch (e: any) {
    // 409 等错误：直接展示后端返回文本（如"设备已存在"）
    error.value = e.message
  }
}

async function del(dev: Device) {
  try {
    await removeDevice(dev.id)
  } catch {
    // 删除失败忽略（下一轮刷新兜底）
  }
  if (selectedId.value === dev.id) selectedId.value = null
  if (masterId.value === dev.id) {
    // 主控设备被删除：显式发空串清理主控路由后再断开
    clearMaster()
  }
  if (checkedIds.value.has(dev.id)) {
    checkedIds.value = new Set(checkedIds.value)
    checkedIds.value.delete(dev.id)
  }
  await refreshStatus()
}

// 设备自动发现
async function onDiscover() {
  try {
    const r = await discover()
    const names = r.added.map((d) => d.name || d.ip)
    if (names.length) {
      pushAlert({ id: 'discover', level: 'offline', msg: `发现新设备：${names.join('、')}`, timestamp: Date.now() })
    } else {
      pushAlert({ id: 'discover', level: 'offline', msg: '未发现新设备', timestamp: Date.now() })
    }
  } catch (e: any) {
    pushAlert({ id: 'discover', level: 'offline', msg: `发现失败：${e?.message || '后端不可达'}`, timestamp: Date.now() })
  }
  await refreshStatus()
}

// 告警堆叠：低电量 / 掉线。pushAlert 入队并逐个弹 snackbar。
function pushAlert(item: AlertItem) {
  snackbarQueue.value.push(item)
  if (snackbarQueue.value.length === 1) showNext()
}

// 当前展示中的 snackbar 文本（取队列头部）
const showingAlert = ref<AlertItem | null>(null)
const snackbarVisible = ref(false)

function showNext() {
  const cur = snackbarQueue.value[0]
  if (!cur) {
    showingAlert.value = null
    snackbarVisible.value = false
    return
  }
  showingAlert.value = cur
  snackbarVisible.value = true
}

function onSnackbarClose() {
  snackbarVisible.value = false
  // 弹出后移出队列，展示下一条
  snackbarQueue.value.shift()
  showNext()
}

// 供 PreviewPanel 回调：状态广播实时更新设备卡
function onDeviceStatus(id: string, s: { battery: number; plugged: boolean; model: string; android: string }) {
  const d = devices.value.find((x) => x.id === id)
  if (!d) return
  d.status = s
}

// 收到 alert 消息：补上时间戳后入队提示
function onDeviceAlert(alert: { id: string; level: 'offline' | 'lowbattery'; msg: string }) {
  pushAlert({ ...alert, timestamp: Date.now() })
}
</script>

<template>
  <v-container fluid>
    <v-row class="mb-2" align="center" justify="space-between">
      <v-col cols="auto" class="d-flex ga-2 flex-wrap">
        <v-btn color="primary" @click="dialog = true">添加设备</v-btn>
        <v-btn variant="tonal" @click="onDiscover">发现设备</v-btn>
        <v-btn
          variant="tonal"
          color="secondary"
          :disabled="checkedDevices.length === 0"
          @click="batchDialog = true"
        >
          批量操作 ({{ checkedDevices.length }})
        </v-btn>
        <v-btn
          variant="tonal"
          color="warning"
          :disabled="!masterId"
          @click="setMaster(masterId!)"
        >
          取消主控
        </v-btn>
      </v-col>
      <v-col cols="auto" class="text-caption text-medium-emphasis">
        {{ devices.length }} 台设备 · {{ onlineCount }} 在线
        <span v-if="masterId"> · 主控：{{ labelOf(masterId) }}</span>
        <span v-if="slaveIds.length"> · 被控 {{ slaveIds.length }} 台</span>
      </v-col>
    </v-row>

    <div class="layout-row" :class="{ 'has-preview': !!selected }">
      <PreviewPanel
        v-if="selected"
        :device="selected"
        :is-master="selected.id === masterId"
        :is-slave="slaveIds.includes(selected.id)"
        @close="selectedId = null"
        @set-master="setMaster(selected.id)"
        @status="onDeviceStatus(selected.id, $event)"
        @alert="onDeviceAlert"
      />
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
            :checked="checkedIds.has(d.id)"
            :is-master="masterId === d.id"
            :is-slave="slaveIds.includes(d.id)"
            :status="d.status"
            @select="toggle(d.id)"
            @delete="del(d)"
            @toggle-check="toggleCheck(d.id)"
            @alert="onDeviceAlert"
          />
        </div>
      </div>
    </div>

    <!-- 主控-被控辅助说明：被控集合 = 勾选的非主控设备 -->
    <div v-if="masterId" class="text-caption text-medium-emphasis mt-2">
      主控模式下，被控集合为下方复选框勾选的设备（不含主控本身）；取消勾选即移出被控。
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

    <!-- 批量操作对话框 -->
    <v-dialog v-model="batchDialog" width="720" scrollable>
      <BatchPanel v-if="batchDialog" :devices="checkedDevices" @close="batchDialog = false" />
    </v-dialog>

    <!-- 告警 snackbar：颜色随级别 -->
    <v-snackbar
      v-model="snackbarVisible"
      :color="showingAlert?.level === 'lowbattery' ? 'warning' : 'error'"
      :timeout="5000"
      @update:model-value="onSnackbarClose"
    >
      <template v-if="showingAlert">{{ showingAlert.msg }}</template>
    </v-snackbar>
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
