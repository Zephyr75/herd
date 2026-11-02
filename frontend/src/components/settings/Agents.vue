<script setup lang="ts">
// Settings > Agents: the pipeline and one agent's definition. Edits save to
// ~/.herd/agents.toml and regenerate the agent's /herd: command.
import { computed, ref } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import { afterOf, type Agent } from '../../lib/types'

const agents = computed(() => herd.settings?.agents ?? [])
const selId = ref(herd.settings?.agents[1]?.id ?? herd.settings?.agents[0]?.id ?? '')
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

const afterName = (a: Agent) => {
  const id = afterOf(a.trigger)
  return agents.value.find((x) => x.id === id)?.name ?? id
}
// "Starts after X" would loop when X (or what X waits for) starts after this agent.
function loops(a: Agent, target: string) {
  for (let cur = target, n = 0; cur && n <= agents.value.length; n++) {
    if (cur === a.id) return true
    cur = afterOf(agents.value.find((x) => x.id === cur)?.trigger ?? '')
  }
  return false
}
const triggers = computed(() => {
  const a = A.value
  const out = [{ value: 'manual', label: 'Manually, with play', loop: false }]
  for (const x of agents.value) {
    if (!a || x.id === a.id) continue
    out.push({ value: 'after:' + x.id, label: 'Automatically when ' + x.name + ' succeeds', loop: loops(a, x.id) })
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
    id, name, command: `/herd:${id} {task} -- {guidance}`, model: 'sonnet', trigger: 'manual', writes: id.toUpperCase() + '.md',
    commit: false, push: false, ignoreRules: false,
    instructions: 'Describe what this agent does and what it writes.',
  })
  selId.value = id
  adding.value = false
  newName.value = ''
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
  for (const a of s.agents) if (afterOf(a.trigger) === id) a.trigger = 'manual' // nothing to wait for any more
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
    <p>Each step of a task runs one agent: a Claude Code session started with the agent's command, model and instructions, in Claude Code's auto mode. The order below is the pipeline.</p>
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
      <input
        v-if="adding"
        v-model="newName"
        v-focus
        class="input new-name"
        placeholder="Name, then Enter"
        @keydown.enter="addAgent"
        @blur="adding = false"
      />
      <button v-else class="add" @click="adding = true">+ Add agent</button>
      <span v-if="adding && newName.trim()" class="muted small new-id">id <span class="mono">{{ idFor(newName) }}</span></span>
    </div>

    <div v-if="A" class="editor card">
      <span class="muted small">ID <span class="mono">{{ A.id }}</span>: made from the name when the agent was created, used by triggers and step names</span>
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
        <select class="input starts" :value="A.trigger" @change="set('trigger', val($event))">
          <option v-for="o in triggers" :key="o.value" :value="o.value" :disabled="o.loop">{{ o.label }}{{ o.loop ? ' (would loop)' : '' }}</option>
        </select>
      </div>
      <div class="checks">
        <label><input type="checkbox" :checked="A.commit" @change="set('commit', checked($event))" /> Commit the worktree when it finishes</label>
        <label><input type="checkbox" :checked="A.push" @change="set('push', checked($event))" /> Push and open the PR when it succeeds</label>
        <label title="The Rules in Settings > Context are not given to this agent"><input type="checkbox" :checked="A.ignoreRules" @change="set('ignoreRules', checked($event))" /> Ignore the common rules</label>
      </div>
      <label class="field">Instructions (the agent's skill)<textarea class="area mono" rows="7" :value="A.instructions" @input="set('instructions', val($event))"></textarea></label>
      <div class="actions">
        <span class="muted small">Placeholders: {{ '{task} {guidance} {n} {sha} {issue}' }}</span>
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
</style>
