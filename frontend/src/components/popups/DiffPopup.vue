<script setup lang="ts">
// The changes of a step, large: the changed files on the left (as in the
// Changes tab), the selected file's read-only diff on the right.
import { computed, onMounted, ref, watch } from 'vue'
import { api, errorText } from '../../lib/api'
import { herd } from '../../lib/state'
import type { Changes, FileDiff } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import DiffView from '../Diff.vue'
import ChangedFiles, { split, statusCls, statusTitle } from '../ChangedFiles.vue'

const stepId = computed(() => herd.diffOf?.step ?? '')
const step = computed(() => herd.tasks.flatMap((t) => t.steps).find((s) => s.id === stepId.value))
const changes = ref<Changes | null>(null)
const diff = ref<FileDiff | null>(null)
const error = ref('')
const sel = computed(() => herd.diffOf?.path ?? '')
const file = computed(() => changes.value?.files.find((f) => f.path === sel.value))

let cseq = 0
async function loadChanges() {
  const mine = ++cseq
  const c = await api.StepChanges(stepId.value).catch(() => null)
  if (mine !== cseq) return
  changes.value = c
  // The file left the changes (e.g. reverted): show the first one instead.
  if (c && c.files.length && !c.files.some((f) => f.path === sel.value) && herd.diffOf) herd.diffOf.path = c.files[0].path
}
let dseq = 0
async function loadDiff() {
  const mine = ++dseq
  if (!sel.value) return
  try {
    const d = await api.FileDiff(stepId.value, sel.value)
    if (mine === dseq) (diff.value = d), (error.value = '')
  } catch (e) {
    if (mine === dseq) (diff.value = null), (error.value = errorText(e))
  }
}
watch(stepId, loadChanges, { immediate: true })
watch(sel, () => {
  diff.value = null
  error.value = ''
  void loadDiff()
}, { immediate: true })
// Uncommitted changes keep moving while the agent works.
let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => [step.value?.sha, step.value?.state, herd.selTask?.steps.length] as const,
  () => {
    clearTimeout(timer)
    timer = setTimeout(() => void loadChanges().then(loadDiff), 800)
  },
)

const pick = (path: string) => herd.diffOf && (herd.diffOf.path = path)
const close = () => {
  herd.overlay = 'none'
  herd.diffOf = null
}

// Zed shows this step's own diff: its commit (an Implement's is its
// Review's), in the task's worktree; uncommitted work opens the worktree.
async function openInZed() {
  const s = step.value
  if (!s) return
  const lines = await herd.attempt('Could not open Zed', () => api.OpenInZed(s.taskId, s.id))
  if (lines) herd.toast('Opening the diff in Zed', lines.join('\n'))
}

const box = ref<HTMLDivElement>()
onMounted(() => box.value?.focus())

// Up and down move through the files.
function key(e: KeyboardEvent) {
  const files = changes.value?.files ?? []
  const i = files.findIndex((f) => f.path === sel.value)
  if (e.key === 'ArrowDown' && i < files.length - 1) pick(files[i + 1].path)
  else if (e.key === 'ArrowUp' && i > 0) pick(files[i - 1].path)
  else return
  e.preventDefault()
}
</script>

<template>
  <button class="scrim dim" aria-label="Close changes" @click="close"></button>
  <div ref="box" class="box popup" role="dialog" aria-label="Changes" tabindex="-1" @keydown="key">
    <div class="head">
      <span class="title">{{ step?.name ?? 'Step' }} changes</span>
      <span class="grow"></span>
      <button class="x" aria-label="Close" title="Close (Esc)" @click="close"><Icon name="close" :size="14" /></button>
    </div>
    <div class="body">
      <div class="side">
        <p v-if="!changes" class="empty">Loading…</p>
        <p v-else-if="!changes.files.length" class="empty">No changed files.</p>
        <ChangedFiles v-else :changes="changes" :selected="sel" @pick="pick" />
      </div>
      <div class="main">
        <div v-if="file" class="fhead">
          <span :class="['st', statusCls(file.status)]" :title="statusTitle(file.status)">{{ file.status === '?' ? 'U' : file.status }}</span>
          <span class="path mono" :title="file.path"><span class="dir">{{ split(file.path).dir }}</span>{{ split(file.path).name }}</span>
          <button class="zed" :title="changes?.source === 'commit' ? `Open commit ${changes.sha} in Zed's diff view` : 'Open the worktree in Zed, where it shows the uncommitted changes'" @click="openInZed">Zed diff</button>
        </div>
        <div class="scroll">
          <p v-if="error" class="empty err">{{ error }}</p>
          <p v-else-if="!diff" class="empty">Loading…</p>
          <p v-else-if="diff.binary" class="empty">Binary file: no text diff.</p>
          <p v-else-if="!diff.diff" class="empty">No text changes (mode or whitespace only).</p>
          <template v-else>
            <DiffView :diff="diff.diff" />
            <p v-if="diff.truncated" class="empty">The diff is too long to show in full; open the file in Zed for the rest.</p>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .box {
    position: absolute;
    z-index: 21;
    top: 48px;
    bottom: 40px;
    left: 50%;
    transform: translateX(-50%);
    width: min(1400px, calc(100% - 48px));
    display: flex;
    flex-direction: column;
    outline: none;
  }
  .head {
    display: flex;
    align-items: center;
    flex: none;
    height: 40px;
    padding: 0 8px 0 14px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    font-weight: 600;
  }
  .grow {
    flex: 1;
  }
  .x {
    width: 26px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .x:hover {
    background: var(--hover);
    color: var(--text);
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
  }
  .side {
    flex: none;
    width: 300px;
    max-width: 35%;
    padding: 12px 14px;
    border-right: 1px solid var(--border);
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .fhead {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 36px;
    padding: 0 10px 0 14px;
    border-bottom: 1px solid var(--border);
    font-size: 12.5px;
  }
  .st {
    flex: none;
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 600;
  }
  .path {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dir {
    color: var(--muted);
  }
  .zed {
    flex: none;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
  .zed:hover:not(:disabled) {
    background: var(--hover);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  .empty {
    margin: 12px 14px;
    color: var(--muted);
    font-size: 12.5px;
  }
  .side .empty {
    margin: 0;
  }
  .err {
    color: var(--approval);
  }
  .c-add { color: var(--done); }
  .c-del { color: var(--approval); }
  .c-mod { color: var(--waiting); }
</style>
