// 键盘注入：把浏览器按键映射成 scrcpy keycode，经 send() 发给设备。
// 与 useControl 一起用于可控制的预览区/控制台。
const KEYMAP: Record<string, number> = {
  Enter: 66, Backspace: 67, Tab: 61, Space: 62,
  ArrowUp: 19, ArrowDown: 20, ArrowLeft: 21, ArrowRight: 22,
  Home: 122, End: 123, PageUp: 92, PageDown: 93,
  Delete: 67, Escape: 111,
  ShiftLeft: 59, ShiftRight: 59, ControlLeft: 113, ControlRight: 113, AltLeft: 57, AltRight: 57,
}

const LETTERS: Record<string, number> = {
  a: 29, b: 30, c: 31, d: 32, e: 33, f: 34, g: 35, h: 36, i: 37,
  j: 38, k: 39, l: 40, m: 41, n: 42, o: 43, p: 44, q: 45, r: 46,
  s: 47, t: 48, u: 49, v: 50, w: 51, x: 52, y: 53, z: 54,
}

const DIGITS: Record<string, number> = {
  '0': 7, '1': 8, '2': 9, '3': 10, '4': 11, '5': 12, '6': 13, '7': 14, '8': 15, '9': 16,
}

export function useKeyboard(send: (msg: Record<string, unknown>) => void) {
  let bound = false

  function keyToKeycode(e: KeyboardEvent): number | null {
    if (e.code.startsWith('Key')) return LETTERS[e.code.slice(3).toLowerCase()] ?? null
    if (e.code.startsWith('Digit')) return DIGITS[e.code.slice(5)] ?? null
    return KEYMAP[e.code] ?? null
  }

  function sendKey(action: number, e: KeyboardEvent) {
    const code = keyToKeycode(e)
    if (code == null) return
    e.preventDefault()
    send({ type: 'key', action, keycode: code })
  }

  // 剪贴板快捷键：Ctrl+C 拉设备剪贴板，Ctrl+V / Ctrl+Shift+V 推 PC 剪贴板。
  // 命中时不走 key 注入，且 keyup 也要拦掉避免发无配对的 UP。
  function isClipboardShortcut(e: KeyboardEvent): boolean {
    if (!e.ctrlKey) return false
    const t = e.target as HTMLElement | null
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return false
    return e.code === 'KeyC' || e.code === 'KeyV'
  }

  function handleClipboard(e: KeyboardEvent) {
    e.preventDefault()
    if (e.code === 'KeyC' && !e.shiftKey) {
      // copyKey=1 (COPY)：让设备先执行复制，靠 autosync 把剪贴板推回来。
      send({ type: 'getClipboard', copyKey: 1 })
    } else if (e.code === 'KeyV') {
      navigator.clipboard.readText()
        .then((text) => { if (text) send({ type: 'setClipboard', clipboard: text, paste: true }) })
        .catch(() => {})
    }
  }

  function onKeyDown(e: KeyboardEvent) {
    if (isClipboardShortcut(e)) {
      handleClipboard(e)
      return
    }
    sendKey(0, e)
  }
  function onKeyUp(e: KeyboardEvent) {
    if (isClipboardShortcut(e)) {
      e.preventDefault()
      return
    }
    sendKey(1, e)
  }

  function bind() {
    if (bound) return
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    bound = true
  }

  function unbind() {
    if (!bound) return
    window.removeEventListener('keydown', onKeyDown)
    window.removeEventListener('keyup', onKeyUp)
    bound = false
  }

  return { bind, unbind }
}
