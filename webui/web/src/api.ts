// 设备状态：来自 GET /api/devices/{id}/status 与 WS 的 {type:"status"} 广播。
// battery 为 -1 表示未知（设备离线或无法采样）。
export interface DeviceStatus {
  battery: number
  plugged: boolean
  model: string
  android: string
}

export interface Device {
  id: string
  ip: string
  online: boolean
  name?: string
  battery?: number
  plugged?: boolean
  model?: string
  android?: string
  status?: DeviceStatus
}

// 单台 exec 结果（POST /api/devices/{id}/exec）
export interface ExecResult {
  exitCode: number
  stdout: string
  stderr: string
}

// 批量任务单台结果（batch/exec、batch/install、batch/push 通用）
export interface BatchItemResult {
  id: string
  ok: boolean
  error?: string
  exitCode?: number
  stdout?: string
  stderr?: string
  remote?: string
}

export interface BatchResult {
  results: BatchItemResult[]
}

export interface DiscoverResult {
  added: Device[]
}

// 统一的错误处理：非 2xx 一律抛出带后端返回文本的异常，
// 调用方直接 catch 并展示 e.message（对应"设备已存在"等 409 提示）。
async function checkOk(r: Response): Promise<Response> {
  if (!r.ok) {
    let msg = ''
    try {
      msg = (await r.text()).trim()
    } catch {
      // ignore
    }
    throw new Error(msg || `请求失败 (${r.status})`)
  }
  return r
}

export async function listDevices(): Promise<Device[]> {
  const r = await fetch('/api/devices')
  await checkOk(r)
  return r.json()
}

export async function addDevice(ip: string): Promise<Device> {
  const r = await fetch('/api/devices', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ip }),
  })
  await checkOk(r)
  return r.json()
}

export async function removeDevice(id: string): Promise<void> {
  const r = await fetch(`/api/devices/${id}`, { method: 'DELETE' })
  await checkOk(r)
}

// 单台设备执行命令
export async function execCommand(id: string, cmd: string): Promise<ExecResult> {
  const r = await fetch(`/api/devices/${id}/exec`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cmd }),
  })
  await checkOk(r)
  return r.json()
}

// 批量执行命令
export async function batchExec(ids: string[], cmd: string): Promise<BatchResult> {
  const r = await fetch('/api/devices/batch/exec', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids, cmd }),
  })
  await checkOk(r)
  return r.json()
}

// 批量装 APK：multipart 上传，file=apk 文件，ids=JSON 数组字符串
export async function batchInstall(ids: string[], file: File): Promise<BatchResult> {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('ids', JSON.stringify(ids))
  const r = await fetch('/api/devices/batch/install', { method: 'POST', body: fd })
  await checkOk(r)
  return r.json()
}

// 批量推文件：multipart 上传，file=文件，remote=目标路径，ids=JSON 数组字符串
export async function batchPush(ids: string[], file: File, remote: string): Promise<BatchResult> {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('remote', remote)
  fd.append('ids', JSON.stringify(ids))
  const r = await fetch('/api/devices/batch/push', { method: 'POST', body: fd })
  await checkOk(r)
  return r.json()
}

// 获取单台设备状态
export async function getStatus(id: string): Promise<DeviceStatus & { id: string; online: boolean }> {
  const r = await fetch(`/api/devices/${id}/status`)
  await checkOk(r)
  return r.json()
}

// 局域网设备自动发现，返回新增列表
export async function discover(): Promise<DiscoverResult> {
  const r = await fetch('/api/devices/discover', { method: 'POST' })
  await checkOk(r)
  return r.json()
}
