import { reactive, ref, type Ref } from 'vue'

export interface StreamHandle {
  connected: Ref<boolean>
  dims: { width: number; height: number }
  connect: () => Promise<void>
  disconnect: () => void
  send: (msg: Record<string, unknown>) => void
}

export function useStream(
  deviceId: string,
  canvas: HTMLCanvasElement,
  opts?: { throttle?: number; fixedCanvas?: boolean },
): StreamHandle {
  let ws: WebSocket | null = null
  let decoder: VideoDecoder | null = null
  let configData: Uint8Array | null = null
  let codecString: string | null = null
  let width = 0
  let height = 0
  let firstKeySeen = false
  let frameCount = 0
  let firstDrawn = false
  // 音频（设备默认 Opus）
  let audioDecoder: AudioDecoder | null = null
  let audioCtx: AudioContext | null = null
  let opusHead: Uint8Array | null = null
  const ctx = canvas.getContext('2d')
  const connected = ref(false)
  const dims = reactive({ width: 0, height: 0 })

  function ensureAudioCtx() {
    if (!audioCtx) audioCtx = new AudioContext()
    if (audioCtx.state === 'suspended') audioCtx.resume().catch(() => {})
    return audioCtx
  }

  // 设备 Opus 配置帧 = AOPUSHDR(8) + size(8, native 小端) + OpusHead(size)。
  function extractOpusHead(payload: Uint8Array): Uint8Array | null {
    if (payload.length < 16) return null
    const id = String.fromCharCode(...payload.slice(0, 8))
    if (id !== 'AOPUSHDR') return null
    const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
    const size = Number(dv.getBigUint64(8, true))
    if (size <= 0 || 16 + size > payload.length) return null
    return payload.slice(16, 16 + size)
  }

  function playAudioData(audioData: AudioData) {
    const actx = ensureAudioCtx()
    const { numberOfChannels, numberOfFrames, sampleRate } = audioData
    const buf = actx.createBuffer(numberOfChannels, numberOfFrames, sampleRate)
    for (let ch = 0; ch < numberOfChannels; ch++) {
      const chData = new Float32Array(numberOfFrames)
      audioData.copyTo(chData, { planeIndex: ch })
      buf.copyToChannel(chData, ch)
    }
    audioData.close()
    const src = actx.createBufferSource()
    src.buffer = buf
    src.connect(actx.destination)
    src.start()
  }

  function setupAudioDecoder() {
    if (audioDecoder) {
      try {
        audioDecoder.close()
      } catch {
        // already closed
      }
    }
    audioDecoder = new AudioDecoder({
      output(audioData) {
        playAudioData(audioData)
      },
      error(e) {
        console.error('AudioDecoder error', e)
      },
    })
  }

  function handleAudioFrame(flags: number, payload: Uint8Array) {
    const isConfig = (flags & 0x01) !== 0
    if (isConfig) {
      opusHead = extractOpusHead(payload)
      if (!opusHead) {
        console.warn('unable to extract OpusHead from config frame')
        return
      }
      setupAudioDecoder()
      if (audioDecoder && audioDecoder.state === 'unconfigured') {
        try {
          audioDecoder.configure({ codec: 'opus', description: opusHead })
        } catch (e) {
          console.error('AudioDecoder configure failed', e)
        }
      }
      return
    }
    if (!audioDecoder || audioDecoder.state !== 'configured') return
    audioDecoder.decode(new EncodedAudioChunk({
      type: 'delta',
      timestamp: performance.now() * 1000,
      data: payload,
    }))
  }

  function closeAudio() {
    if (audioDecoder) {
      try {
        audioDecoder.close()
      } catch {
        // already closed
      }
      audioDecoder = null
    }
    audioCtx?.close().catch(() => {})
    audioCtx = null
    opusHead = null
  }

  function setupDecoder() {
    decoder?.close()
    firstKeySeen = false
    decoder = new VideoDecoder({
      output(frame) {
        if (!width || !height) {
          width = frame.displayWidth
          height = frame.displayHeight
          if (!opts?.fixedCanvas) {
            canvas.width = width
            canvas.height = height
          }
        }
        frameCount++
        // 第一帧永远画（静态屏帧少，节流不能跳过初始画面），之后按 throttle 抽帧。
        const throttled = opts?.throttle && opts.throttle > 1 && frameCount % opts.throttle !== 0
        if (firstDrawn && throttled) {
          frame.close()
          return
        }
        firstDrawn = true
        if (ctx) ctx.drawImage(frame, 0, 0, canvas.width, canvas.height)
        frame.close()
      },
      error(e) {
        console.error('VideoDecoder error', e)
      },
    })
  }

  // 从配置帧的 AnnexB SPS 提取 profile/constraints，构建 codec 字符串。
  // SPS 的 level_idc 在部分编码器上不可靠（真机写 1.0 但实际是 1080x2400），
  // 因此 level 固定 L5.2 (0x34) 覆盖该分辨率各帧率。
  function deriveCodec(configPayload: Uint8Array): string | null {
    for (let i = 0; i + 4 < configPayload.length; i++) {
      if (configPayload[i] === 0 && configPayload[i + 1] === 0 && configPayload[i + 2] === 0 && configPayload[i + 3] === 1) {
        const nal = i + 4
        if (configPayload[nal] === 0x67 && nal + 3 < configPayload.length) {
          const hex = (v: number) => v.toString(16).padStart(2, '0')
          return `avc1.${hex(configPayload[nal + 1])}${hex(configPayload[nal + 2])}34`
        }
      }
    }
    return null
  }

  function handleFrame(flags: number, payload: Uint8Array) {
    const isConfig = (flags & 0x01) !== 0
    const isKey = (flags & 0x02) !== 0
    if (isConfig) {
      configData = payload
      if (!codecString) codecString = deriveCodec(payload)
    }
    if (!decoder || decoder.state === 'closed') setupDecoder()
    if (decoder && decoder.state === 'unconfigured' && codecString) {
      // 不传 description：负载是 AnnexB，传了会让 Chrome 期待 AVCC 长度前缀而报错。
      decoder.configure({ codec: codecString, optimizeForLatency: true, avc: { format: 'annexb' } })
    }
    if (!decoder || decoder.state !== 'configured') return
    if (isConfig) return // 参数集帧无画面，跳过；SPS/PPS 会前置到首个 IDR
    if (!firstKeySeen) {
      if (!isKey) return
      firstKeySeen = true
      if (configData) {
        // Chrome 的 annexb 模式需要关键帧带内 SPS/PPS 才能初始化；
        // 设备把参数集放在单独的 config 帧，这里前置到首个 IDR 一起喂。
        const merged = new Uint8Array(configData.length + payload.length)
        merged.set(configData, 0)
        merged.set(payload, configData.length)
        decoder.decode(new EncodedVideoChunk({
          type: 'key',
          timestamp: performance.now() * 1000,
          data: merged,
        }))
        return
      }
    }
    decoder.decode(new EncodedVideoChunk({
      type: isKey ? 'key' : 'delta',
      timestamp: performance.now() * 1000,
      data: payload,
    }))
  }

  function handleBinary(buf: ArrayBuffer) {
    const u8 = new Uint8Array(buf)
    if (u8.length < 2) return
    if (u8[0] === 0x01) {
      handleFrame(u8[1], u8.slice(2))
    } else if (u8[0] === 0x02) {
      handleAudioFrame(u8[1], u8.slice(2))
    }
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
                if (!opts?.fixedCanvas) {
                  canvas.width = width
                  canvas.height = height
                }
                dims.width = width
                dims.height = height
                // 新会话 = 新流（重连/旋转换分辨率）：重建解码器并清掉旧参数，
                // 否则旧 SPS/PPS 与首帧 IDR 不匹配，旋转后会花屏。
                setupDecoder()
                codecString = null
                configData = null
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
          closeAudio()
        }
      })
    },
    disconnect() {
      ws?.close()
      decoder?.close()
      decoder = null
      closeAudio()
    },
    send(msg) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg))
      }
    },
  }
}
