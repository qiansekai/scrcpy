import { reactive, ref, type Ref } from 'vue'

export interface StreamHandle {
  connected: Ref<boolean>
  dims: { width: number; height: number }
  connect: () => Promise<void>
  disconnect: () => void
  send: (msg: Record<string, unknown>) => void
}

export function useStream(deviceId: string, canvas: HTMLCanvasElement): StreamHandle {
  let ws: WebSocket | null = null
  let decoder: VideoDecoder | null = null
  let configData: Uint8Array | null = null
  let width = 0
  let height = 0
  let firstKeySeen = false
  const ctx = canvas.getContext('2d')
  const connected = ref(false)
  const dims = reactive({ width: 0, height: 0 })

  function setupDecoder() {
    decoder?.close()
    firstKeySeen = false
    decoder = new VideoDecoder({
      output(frame) {
        if (!width || !height) {
          width = frame.displayWidth
          height = frame.displayHeight
          canvas.width = width
          canvas.height = height
        }
        if (ctx) ctx.drawImage(frame, 0, 0, canvas.width, canvas.height)
        frame.close()
      },
      error(e) {
        console.error('VideoDecoder error', e)
      },
    })
  }

  function ensureDecoder() {
    if (!decoder || decoder.state === 'closed') setupDecoder()
    if (decoder && decoder.state === 'unconfigured') {
      decoder.configure({ codec: 'avc1.640028', optimizeForLatency: true })
    }
  }

  function handleFrame(flags: number, payload: Uint8Array) {
    const isConfig = (flags & 0x01) !== 0
    const isKey = (flags & 0x02) !== 0
    if (isConfig) configData = payload
    ensureDecoder()
    if (!decoder || decoder.state !== 'configured') return
    if (!firstKeySeen) {
      if (!isKey && !isConfig) return
      firstKeySeen = true
      if (configData && configData !== payload) {
        decoder.decode(new EncodedVideoChunk({
          type: 'key',
          timestamp: performance.now() * 1000,
          data: configData,
        }))
      }
    }
    decoder.decode(new EncodedVideoChunk({
      type: isKey || isConfig ? 'key' : 'delta',
      timestamp: performance.now() * 1000,
      data: payload,
    }))
  }

  function handleBinary(buf: ArrayBuffer) {
    const u8 = new Uint8Array(buf)
    if (u8.length < 2 || u8[0] !== 0x01) return
    handleFrame(u8[1], u8.slice(2))
  }

  return {
    connected,
    dims,
    async connect() {
      setupDecoder()
      connected.value = false
      await new Promise<void>((resolve, reject) => {
        ws = new WebSocket(`ws://${location.host}/ws/${deviceId}`)
        ws.binaryType = 'arraybuffer'
        ws.onopen = () => {
          connected.value = true
          resolve()
        }
        ws.onerror = () => {
          connected.value = false
          reject(new Error('WS 连接失败'))
        }
        ws.onmessage = (ev) => {
          if (typeof ev.data === 'string') {
            try {
              const m = JSON.parse(ev.data)
              if (m.type === 'session') {
                width = m.width
                height = m.height
                canvas.width = width
                canvas.height = height
                dims.width = width
                dims.height = height
              }
            } catch (e) {
              console.error('WS 消息解析失败', e)
            }
            return
          }
          handleBinary(ev.data as ArrayBuffer)
        }
        ws.onclose = () => {
          connected.value = false
          decoder?.close()
          decoder = null
        }
      })
    },
    disconnect() {
      ws?.close()
      decoder?.close()
      decoder = null
    },
    send(msg) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg))
      }
    },
  }
}
