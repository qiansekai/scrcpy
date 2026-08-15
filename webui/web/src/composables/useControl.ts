import { ref, type Ref } from 'vue'

// 触控映射：把 canvas 上的指针事件换算成设备像素坐标，经 send() 发给设备。
// DeviceConsole 与 PreviewPanel 共用。dims 来自 useStream 的 session 消息。
export function useControl(
  send: (msg: Record<string, unknown>) => void,
  canvas: Ref<HTMLCanvasElement | null>,
  dims: { width: number; height: number },
) {
  let pointerDown = false
  let bound = false

  function canvasPoint(e: PointerEvent) {
    const el = canvas.value
    if (!el) return null
    if (!dims.width || !dims.height) return null
    const rect = el.getBoundingClientRect()
    return {
      x: ((e.clientX - rect.left) / rect.width) * dims.width,
      y: ((e.clientY - rect.top) / rect.height) * dims.height,
    }
  }

  function sendTouch(action: number, e: PointerEvent) {
    const p = canvasPoint(e)
    if (!p) return
    send({ type: 'touch', action, x: p.x, y: p.y, screenW: dims.width, screenH: dims.height })
  }

  function onPointerDown(e: PointerEvent) {
    pointerDown = true
    canvas.value?.setPointerCapture(e.pointerId)
    sendTouch(0, e)
  }
  function onPointerMove(e: PointerEvent) {
    if (pointerDown) sendTouch(2, e)
  }
  function onPointerUp(e: PointerEvent) {
    pointerDown = false
    sendTouch(1, e)
  }
  function onPointerCancel(e: PointerEvent) {
    pointerDown = false
    sendTouch(1, e)
  }

  function bind() {
    if (bound) return
    const el = canvas.value
    if (!el) return
    el.addEventListener('pointerdown', onPointerDown)
    el.addEventListener('pointermove', onPointerMove)
    el.addEventListener('pointerup', onPointerUp)
    el.addEventListener('pointercancel', onPointerCancel)
    bound = true
  }

  function unbind() {
    if (!bound) return
    const el = canvas.value
    if (el) {
      el.removeEventListener('pointerdown', onPointerDown)
      el.removeEventListener('pointermove', onPointerMove)
      el.removeEventListener('pointerup', onPointerUp)
      el.removeEventListener('pointercancel', onPointerCancel)
    }
    bound = false
  }

  return { bind, unbind }
}
