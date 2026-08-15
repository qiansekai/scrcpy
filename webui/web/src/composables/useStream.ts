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
  opts?: { throttle?: number; fixedCanvas?: boolean; noAudio?: boolean },
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
  let opusConfig: { description: Uint8Array; sampleRate: number; numberOfChannels: number } | null = null
  let audioStarted = false
  let playWhen = 0
  const ctx = canvas.getContext('2d')
  const connected = ref(false)
  const dims = reactive({ width: 0, height: 0 })

  function ensureAudioCtx() {
    if (!audioCtx) {
      // interactive 模式把 AudioContext 输出缓冲降到 ~20ms，显著降低可听延迟
      audioCtx = new AudioContext({ latencyHint: 'interactive' })
    }
    if (audioCtx.state === 'suspended') audioCtx.resume().catch(() => {})
    return audioCtx
  }

  // 设备端 fixOpusConfigPacket 已把音频配置帧 payload 裁成纯 OpusHead。
  // 解析出 AudioDecoderConfig 需要的 sampleRate/numberOfChannels。
  function parseOpusHead(payload: Uint8Array): { description: Uint8Array; sampleRate: number; numberOfChannels: number } | null {
    if (payload.length < 16) return null
    const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
    return {
      description: payload,
      sampleRate: dv.getUint32(12, true),
      numberOfChannels: payload[9],
    }
  }

  // 用 createBufferSource + 时间调度播放。缓冲量是平滑与延迟的权衡：
  // 首帧缓冲 60ms（低延迟），播放时钟双向贴合 currentTime——落后就追，
  // 超前太多就拉回，避免延迟在帧间累积。
  function playAudioData(audioData: AudioData) {
    const actx = ensureAudioCtx()
    const { numberOfChannels, numberOfFrames, sampleRate, format } = audioData
    const buf = actx.createBuffer(numberOfChannels, numberOfFrames, sampleRate)
    if (format === 'f32-planar' || format === 's16-planar') {
      for (let ch = 0; ch < numberOfChannels; ch++) {
        const bytes = audioData.allocationSize({ planeIndex: ch })
        const chData = new Float32Array(bytes / 4)
        audioData.copyTo(chData, { planeIndex: ch })
        buf.copyToChannel(chData, ch)
      }
    } else {
      // interleaved（如 f32）：整块拷贝后拆到各 channel。
      const inter = new Float32Array(numberOfFrames * numberOfChannels)
      audioData.copyTo(inter, { planeIndex: 0 })
      for (let ch = 0; ch < numberOfChannels; ch++) {
        const chData = new Float32Array(numberOfFrames)
        for (let i = 0; i < numberOfFrames; i++) {
          chData[i] = inter[i * numberOfChannels + ch]
        }
        buf.copyToChannel(chData, ch)
      }
    }
    audioData.close()
    const src = actx.createBufferSource()
    src.buffer = buf
    src.connect(actx.destination)
    if (!audioStarted) {
      playWhen = actx.currentTime + 0.03
      audioStarted = true
    } else {
      if (playWhen < actx.currentTime - 0.02) {
        playWhen = actx.currentTime + 0.02 // 落后太多，追上
      } else if (playWhen > actx.currentTime + 0.08) {
        playWhen = actx.currentTime + 0.03 // 超前累积，拉回
      }
    }
    src.start(playWhen)
    playWhen += buf.duration
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
    if (opts?.noAudio) return // 缩略图不播音频，只有选中预览出声
    const isConfig = (flags & 0x01) !== 0
    if (isConfig) {
      opusConfig = parseOpusHead(payload)
      if (!opusConfig) {
        console.warn('unable to parse OpusHead from config frame')
        return
      }
      setupAudioDecoder()
      if (audioDecoder && audioDecoder.state === 'unconfigured') {
        try {
          audioDecoder.configure({
            codec: 'opus',
            sampleRate: opusConfig.sampleRate,
            numberOfChannels: opusConfig.numberOfChannels,
            description: opusConfig.description,
          })
        } catch (e) {
          console.error('AudioDecoder configure failed', e)
        }
      }
      return
    }
    if (!audioDecoder || audioDecoder.state !== 'configured') return
    try {
      audioDecoder.decode(new EncodedAudioChunk({
        type: 'key', // Opus 每包独立可解码；WebCodecs 音频首块需 key
        timestamp: performance.now() * 1000,
        data: payload,
      }))
    } catch (e) {
      console.error('AudioDecoder decode failed', e)
    }
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
    opusConfig = null
    audioStarted = false
    playWhen = 0
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
