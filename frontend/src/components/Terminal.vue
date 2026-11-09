<script setup lang="ts">
// One step's Claude Code session: xterm.js on the local WebSocket. The
// server replays the scrollback on connect, so switching tabs reconnects.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { api, errorText } from '../lib/api'
import { herd, resumeCommand } from '../lib/state'
import type { Step } from '../lib/types'
import Icon from './ui/Icon.vue'

// A paused step (stopped with Stop) is not reconnected: the user may be
// running the same session elsewhere, e.g. claude --resume in Zed.
const props = defineProps<{ step: Step }>()
const paused = () => props.step.paused && !props.step.live
const others = () => (props.step.live ? undefined : herd.elsewhere[props.step.id])

async function resumeAnyway() {
  await herd.resumeAnyway(props.step)
  if (!paused()) connect(props.step.id)
}
async function checkAgain() {
  const procs = await api.SessionElsewhere(props.step.id).catch(() => [])
  if (procs.length) herd.elsewhere[props.step.id] = procs
  else {
    delete herd.elsewhere[props.step.id]
    await herd.resumeStep(props.step)
    if (!paused()) connect(props.step.id)
  }
}

const el = ref<HTMLDivElement>()
const ended = ref(false)
const exitCode = ref<number | null>(null)
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

function connect(id: string, resume = false) {
  cleanup?.()
  if (!el.value || (paused() && !resume)) return
  const ap = herd.settings?.config.appearance
  ended.value = false
  exitCode.value = null
  const t = new Terminal({
    fontFamily: `'${ap?.termFont ?? 'Geist Mono'}', 'Geist Mono', monospace`,
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

  // TerminalURL attaches to the live session, or replays one that just ended;
  // Resume explicitly reopens the Claude Code conversation.
  ;(resume ? api.ResumeStep(id) : api.TerminalURL(id))
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
            const m = JSON.parse(ev.data)
            if (m.type === 'exit') {
              ended.value = true
              exitCode.value = typeof m.code === 'number' ? m.code : null
            }
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
    .catch(async (e) => {
      // The resume guard: another process runs this session.
      const procs = await api.SessionElsewhere(id).catch(() => [])
      if (procs.length) herd.elsewhere[id] = procs
      else t.write(`\x1b[31m${errorText(e)}\x1b[0m\r\n`)
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

onMounted(() => connect(props.step.id))
watch(() => props.step.id, (id) => connect(id))
watch(attempt, () => connect(props.step.id, true))
watch(paused, (p) => (p ? cleanup?.() : connect(props.step.id)))
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
    <div v-if="others()" class="paused">
      <span class="ico warn"><Icon name="warn" :size="22" /></span>
      <span class="title">Claude is still running this session elsewhere</span>
      <span class="text">Exit it there first, so only one Claude writes the conversation.</span>
      <span v-for="p in others()" :key="p.pid" class="proc mono">pid {{ p.pid }} · {{ p.cmd }}</span>
      <span class="row">
        <button class="btn" @click="checkAgain">Check again</button>
        <button class="btn danger" @click="resumeAnyway">Resume anyway</button>
      </span>
    </div>
    <div v-else-if="paused()" class="paused">
      <span class="ico"><Icon name="stop" :size="22" /></span>
      <span class="title">Claude is stopped in herd</span>
      <span class="text">Continue this session elsewhere, e.g. in a Zed terminal, with the resume command. Resume here when you are done, so only one Claude runs it at a time.</span>
      <span class="row">
        <button class="btn" @click="herd.copyResume(step)"><Icon name="copy" :size="14" />Copy claude --resume</button>
        <button class="btn primary" @click="herd.resumeStep(step)"><Icon name="play" :size="13" />Resume here</button>
      </span>
    </div>
    <div v-else-if="ended" class="ended">
      <span>Session ended{{ exitCode !== null && exitCode !== 0 ? ` (exit ${exitCode})` : '' }}.</span>
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
.paused {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 48px;
  text-align: center;
  background: var(--bg);
}
.paused .ico {
  display: flex;
  color: var(--faint);
}
.paused .title {
  font-size: 14px;
}
.paused .text {
  max-width: 400px;
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--muted);
}
.paused .ico.warn {
  color: var(--waiting);
}
.paused .proc {
  max-width: 520px;
  padding: 4px 8px;
  border-radius: 4px;
  background: var(--surface);
  font-size: 11.5px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.paused .row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
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
