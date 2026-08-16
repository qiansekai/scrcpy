import { type Ref } from 'vue'

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
    if (e.button !== 0) return // 右键走 contextmenu → BACK，不进触控
    pointerDown = true
    canvas.value?.setPointerCapture(e.pointerId)
    sendTouch(0, e)
  }
  function onPointerMove(e: PointerEvent) {
    if (pointerDown) sendTouch(2, e)
  }
  function onPointerUp(e: PointerEvent) {
    if (e.button !== 0) return
    pointerDown = false
    sendTouch(1, e)
  }
  function onPointerCancel(e: PointerEvent) {
    pointerDown = false
    sendTouch(1, e)
  }
  // 右键返回：挡掉浏览器右键菜单，向设备发 BACK。
  function onContextMenu(e: MouseEvent) {
    e.preventDefault()
    send({ type: 'back' })
  }

  function bind() {
    if (bound) return
    const el = canvas.value
    if (!el) return
    el.addEventListener('pointerdown', onPointerDown)
    el.addEventListener('pointermove', onPointerMove)
    el.addEventListener('pointerup', onPointerUp)
    el.addEventListener('pointercancel', onPointerCancel)
    el.addEventListener('contextmenu', onContextMenu)
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
      el.removeEventListener('contextmenu', onContextMenu)
    }
    bound = false
  }

  return { bind, unbind }
}
