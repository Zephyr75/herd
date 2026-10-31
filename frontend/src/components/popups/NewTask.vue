<script setup lang="ts">
// New task (⌘N): a project, then an issue, a description, or both.
import { computed, onMounted, ref } from 'vue'
import { api, errorText } from '../../lib/api'
import { herd } from '../../lib/state'
import { isMac } from '../../lib/keymap'
import Icon from '../ui/Icon.vue'

// herd.previewNew pre-fills the preview's artboards 4 to 6 in the browser mock.
const presets: Record<string, { p: string; i: string; d: string }> = {
  issue: { p: 'core', i: '1190', d: '' },
  both: { p: 'core', i: '1190', d: 'Start with the API flag only; the app change is a separate task.' },
  local: { p: 'rider', i: '', d: 'Add haptic feedback when a ride starts and ends.' },
}
const pre = presets[herd.previewNew]

const project = ref(pre?.p ?? herd.selTask?.project ?? herd.projects[0]?.id ?? '')
const issue = ref(pre?.i ?? '')
const desc = ref(pre?.d ?? '')
const issueTitle = ref('')
const titleError = ref('')
const busy = ref(false)
const error = ref('')
const issueInput = ref<HTMLInputElement>()
onMounted(() => {
  issueInput.value?.focus()
  if (pre?.i) void fetchTitle()
})

const p = computed(() => herd.project(project.value))
const nIssue = computed(() => issue.value.trim())
const nDesc = computed(() => desc.value.trim())
const mode = computed(() => (nIssue.value && nDesc.value ? 'both' : nIssue.value ? 'issue' : nDesc.value ? 'local' : 'none'))
const slug = computed(() => {
  if (nIssue.value) return nIssue.value
  const words = nDesc.value.toLowerCase().replace(/[^a-z0-9 ]+/g, ' ').split(/\s+/).filter(Boolean).slice(0, 3)
  return 'local-' + (words.join('-') || 'task')
})
const info = computed(
  () =>
    ({
      issue: { label: 'Issue only', text: `Claude reads issue #${nIssue.value} from ${p.value?.repo || 'the repo'} and plans from it.`, cls: 's-running' },
      both: { label: 'Issue + description', text: `Claude reads issue #${nIssue.value} and follows your description on top of it.`, cls: 's-running' },
      local: { label: 'LOCAL', text: 'No GitHub issue. Claude works from your description only; the task is tagged LOCAL.', cls: 's-local' },
      none: { label: 'Missing', text: 'Add a GitHub issue number, a description, or both.', cls: 's-idle' },
    })[mode.value],
)
const first = computed(() => herd.settings?.agents[0]?.name ?? 'Plan')
const valid = computed(() => mode.value !== 'none' && !!p.value && !busy.value)

function onIssue(e: Event) {
  const el = e.target as HTMLInputElement
  issue.value = el.value.replace(/[^0-9]/g, '')
  el.value = issue.value
  issueTitle.value = ''
  titleError.value = ''
}

async function fetchTitle() {
  if (!nIssue.value || !p.value?.repo) return
  const want = nIssue.value
  try {
    const t = await api.IssueTitle(project.value, want)
    if (want === nIssue.value) issueTitle.value = t
  } catch (e) {
    if (want === nIssue.value) titleError.value = errorText(e)
  }
}

function pickProject(id: string) {
  project.value = id
  issueTitle.value = ''
  void fetchTitle()
}

