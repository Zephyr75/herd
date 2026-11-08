<script setup lang="ts">
// Settings > Agents: the pipeline and one agent's definition. Edits save to
// ~/.herd/agents.toml and regenerate the agent's /herd: command. The first
// agent starts every task.
import { computed, ref } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import { afterOf, type Agent } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import Dropdown from '../ui/Dropdown.vue'

const agents = computed(() => herd.settings?.agents ?? [])
const firstId = computed(() => agents.value[0]?.id ?? '')
const selId = ref(agents.value[0]?.id ?? '')
const A = computed(() => agents.value.find((a) => a.id === selId.value) ?? agents.value[0])
const confirmDelete = ref(false)

const models: [string, string][] = [['opus', 'Opus'], ['sonnet', 'Sonnet'], ['haiku', 'Haiku']]
const modelLabel = (m: string) => models.find((x) => x[0] === m)?.[1] ?? m
const isAuto = (a: Agent) => a.trigger.startsWith('after:')
const vFocus = { mounted: (el: HTMLElement) => el.focus() }

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

const nameOf = (id: string) => agents.value.find((x) => x.id === id)?.name ?? id
const afterName = (a: Agent) => nameOf(afterOf(a.trigger))
const sub = (a: Agent) => modelLabel(a.model) + ' · ' + (isAuto(a) ? 'automatic after ' + afterName(a) : 'manual')

// "Starts after X" would loop when X (or what X waits for) starts after this agent.
function loops(a: Agent, target: string) {
  for (let cur = target, n = 0; cur && n <= agents.value.length; n++) {
    if (cur === a.id) return true
    cur = afterOf(agents.value.find((x) => x.id === cur)?.trigger ?? '')
  }
  return false
}
// An agent starts by hand, or when another one succeeds.
const triggers = computed(() => {
  const a = A.value
  if (!a) return []
  const out = [{ value: 'manual', label: 'Manually, with play', disabled: false }]
  for (const x of agents.value) {
    if (x.id === a.id) continue
    const loop = loops(a, x.id)
    out.push({ value: 'after:' + x.id, label: 'Automatically when ' + x.name + ' succeeds' + (loop ? ' (would loop)' : ''), disabled: loop })
  }
  return out
})

