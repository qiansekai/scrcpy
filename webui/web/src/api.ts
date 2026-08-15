export interface Device {
  id: string
  ip: string
  online: boolean
  name?: string
}

export async function listDevices(): Promise<Device[]> {
  const r = await fetch('/api/devices')
  return r.json()
}

export async function addDevice(ip: string): Promise<Device> {
  const r = await fetch('/api/devices', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ip }),
  })
  if (!r.ok) throw new Error((await r.text()) || '添加失败')
  return r.json()
}

export async function removeDevice(id: string): Promise<void> {
  await fetch(`/api/devices/${id}`, { method: 'DELETE' })
}
