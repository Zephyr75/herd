<script setup lang="ts">
// New task (⌘N): a project, then an issue, a description, or both.
import { computed, onMounted, ref, watch } from 'vue'
import { api, errorText } from '../../lib/api'
import { herd } from '../../lib/state'
import { isMac } from '../../lib/keymap'
import Icon from '../ui/Icon.vue'
import Dropdown from '../ui/Dropdown.vue'
import type { Issue } from '../../lib/types'

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
const files = ref<string[]>([])
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
// The branch: the prefix from Settings, then words from the issue title or
// the description, until the user edits it.
const prefix = computed(() => herd.settings?.config.branchPrefix ?? 'ar-')
const words = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim().split(/\s+/).filter(Boolean).slice(0, 7).join('-')
const branchEdited = ref(false)
const branchInput = ref('')
const suggested = computed(() => prefix.value + (words(issueTitle.value) || words(nDesc.value) || nIssue.value || 'task'))
const branch = computed({
  get: () => (branchEdited.value ? branchInput.value : suggested.value),
  set: (v: string) => {
    branchEdited.value = true
    branchInput.value = v.replace(/\s+/g, '-')
  },
})
const branchOk = computed(() => branch.value.startsWith(prefix.value) && branch.value.length > prefix.value.length && /^[A-Za-z0-9._/-]+$/.test(branch.value))

const info = computed(
  () =>
    ({
      issue: { label: 'Issue only', text: `Claude reads issue #${nIssue.value} from ${p.value?.repo || 'the repo'} and plans from it.`, cls: 's-running' },
      both: { label: 'Issue + description', text: `Claude reads issue #${nIssue.value} and follows your description on top of it.`, cls: 's-running' },
      local: { label: 'LOCAL', text: 'No GitHub issue. Claude works from your description only; the task is tagged LOCAL.', cls: 's-local' },
      none: { label: 'Missing', text: 'Add a GitHub issue number, a description, or both.', cls: 's-idle' },
    })[mode.value],
)
const first = computed(() => herd.settings?.agents.find((a) => !a.builtin)?.name ?? 'Plan')
const valid = computed(() => mode.value !== 'none' && !!p.value && !busy.value && branchOk.value)

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

// Open issues assigned to you; the # field takes any other issue.
const mine = ref<Issue[] | null>(null)
const mineError = ref('')
let mseq = 0
watch(
  project,
  async (id) => {
    const want = ++mseq
    mine.value = null
    mineError.value = ''
    if (!herd.project(id)?.repo) return
    try {
      const is = await api.MyIssues(id)
      if (want === mseq) mine.value = is
    } catch (e) {
      if (want === mseq) (mineError.value = errorText(e)), (mine.value = [])
    }
  },
  { immediate: true },
)
const hasTask = (n: number) => herd.tasks.some((t) => t.project === project.value && t.issue === String(n))
const mineItems = computed(() =>
  (mine.value ?? []).map((is) => ({
    value: String(is.number),
    label: `${is.priority ? is.priority + ' · ' : ''}#${is.number} ${is.title}${hasTask(is.number) ? ' (has a task)' : ''}`,
    disabled: hasTask(is.number),
  })),
)
const minePlaceholder = computed(() =>
  mine.value === null ? 'Loading the issues assigned to you…' : mineError.value ? 'Could not list your issues: ' + mineError.value : mine.value.length ? `Assigned to you (${mine.value.length} open, most urgent first)` : 'No open issues assigned to you',
)
function pickMine(v: string) {
  const is = mine.value?.find((x) => String(x.number) === v)
  if (!is) return
  issue.value = String(is.number)
  issueTitle.value = is.title
  titleError.value = ''
}

// Files attached to the task: herd copies them to the run folder and TASK.md lists them.
const baseName = (f: string) => f.split(/[\\/]/).pop() ?? f
async function addFiles() {
  const picked = await herd.attempt('Could not open the file picker', () => api.ChooseFiles('Attach files to the task'))
  for (const f of picked ?? []) if (!files.value.includes(f)) files.value.push(f)
}

// Adds a project without leaving the pop-up, then selects it.
async function addProject() {
  const pr = await herd.attempt('Could not add the project', () => api.AddProject())
  if (!pr) return
  herd.toast('Added ' + pr.name, pr.path)
  pickProject(pr.id)
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
    const t = await api.CreateTask(project.value, nIssue.value, nDesc.value, branch.value, run, files.value)
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
          <button class="seg add" title="Pick a git repository to add as a project" @click="addProject"><Icon name="plus" :size="12" />Add project</button>
        </div>
      </div>
      <div class="group">
        <span class="lbl">GitHub issue <span class="faint">optional</span></span>
        <Dropdown v-if="p?.repo" class="mine" wide :items="mineItems" :value="nIssue" :placeholder="minePlaceholder" :disabled="!mine?.length" @pick="pickMine" />
        <div class="issue-row">
          <span v-if="p?.repo" class="muted small">or any issue</span>
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
      <div class="group">
        <span class="lbl">Files <span class="faint">optional: screenshots, logs, specs… Claude reads them with the task</span></span>
        <div class="chips">
          <span v-for="f in files" :key="f" class="file" :title="f">
            {{ baseName(f) }}
            <button class="x" :aria-label="`Remove ${baseName(f)}`" @click="files = files.filter((x) => x !== f)"><Icon name="close" :size="11" /></button>
          </span>
          <button class="seg add" @click="addFiles"><Icon name="plus" :size="12" />Add files</button>
        </div>
      </div>
      <div class="group">
        <label for="nfbranch" class="lbl">Branch <span class="faint">starts with {{ prefix }}</span></label>
        <input id="nfbranch" v-model="branch" class="input mono branch" autocomplete="off" spellcheck="false" />
        <span v-if="mode !== 'none' && !branchOk" class="err">Start with {{ prefix }}, then name the task: letters, digits, . _ / -</span>
      </div>
      <div class="mode">
        <div class="mode-row">
          <span :class="['mode-chip', info.cls]">{{ info.label }}</span>
          <span class="mode-text">{{ info.text }}</span>
        </div>
        <div v-if="mode !== 'none'" class="preview">
          <span v-if="mode === 'local'" class="local-chip">LOCAL</span>
          <span>branch {{ branch }}</span>
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
        <Icon name="play" :size="13" />Create and run {{ first }}
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
  .add {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--muted);
  }
  .file {
    display: flex;
    align-items: center;
    gap: 4px;
    height: 26px;
    padding: 0 4px 0 9px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--surface);
    font-size: 12px;
  }
  .x {
    display: flex;
    padding: 3px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--muted);
  }
  .x:hover {
    background: var(--hover);
    color: var(--text);
  }
  .mine {
    margin-bottom: 8px;
  }
  .mine :deep(.chip) {
    height: 30px;
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
  .branch {
    height: 30px;
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