// A new agent is named first: its id (used by triggers and step names) comes
// from the name and never changes, so nothing that points at it breaks.
const adding = ref(false)
const newName = ref('')
const idFor = (name: string) => {
  const base = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'agent'
  let id = base
  for (let i = 2; agents.value.some((a) => a.id === id); i++) id = `${base}-${i}`
  return id
}
function addAgent() {
  const s = herd.settings
  const name = newName.value.trim()
  if (!s || !name) return
  const id = idFor(name)
  s.agents.push({
    id, name, command: `/herd:${id} {task} -- {guidance}`, model: 'sonnet', trigger: 'manual', writes: id.toUpperCase() + '-{n}.md',
    commit: false, push: false, openPR: false, revert: false, rerun: false, choose: false,
    ignoreRules: false, showDiff: false, sisters: false, allow: [], next: [], onFail: [],
    instructions: 'Describe what this agent reads, what it does and what it writes.',
  })
  selId.value = id
  adding.value = false
  newName.value = ''
  changed()
}
// Any agent moves and goes; the first one starts every task.
const index = computed(() => agents.value.findIndex((a) => a.id === A.value?.id))
function move(delta: number) {
  const s = herd.settings
  const a = A.value
  if (!s || !a) return
  const i = s.agents.findIndex((x) => x.id === a.id)
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
  for (const a of s.agents) {
    if (afterOf(a.trigger) === id) a.trigger = 'manual' // nothing to wait for any more
    a.next = (a.next ?? []).filter((x) => x !== id)
    a.onFail = (a.onFail ?? []).filter((x) => x !== id)
  }
  selId.value = s.agents[0]?.id ?? ''
  confirmDelete.value = false
  changed()
}
// What Next step offers after an agent's step: two ordered lists of agents
// (the first one is the highlighted button). Loops back are fine.
type ListKey = 'next' | 'onFail'
const addable = (k: ListKey) => agents.value.filter((x) => !(A.value?.[k] ?? []).includes(x.id)).map((x) => ({ value: x.id, label: x.name }))
const addTo = (k: ListKey, id: string) => A.value && set(k, [...(A.value[k] ?? []), id])
const dropFrom = (k: ListKey, id: string) => A.value && set(k, (A.value[k] ?? []).filter((x) => x !== id))
// The git boxes run in order, each needing the one before: ticking one
// ticks those before it, unticking one unticks those after it.
type GitKey = 'commit' | 'push' | 'openPR'
const gitOrder: GitKey[] = ['commit', 'push', 'openPR']
function setGit(k: GitKey, on: boolean) {
  const a = A.value
  if (!a) return
  const i = gitOrder.indexOf(k)
  gitOrder.forEach((g, j) => {
    if (on && j <= i) a[g] = true
    if (!on && j >= i) a[g] = false
  })
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
    <p>Each step of a task runs one agent: a Claude Code session started with the agent's command, model and instructions, in Claude Code's auto mode. The first agent starts every task; the order below is the pipeline.</p>
  </div>

  <div class="pipeline">
    <template v-for="(a, i) in agents" :key="a.id">
      <span class="pp" :class="{ auto: isAuto(a) }"><Icon v-if="a.id === firstId" name="play" :size="10" />{{ a.name }}{{ isAuto(a) ? ' (auto)' : '' }}</span>
      <span v-if="i < agents.length - 1" class="muted" aria-hidden="true">→</span>
    </template>
  </div>

  <div class="split">
    <div class="list">
      <button v-for="a in agents" :key="a.id" class="list-btn" :class="{ on: a.id === A?.id }" @click="pick(a.id)">
        <span class="name">{{ a.name }}<span v-if="a.id === firstId" class="badge" title="The first agent starts every task and every sister task"><Icon name="play" :size="9" />first</span></span>
        <span class="sub">{{ sub(a) }}</span>
      </button>
      <input v-if="adding" v-model="newName" v-focus class="input new-name" placeholder="Name, then Enter" @keydown.enter="addAgent" @blur="adding = false" />
      <button v-else class="add" @click="adding = true">+ Add agent</button>
      <span v-if="adding && newName.trim()" class="muted small new-id">id <span class="mono">{{ idFor(newName) }}</span></span>
    </div>

    <div v-if="A" class="editor card">
      <span class="muted small">ID <span class="mono">{{ A.id }}</span>: made from the name when the agent was created, used by triggers and step names</span>
      <p v-if="A.id === firstId" class="first"><Icon name="play" :size="12" />First in the list: it starts every task and every sister task. Move another agent above it to change that.</p>
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
        <Dropdown class="starts" :items="triggers" :value="A.trigger" @pick="set('trigger', $event)" />
      </div>
      <div class="field">
        <span>When its step succeeds, herd</span>
        <div class="checks">
          <label title="Its artifact's title line and body are the commit message"><input type="checkbox" :checked="A.commit" @change="setGit('commit', checked($event))" /> commits</label>
          <span class="muted" aria-hidden="true">→</span>
          <label title="Pushes the branch to origin (needs the commit)"><input type="checkbox" :checked="A.push" @change="setGit('push', checked($event))" /> pushes</label>
          <span class="muted" aria-hidden="true">→</span>
          <label title="Opens the draft PR, or updates it, with its artifact's title line and body (needs the push)"><input type="checkbox" :checked="A.openPR" @change="setGit('openPR', checked($event))" /> opens the PR</label>
        </div>
        <span class="hint">In that order, stopping at the first that fails; Next step then says why and offers to run it again.</span>
      </div>
      <div class="field">
        <span>Its steps</span>
        <label class="box"><input type="checkbox" :checked="A.revert" @change="set('revert', checked($event))" /> Can be reverted</label>
        <span class="hint">Revert shows on its latest step once that step and every step after it finished, while no step that commits ran after it and nothing was committed since it started. It puts the worktree back to how it was when the step started, and removes that step and the ones after it.</span>
        <label class="box"><input type="checkbox" :checked="A.rerun" @change="set('rerun', checked($event))" /> Can be rerun</label>
        <span class="hint">Rerun shows on its step while it is the task's latest step and every step has finished. It runs the step again in its own conversation, on the worktree as it is now (e.g. after hand edits); if it commits, its commit is amended.</span>
        <label class="box"><input type="checkbox" :checked="A.choose" @change="set('choose', checked($event))" /> Asks you to choose</label>
        <span class="hint">When its artifact has an Options section, Next step asks you to name the option to take in the guidance.</span>
      </div>
      <div class="checks">
        <label title="The Rules in Settings > Context are not given to this agent"><input type="checkbox" :checked="A.ignoreRules" @change="set('ignoreRules', checked($event))" /> Ignore the common rules</label>
        <label title="A Diff button on its steps opens their changes in Zed"><input type="checkbox" :checked="A.showDiff" @change="set('showDiff', checked($event))" /> Show a Diff button</label>
        <label title="When the work also needs other repositories, its artifact ends with a Sister tasks section and Next step offers to create each one in a click"><input type="checkbox" :checked="A.sisters" @change="set('sisters', checked($event))" /> Can create sister tasks</label>
      </div>
      <div v-for="[k, label] in ([['next', 'After its step succeeds, Next step offers'], ['onFail', 'After its step fails, Next step offers']] as [ListKey, string][])" :key="k" class="field">
        <span>{{ label }}</span>
        <div class="offers">
          <span v-for="(id, i) in A[k] ?? []" :key="id" class="offer" :class="{ main: i === 0 }" :title="i === 0 ? 'The highlighted button' : ''">{{ nameOf(id) }}<button class="x" :aria-label="'Remove ' + nameOf(id)" @click="dropFrom(k, id)">×</button></span>
          <Dropdown v-if="addable(k).length" :items="addable(k)" @pick="addTo(k, $event)"><Icon name="plus" :size="11" />Add</Dropdown>
          <span v-if="k === 'onFail' && !(A[k] ?? []).length" class="muted small">{{ A.name }} again</span>
        </div>
      </div>
      <label class="field"
        >Runs without asking (one Claude Code permission rule per line)
        <textarea class="area mono" rows="2" placeholder="Bash(make test-path:*)" :value="(A.allow ?? []).join('\n')" @input="set('allow', val($event).split('\n'))"></textarea>
        <span class="hint flat">Tools and commands this agent may use without a permission prompt. Your rules still apply: only list what they allow.</span>
      </label>
      <label class="field">Instructions (the agent's skill: what it reads, does and writes)<textarea class="area mono" rows="7" :value="A.instructions" @input="set('instructions', val($event))"></textarea></label>
      <div class="actions">
        <span class="muted small">Placeholders: {{ '{task} {guidance} {n} {sha} {issue} {base}' }}</span>
        <span class="grow"></span>
        <button class="btn ghost" :disabled="index <= 0" @click="move(-1)">Move up</button>
        <button class="btn ghost" :disabled="index === agents.length - 1" @click="move(1)">Move down</button>
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
  .segs {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
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
  .starts {
    height: 30px;
    max-width: 340px;
  }
  .new-name {
    margin-top: 6px;
    height: 28px;
  }
  .new-id {
    padding: 2px 4px;
  }
  .pp {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-left: 6px;
    padding: 0 5px;
    border-radius: 3px;
    border: 1px solid var(--border);
    color: var(--muted);
    font-size: 10.5px;
    font-weight: 400;
    vertical-align: 1px;
  }
  .first {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: 12.5px;
    color: var(--muted);
  }
  .box {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
  }
  .hint {
    margin: -2px 0 4px 22px;
    font-size: 12px;
    line-height: 1.45;
    color: var(--muted);
  }
  .hint.flat {
    margin-left: 0;
  }
  .field > .hint:last-child {
    margin-bottom: 0;
  }
  .special {
    margin: 0;
    padding: 8px 10px;
    border-radius: 6px;
    border: 1px solid var(--waiting-dot);
    font-size: 12.5px;
    line-height: 1.5;
  }
  .offers {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .offer {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 24px;
    padding: 0 2px 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    font-size: 12.5px;
  }
  .offer.main {
    border-color: var(--accent);
  }
  .offer .x {
    border: 0;
    background: none;
    color: var(--muted);
    padding: 0 4px;
  }
</style>
