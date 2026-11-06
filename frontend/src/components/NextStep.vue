<script setup lang="ts">
import { computed } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'

const t = computed(() => herd.selTask)
const nx = computed(() => t.value?.next)

async function restore() {
  const task = t.value
  if (task) await herd.attempt('Could not restore', () => api.RestoreTask(task.id))
}
// An agent another one follows (Implement) can also be created manually: no
// Claude runs, its session is for the user to drive outside herd.
const manualOk = (agent: string) => !!herd.settings?.agents.some((a) => a.trigger === 'after:' + agent)
// Deleting is for good: herd forgets the task and removes its worktree, local
// branch and run files. Two clicks.
async function deleteTask() {
  const task = t.value
  if (!task) return
  if (herd.confirmDelete !== task.id) {
    herd.confirmDelete = task.id
    return
  }
  herd.confirmDelete = ''
  const lines = await herd.attempt('Could not delete the task', () => api.DeleteTask(task.id))
  if (lines) herd.toast('Deleted ' + (task.issue ? '#' + task.issue : task.title), lines.join('\n'))
}
// One click per repository the plan also needs: its sister task, or first its project.
async function createSister(repo: string) {
  const task = t.value
  if (!task) return
  const s = await herd.attempt('Could not create the sister task', () => api.CreateSister(task.id, repo))
  if (s) herd.toast('Sister task created', `${repo}: ${s.title}`)
}
const addProject = () => herd.attempt('Could not add the project', () => api.AddProject())
const allow = (id: string) => herd.attempt('Allow once failed', () => api.AllowOnce(id))
const markDone = (id: string) => herd.attempt('Mark done failed', () => api.MarkDone(id))
</script>

<template>
  <div v-if="t && nx" class="box">
    <div class="top">
      <span class="muted small">Next step</span>
      <span class="grow"></span>
      <span v-if="!t.issue" class="local-chip">LOCAL</span>
      <span class="ref">{{ t.issue ? `#${t.issue} · ` : '' }}{{ t.projectName }}</span>
    </div>

    <div v-for="o in nx.sisters" :key="o.repo" class="sister">
      <p class="scope" :title="o.scope"><b>The plan also needs {{ o.repo }}</b>: {{ o.scope }}</p>
      <button v-if="o.project" class="btn primary" title="Create a task in that repository for the same issue, with its own worktree, plan and PR, linked to this one" @click="createSister(o.repo)"><Icon name="plus" :size="13" />Create sister task in {{ o.repo }}</button>
      <button v-else class="btn" :title="`${o.repo} is not a herd project yet: pick its folder, then create the sister task`" @click="addProject"><Icon name="plus" :size="13" />Add {{ o.repo }} as a project</button>
    </div>

    <template v-if="nx.options.length && !t.archived">
      <label for="guide" class="sr-only">Guidance for Claude</label>
      <p v-if="nx.warning" class="warn"><Icon name="warn" :size="14" />{{ nx.warning }}</p>
      <p v-if="nx.elsewhere.length" class="warn">
        <Icon name="warn" :size="14" />Agents also edited files outside the worktree, in {{ nx.elsewhere.join(', ') }}. Commit and push only covers {{ t.branch }}: check those edits yourself.
      </p>
      <textarea id="guide" v-model="herd.guidance" rows="3" :class="{ need: nx.warning && !herd.guidance.trim() }" :placeholder="nx.warning ? 'Which option? e.g. take option B, because…' : 'Guidance for Claude (optional)'"></textarea>
      <div class="opts">
        <template v-for="(o, i) in nx.options" :key="o.agent + o.n">
          <button class="btn" :class="{ primary: i === 0 }" @click="herd.run(o)"><Icon name="play" :size="13" />{{ o.label }}</button>
          <button
            v-if="manualOk(o.agent)"
            class="btn"
            :title="`Create ${o.label} without running Claude, and copy the command to use its session outside herd`"
            @click="herd.runManual(o)"
          >
            <Icon name="terminal" :size="13" />{{ o.label }} manually
          </button>
        </template>
      </div>
    </template>

    <p v-if="nx.text">{{ nx.text }}</p>
    <button v-if="herd.prReady(t) === 'draft' && herd.copilotClean(t)" class="btn" title="Turn the draft PR into one ready for review and add the ready label" @click="herd.markReady(t)"><Icon name="check" :size="13" />Mark PR #{{ t.pr }} ready for review</button>
    <p v-else-if="herd.prReady(t) === 'ready'" class="muted small">PR #{{ t.pr }} is ready for review.</p>

    <button v-if="nx.approval" class="btn allow" @click="allow(nx.approval)">Allow once</button>
    <button v-if="nx.markDone" class="btn ghost-border" @click="markDone(nx.markDone)">Mark done</button>

    <p v-if="t.projectGone">{{ t.projectName }} is no longer in Settings &gt; Projects, so herd cannot restore this task or touch its repository. Delete task only removes it from herd.</p>
    <div v-if="t.archived" class="row">
      <button v-if="!t.worktreeRemoved && !t.projectGone" class="btn" @click="restore">Restore to Tasks</button>
      <button
        class="btn"
        :class="herd.confirmDelete === t.id ? 'danger' : 'transparent'"
        title="Forget the task in herd, and remove its worktree, local branch and run files (the pushed branch and the PR stay on GitHub)"
        @click="deleteTask"
      >
        {{ herd.confirmDelete === t.id ? 'Delete for good? This cannot be undone' : t.projectGone ? 'Delete task' : 'Delete task and worktree' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
  .scope {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3; /* the full description goes into the sister task */
    overflow: hidden;
  }
.box {
  flex: none;
  border-top: 1px solid var(--border);
  padding: 10px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.top {
  display: flex;
  align-items: center;
  gap: 8px;
}
.small {
  font-size: 12px;
}
.ref {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--muted);
}
.grow {
  flex: 1;
}
textarea {
  resize: none;
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 7px 9px;
  font-size: 13px;
  line-height: 1.45;
}
.warn {
  display: flex;
  gap: 7px;
  align-items: flex-start;
  margin: 0;
  padding: 7px 9px;
  border-radius: 6px;
  background: var(--waiting-bg);
  color: var(--waiting);
  font-size: 12.5px;
  line-height: 1.45;
}
.warn :deep(svg) {
  flex: none;
  margin-top: 2px;
  color: var(--waiting-dot);
}
textarea.need {
  border-color: var(--waiting-dot);
}
textarea:focus {
  outline: none;
}
.opts,
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--muted);
}
.allow {
  align-self: flex-start;
  border-color: var(--approval-dot);
  background: var(--approval-bg);
  color: var(--approval);
}
.ghost-border {
  align-self: flex-start;
  background: transparent;
}
.transparent {
  background: transparent;
}
.sister {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
}
.sister p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.5;
}
</style>
