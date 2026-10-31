<script setup lang="ts">
// Settings > Agents: the pipeline and one agent's definition. Edits save to
// ~/.herd/agents.toml and regenerate the agent's /herd: command.
import { computed, ref } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import type { Agent } from '../../lib/types'

const agents = computed(() => herd.settings?.agents ?? [])
const selId = ref(herd.settings?.agents[1]?.id ?? herd.settings?.agents[0]?.id ?? '')
const A = computed(() => agents.value.find((a) => a.id === selId.value) ?? agents.value[0])
const newAllowed = ref('')
const newDenied = ref('')
const confirmDelete = ref(false)

const models: [string, string][] = [['opus', 'Opus'], ['sonnet', 'Sonnet'], ['haiku', 'Haiku']]
const perms: [string, string][] = [['manual', 'Ask every time'], ['acceptEdits', 'Accept edits'], ['readonly', 'Read only']]
const modelLabel = (m: string) => models.find((x) => x[0] === m)?.[1] ?? m
const isAuto = (a: Agent) => a.trigger.startsWith('after:')

const save = debounce(() => {
  const s = herd.settings
  if (s) void herd.attempt('Could not save agents.toml', () => api.SaveAgents(JSON.parse(JSON.stringify(s.agents))))
}, 500)
function changed() {
  herd.edited()
  save()
}

function set<K extends keyof Agent>(k: K, v: Agent[K]) {
  if (!A.value) return
  A.value[k] = v
  changed()
}
const val = (e: Event) => (e.target as HTMLInputElement).value
const checked = (e: Event) => (e.target as HTMLInputElement).checked

const prevAgent = computed(() => {
  const i = agents.value.findIndex((a) => a.id === A.value?.id)
  return i > 0 ? agents.value[i - 1] : null
})
const afterName = (a: Agent) => agents.value.find((x) => 'after:' + x.id === a.trigger)?.name ?? a.trigger.slice(6)
const triggers = computed<[string, string][]>(() => {
  const out: [string, string][] = [['manual', 'Manually, with play']]
  const prev = prevAgent.value
  if (prev) out.push(['after:' + prev.id, 'Automatically after ' + prev.name])
  const a = A.value
  if (a && isAuto(a) && a.trigger !== 'after:' + prev?.id) out.push([a.trigger, 'Automatically after ' + afterName(a)])
  return out
})

function addTool(list: 'allowed' | 'denied') {
  const box = list === 'allowed' ? newAllowed : newDenied
  const v = box.value.trim()
  if (!v || !A.value) return
  set(list, [...(A.value[list] ?? []), v])
  box.value = ''
}
function removeTool(list: 'allowed' | 'denied', i: number) {
  if (A.value) set(list, A.value[list].filter((_, j) => j !== i))
}

function addAgent() {
  const s = herd.settings
  if (!s) return
  let n = s.agents.length + 1
  while (s.agents.some((a) => a.id === 'agent' + n)) n++
  const id = 'agent' + n
  s.agents.push({
    id, name: 'Agent ' + n, command: `/herd:${id} {task} -- {guidance}`, model: 'sonnet', trigger: 'manual', writes: id.toUpperCase() + '.md',
    preview: '', done: 'status: done', permissions: 'readonly', allowed: ['Read', 'Grep', 'Glob', 'Edit(/{run}/**)'], denied: [], commit: false, push: false,
    instructions: 'Describe what this agent does and what it writes.', rules: '',
  })
  selId.value = id
  changed()
}
function move(delta: number) {
  const s = herd.settings
  if (!s || !A.value) return
  const i = s.agents.findIndex((a) => a.id === A.value!.id)
  const j = i + delta
  if (j < 0 || j >= s.agents.length) return
  ;[s.agents[i], s.agents[j]] = [s.agents[j], s.agents[i]]
  changed()
}
function remove() {
  const s = herd.settings
  if (!s || !A.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    return
  }
  const id = A.value.id
  s.agents = s.agents.filter((a) => a.id !== id)
  selId.value = s.agents[0]?.id ?? ''
  confirmDelete.value = false
  changed()
}
function pick(id: string) {
  selId.value = id
  confirmDelete.value = false
}
</script>

