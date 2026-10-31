<script setup lang="ts">
// Settings > Projects: one card per repository, with prerequisite checks.
import { computed, onMounted, ref } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import Icon from '../ui/Icon.vue'
import type { Check, Project } from '../../lib/types'

const checks = ref<Check[]>([])
const checking = ref(false)
const confirmRemove = ref('')
const manualPath = ref('')

async function runChecks() {
  checking.value = true
  checks.value = (await herd.attempt('Checks failed', () => api.CheckPrerequisites())) ?? []
  checking.value = false
}
onMounted(runChecks)

const global = computed(() => checks.value.filter((c) => !c.project))
const failing = (id: string) => checks.value.filter((c) => c.project === id && !c.ok)
const open = (id: string) => herd.tasks.filter((t) => t.project === id && !t.archived).length

const save = debounce((p: Project) => {
  void herd.attempt('Could not save the project', () => api.SaveProject(JSON.parse(JSON.stringify(p))))
}, 600)
function set(p: Project, k: keyof Project, e: Event) {
  let v = (e.target as HTMLInputElement).value
  if (k === 'worktreesDir') v = v.replace(/\/+$/, '')
  p[k] = v
  herd.edited()
  save(p)
}

async function add() {
  const p = await herd.attempt('Could not add the project', () => api.AddProject())
  if (p) {
    herd.toast('Added ' + p.name, p.path)
    void runChecks()
  }
}
async function addPath() {
  const dir = manualPath.value.trim()
  if (!dir) return
  const p = await herd.attempt('Could not add the project', () => api.AddProjectPath(dir))
  if (p) {
    manualPath.value = ''
    herd.toast('Added ' + p.name, p.path)
    void runChecks()
  }
}
async function remove(p: Project) {
  if (confirmRemove.value !== p.id) {
    confirmRemove.value = p.id
    return
  }
  confirmRemove.value = ''
  await herd.attempt('Could not remove the project', () => api.RemoveProject(p.id))
}
const openInZed = (id: string) => herd.attempt('Could not open Zed', () => api.OpenProjectInZed(id))
</script>

<template>
  <div class="top">
    <div class="intro grow">
      <h1>Projects</h1>
      <p>Repositories herd can run tasks in. Tasks from every project run in parallel, each in its own worktree.</p>
    </div>
    <button class="btn primary" @click="add">+ Add project</button>
  </div>

  <div class="checks card">
    <div v-for="c in global" :key="c.label" class="check" :class="{ bad: !c.ok }">
      <span class="ico"><Icon :name="c.ok ? 'check' : 'warn'" /></span>
      <span class="label">{{ c.label }}</span>
      <span class="detail">{{ c.detail }}</span>
    </div>
    <div class="check">
      <span class="grow muted small">{{ checking ? 'Checking…' : 'zed, gh and each repository are checked when this page opens.' }}</span>
      <button class="btn ghost" @click="runChecks">Check again</button>
    </div>
  </div>

  <div v-for="p in herd.projects" :key="p.id" class="proj card">
    <div class="row">
      <Icon name="folder" :size="15" />
      <span class="name">{{ p.name }}</span>
      <span class="path">{{ p.path }}</span>
      <span class="grow"></span>
      <span class="muted small">{{ open(p.id) }} open task{{ open(p.id) === 1 ? '' : 's' }}</span>
      <button class="btn ghost" @click="openInZed(p.id)">Open in Zed</button>
      <button class="btn" :class="confirmRemove === p.id ? 'danger' : 'ghost'" @click="remove(p)">{{ confirmRemove === p.id ? 'Remove from herd?' : 'Remove' }}</button>
    </div>
    <div class="grid4">
      <label class="field">GitHub repo<input class="input mono" :value="p.repo" placeholder="owner/name" @input="set(p, 'repo', $event)" /></label>
      <label class="field">Base branch<input class="input mono" :value="p.baseBranch" @input="set(p, 'baseBranch', $event)" /></label>
      <label class="field">Worktrees folder<input class="input mono" :value="p.worktreesDir + '/'" @input="set(p, 'worktreesDir', $event)" /></label>
      <label class="field">Port range<input class="input mono" :value="p.portRange" placeholder="7100-7199" @input="set(p, 'portRange', $event)" /></label>
    </div>
    <div v-for="c in failing(p.id)" :key="c.label" class="check bad">
      <span class="ico"><Icon name="warn" /></span><span class="label">{{ c.label }}</span><span class="detail">{{ c.detail }}</span>
    </div>
  </div>

  <div class="manual">
    <label class="field grow">Or add by path<input v-model="manualPath" class="input mono" placeholder="~/code/my-repo" @keydown.enter="addPath" /></label>
    <button class="btn" @click="addPath">Add</button>
  </div>
</template>

<style scoped>
  .top {
    display: flex;
    align-items: flex-end;
    gap: 12px;
    flex-wrap: wrap;
  }
  .grow {
    flex: 1;
  }
  .btn {
    height: 28px;
  }
  .small {
    font-size: 12px;
  }
  .checks {
    display: flex;
    flex-direction: column;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    font-size: 12.5px;
  }
  .check + .check {
    border-top: 1px solid var(--border);
  }
  .check .ico {
    display: flex;
    color: var(--done);
  }
  .check.bad .ico,
  .check.bad .label {
    color: var(--approval);
  }
  .check .detail {
    color: var(--muted);
    font-family: var(--mono);
    font-size: 12px;
  }
  .proj {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px 16px;
  }
  .proj .check {
    padding: 0;
    border: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .name {
    font-weight: 600;
  }
  .path {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--muted);
  }
  .grid4 {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .manual {
    display: flex;
    align-items: flex-end;
    gap: 8px;
    max-width: 560px;
  }
</style>