async function create(run: boolean) {
  if (!valid.value) return
  busy.value = true
  error.value = ''
  try {
    const t = await api.CreateTask(project.value, nIssue.value, nDesc.value, run)
    herd.overlay = 'none'
    // The task:changed event may arrive after the call returns.
    if (!herd.task(t.id)) herd.tasks.push(t)
    herd.pickTask(t.id)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

function key(e: KeyboardEvent) {
  const mod = isMac() ? e.metaKey : e.ctrlKey
  if (e.key === 'Enter' && mod) {
    e.preventDefault()
    void create(true)
  }
}
</script>

<template>
  <button class="scrim dim" aria-label="Cancel new task" @click="herd.overlay = 'none'"></button>
  <div class="box popup" role="dialog" aria-label="New task" tabindex="-1" @keydown="key">
    <div class="head">
      <span class="title">New task</span>
    </div>
    <div class="body">
      <div class="group">
        <span class="lbl">Project</span>
        <div class="chips">
          <button v-for="pr in herd.projects" :key="pr.id" class="seg" :class="{ on: pr.id === project }" @click="pickProject(pr.id)">{{ pr.name }}</button>
          <span v-if="!herd.projects.length" class="muted">Add a project in Settings first.</span>
        </div>
      </div>
      <div class="group">
        <label for="nfissue" class="lbl">GitHub issue <span class="faint">optional</span></label>
        <div class="issue-row">
          <div class="issue">
            <span class="muted mono">#</span>
            <input id="nfissue" ref="issueInput" :value="issue" placeholder="1190" inputmode="numeric" autocomplete="off" @input="onIssue" @blur="fetchTitle" />
          </div>
          <span class="repo">{{ p?.repo || 'no GitHub repo' }}</span>
          <span v-if="issueTitle" class="fetched">“{{ issueTitle }}”</span>
          <span v-if="titleError" class="err">{{ titleError }}</span>
        </div>
      </div>
      <div class="group">
        <label for="nfdesc" class="lbl">Description <span class="faint">optional</span></label>
        <textarea id="nfdesc" v-model="desc" rows="4" placeholder="What should Claude do? Added on top of the issue, or used alone for a LOCAL task."></textarea>
      </div>
      <div class="mode">
        <div class="mode-row">
          <span :class="['mode-chip', info.cls]">{{ info.label }}</span>
          <span class="mode-text">{{ info.text }}</span>
        </div>
        <div v-if="mode !== 'none'" class="preview">
          <span v-if="mode === 'local'" class="local-chip">LOCAL</span>
          <span>branch agent/{{ slug }}</span>
          <span>worktree {{ p?.worktreesDir ?? '.worktrees' }}/{{ slug }}</span>
        </div>
      </div>
      <p v-if="error" class="err">{{ error }}</p>
    </div>
    <div class="foot">
      <span class="muted small">Starts with the {{ first }} agent</span>
      <span class="grow"></span>
      <button class="btn ghost" @click="herd.overlay = 'none'">Cancel</button>
      <button class="btn" :disabled="!valid" @click="create(false)">Create</button>
      <button class="btn primary run" :disabled="!valid" @click="create(true)">
        <Icon name="play" :size="10" />Create and run {{ first }}
      </button>
    </div>
  </div>
</template>

<style scoped>
  .box {
    position: absolute;
    z-index: 21;
    top: 64px;
    left: 50%;
    transform: translateX(-50%);
    width: 580px;
    max-width: calc(100% - 32px);
    display: flex;
    flex-direction: column;
    outline: none;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 40px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    font-weight: 600;
  }
  .grow {
    flex: 1;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 14px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: 12px;
    color: var(--muted);
  }
  .faint {
    color: var(--faint);
  }
  .chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .issue-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .issue {
    display: flex;
    align-items: center;
    width: 160px;
    height: 30px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
  }
  .issue input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: none;
    background: transparent;
    color: var(--text);
    font-family: var(--mono);
    font-size: 13px;
  }
  .repo {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--muted);
  }
  .fetched {
    font-size: 12px;
  }
  .err {
    margin: 0;
    font-size: 12px;
    color: var(--approval);
  }
  textarea {
    resize: vertical;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
    color: var(--text);
    font-size: 13px;
    line-height: 1.5;
    outline: none;
  }
  .mode {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
  }
  .mode-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .mode-chip {
    flex: none;
    font-size: 11.5px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 4px;
  }
  .mode-text {
    font-size: 12.5px;
    line-height: 1.5;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--muted);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-top: 1px solid var(--border);
  }
  .small {
    font-size: 12px;
  }
  .btn {
    height: 28px;
  }
  .run {
    gap: 7px;
  }
</style>