<template>
  <div class="intro">
    <h1>Agents</h1>
    <p>Each step of a task runs one agent: a Claude Code session started with the agent's command, model, permissions and instructions. The order below is the pipeline.</p>
  </div>

  <div class="pipeline">
    <template v-for="(a, i) in agents" :key="a.id">
      <span class="pp" :class="{ auto: isAuto(a) }">{{ a.name }}{{ isAuto(a) ? ' (auto)' : '' }}</span>
      <span v-if="i < agents.length - 1" class="muted" aria-hidden="true">→</span>
    </template>
    <span class="grow"></span>
    <span class="muted small">Review can loop back to Implement</span>
  </div>

  <div class="split">
    <div class="list">
      <button v-for="a in agents" :key="a.id" class="list-btn" :class="{ on: a.id === A?.id }" @click="pick(a.id)">
        <span class="name">{{ a.name }}</span>
        <span class="sub">{{ modelLabel(a.model) }} · {{ isAuto(a) ? 'automatic after ' + afterName(a) : 'manual' }}</span>
      </button>
      <button class="add" @click="addAgent">+ Add agent</button>
    </div>

    <div v-if="A" class="editor card">
      <div class="grid2">
        <label class="field">Name<input class="input" :value="A.name" @input="set('name', val($event))" /></label>
        <label class="field">Writes artifact<input class="input mono" :value="A.writes" @input="set('writes', val($event))" /></label>
      </div>
      <label class="field">Command sent to Claude Code<input class="input mono" :value="A.command" @input="set('command', val($event))" /></label>
      <div class="field">
        <span>Model</span>
        <div class="segs">
          <button v-for="[v, l] in models" :key="v" class="seg" :class="{ on: A.model === v }" @click="set('model', v)">{{ l }}</button>
        </div>
      </div>
      <div class="field">
        <span>Starts</span>
        <div class="segs">
          <button v-for="[v, l] in triggers" :key="v" class="seg" :class="{ on: A.trigger === v }" @click="set('trigger', v)">{{ l }}</button>
        </div>
      </div>
      <div class="field">
        <span>Permissions</span>
        <div class="segs">
          <button v-for="[v, l] in perms" :key="v" class="seg" :class="{ on: A.permissions === v }" @click="set('permissions', v)">{{ l }}</button>
        </div>
      </div>
      <div class="grid2">
        <div class="field">
          <span>Allowed tools</span>
          <div class="chips">
            <span v-for="(x, i) in A.allowed ?? []" :key="x + i" class="tool-chip">{{ x }}<button class="rm" :aria-label="'Remove ' + x" @click="removeTool('allowed', i)">×</button></span>
          </div>
          <input v-model="newAllowed" class="input mono small-in" placeholder="Add, e.g. Bash(go test:*)" @keydown.enter="addTool('allowed')" />
        </div>
        <div class="field">
          <span>Denied tools</span>
          <div class="chips">
            <span v-for="(x, i) in A.denied ?? []" :key="x + i" class="tool-chip denied">{{ x }}<button class="rm" :aria-label="'Remove ' + x" @click="removeTool('denied', i)">×</button></span>
          </div>
          <input v-model="newDenied" class="input mono small-in" placeholder="Add, e.g. Bash(git push:*)" @keydown.enter="addTool('denied')" />
        </div>
      </div>
      <label class="field">Done when<input class="input mono" :value="A.done" @input="set('done', val($event))" /></label>
      <div class="checks">
        <label><input type="checkbox" :checked="A.commit" @change="set('commit', checked($event))" /> Commit the worktree when done</label>
        <label><input type="checkbox" :checked="A.push" @change="set('push', checked($event))" /> Push and open the PR when done</label>
      </div>
      <label class="field">Instructions (the agent's skill)<textarea class="area mono" rows="7" :value="A.instructions" @input="set('instructions', val($event))"></textarea></label>
      <label class="field">Rules (always followed, added to the system prompt)<textarea class="area mono" rows="4" :value="A.rules ?? ''" placeholder="e.g. Never touch migrations/&#10;Run go vet before finishing" @input="set('rules', val($event))"></textarea></label>
      <div class="actions">
        <span class="muted small">Placeholders: {{ '{task} {guidance} {n} {sha} {issue}' }}; in tools {{ '{run} {worktree}' }}</span>
        <span class="grow"></span>
        <button class="btn ghost" @click="move(-1)">Move up</button>
        <button class="btn ghost" @click="move(1)">Move down</button>
        <button class="btn" :class="confirmDelete ? 'danger' : 'ghost'" @click="remove">{{ confirmDelete ? 'Remove ' + A.name + '?' : 'Remove' }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .pipeline {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
  }
  .pp {
    padding: 3px 9px;
    border-radius: 4px;
    font-size: 12.5px;
    border: 1px solid var(--border);
    background: var(--bg);
  }
  .pp.auto {
    background: var(--sel);
  }
  .grow {
    flex: 1;
  }
  .small {
    font-size: 12px;
  }
  .split {
    display: flex;
    gap: 18px;
    flex-wrap: wrap;
    align-items: flex-start;
  }
  .list {
    flex: 1 1 200px;
    max-width: 230px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name {
    font-weight: 500;
  }
  .add {
    margin-top: 6px;
    height: 28px;
    border-radius: 6px;
    border: 1px dashed var(--border);
    background: transparent;
    color: var(--muted);
  }
  .editor {
    flex: 999 1 420px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 16px 18px;
  }
  .grid2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  .segs,
  .chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .tool-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .rm {
    border: 0;
    background: transparent;
    color: inherit;
    opacity: 0.6;
    padding: 0 0 0 2px;
    font-size: 13px;
    line-height: 1;
  }
  .rm:hover {
    opacity: 1;
  }
  .small-in {
    height: 26px;
    font-size: 12px;
  }
  .checks {
    display: flex;
    gap: 18px;
    flex-wrap: wrap;
    font-size: 13px;
  }
  .checks label {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
</style>
