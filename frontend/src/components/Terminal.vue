<script setup lang="ts">
// One step's Claude Code session: xterm.js on the local WebSocket. The
// server replays the scrollback on connect, so switching tabs reconnects.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { api, errorText } from '../lib/api'
import { herd } from '../lib/state'

const props = defineProps<{ stepId: string }>()

const el = ref<HTMLDivElement>()
const ended = ref(false)
const attempt = ref(0)
let term: Terminal | null = null
let cleanup: (() => void) | null = null

function cssTheme() {
  const s = getComputedStyle(document.documentElement)
  const v = (n: string) => s.getPropertyValue(n).trim()
  return {
    background: v('--bg'), foreground: v('--text'), cursor: v('--text'), cursorAccent: v('--bg'), selectionBackground: v('--ansi-selection'),
    black: v('--ansi-black'), red: v('--ansi-red'), green: v('--ansi-green'), yellow: v('--ansi-yellow'), blue: v('--ansi-blue'),
    magenta: v('--ansi-magenta'), cyan: v('--ansi-cyan'), white: v('--ansi-white'), brightBlack: v('--ansi-bright-black'),
    brightRed: v('--ansi-red'), brightGreen: v('--ansi-green'), brightYellow: v('--ansi-yellow'), brightBlue: v('--ansi-blue'),
    brightMagenta: v('--ansi-magenta'), brightCyan: v('--ansi-cyan'), brightWhite: v('--text'),
  }
}

function connect(id: string) {
  cleanup?.()
  if (!el.value) return
  const ap = herd.settings?.config.appearance
  ended.value = false
  const t = new Terminal({
    fontFamily: `'${ap?.termFont ?? 'IBM Plex Mono'}', 'IBM Plex Mono', monospace`,
    fontSize: ap?.termFontSize ?? 12.5,
    lineHeight: 1.25,
    cursorBlink: true,
    scrollback: 10000,
    theme: cssTheme(),
    allowProposedApi: true,
  })
  term = t
  const fit = new FitAddon()
  t.loadAddon(fit)
  t.open(el.value)
  const enc = new TextEncoder()
  let ws: WebSocket | null = null
  let disposed = false

  const sendSize = () => {
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'resize', cols: t.cols, rows: t.rows }))
  }
  const refit = () => {
    try {
      fit.fit()
    } catch {
      /* hidden */
    }
  }
  requestAnimationFrame(refit)

  api
    .TerminalURL(id)
    .then(async (url) => {
      if (disposed) return
      if (url.startsWith('mock:')) {
        const m = await import('../lib/mock')
        t.write(m.mockTerminal(id))
        t.write('\x1b[90m(mock session: run the desktop app for a live Claude Code terminal)\x1b[0m\r\n> ')
        return
      }
      ws = new WebSocket(url)
      ws.binaryType = 'arraybuffer'
      ws.onopen = () => {
        refit()
        sendSize()
        t.focus()
      }
      ws.onmessage = (ev) => {
        if (typeof ev.data === 'string') {
          try {
            if (JSON.parse(ev.data).type === 'exit') ended.value = true
          } catch {
            /* ignore */
          }
          return
        }
        t.write(new Uint8Array(ev.data))
      }
      ws.onclose = () => {
        if (!disposed) ended.value = true
      }
    })
    .catch((e) => {
      t.write(`\x1b[31m${errorText(e)}\x1b[0m\r\n`)
      ended.value = true
    })

  const d1 = t.onData((d) => ws?.readyState === WebSocket.OPEN && ws.send(enc.encode(d)))
  const d2 = t.onBinary((d) => {
    if (ws?.readyState !== WebSocket.OPEN) return
    const b = new Uint8Array(d.length)
    for (let i = 0; i < d.length; i++) b[i] = d.charCodeAt(i) & 0xff
    ws.send(b)
  })
  const d3 = t.onResize(sendSize)
  const ro = new ResizeObserver(refit)
  ro.observe(el.value)

  cleanup = () => {
    disposed = true
    ro.disconnect()
    d1.dispose()
    d2.dispose()
    d3.dispose()
    ws?.close()
    t.dispose()
    term = null
    cleanup = null
  }
}

onMounted(() => connect(props.stepId))
watch([() => props.stepId, attempt], () => connect(props.stepId))
onBeforeUnmount(() => cleanup?.())

// Live theme switch.
watch(
  () => herd.theme,
  () =>
    requestAnimationFrame(() => {
      if (term) term.options.theme = cssTheme()
    }),
)
</script>

<template>
  <div class="wrap">
    <div ref="el" class="term"></div>
    <div v-if="ended" class="ended">
      <span>Session ended.</span>
      <button class="btn" @click="attempt++">Resume</button>
    </div>
  </div>
</template>

<style scoped>
.wrap {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  padding: 8px 4px 4px 12px;
  background: var(--bg);
}
.term {
  flex: 1;
  min-width: 0;
  min-height: 0;
}
.term :deep(.xterm-viewport) {
  background: var(--bg) !important;
}
.ended {
  position: absolute;
  left: 50%;
  bottom: 16px;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px 6px 12px;
  border-radius: 8px;
  background: var(--elev);
  border: 1px solid var(--border);
  box-shadow: var(--shadow);
  color: var(--muted);
  font-size: 12.5px;
}
</style>
