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
async function removeWt() {
  const task = t.value
  if (!task) return
  if (herd.confirmRemove !== task.id) {
    herd.confirmRemove = task.id
    return
  }
  herd.confirmRemove = ''
  const lines = await herd.attempt('Could not remove the worktree', () => api.RemoveWorktree(task.id))
  if (lines) herd.toast('Removed worktree', lines.join('\n'))
}
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

    <template v-if="nx.options.length && !t.archived">
      <label for="guide" class="sr-only">Guidance for Claude</label>
      <textarea id="guide" v-model="herd.guidance" rows="3" placeholder="Guidance for Claude (optional)"></textarea>
      <div class="opts">
        <button v-for="(o, i) in nx.options" :key="o.agent + o.n" class="btn" :class="{ primary: i === 0 }" @click="herd.run(o)">
          <Icon name="play" :size="10" />{{ o.label }}
        </button>
      </div>
    </template>

    <p v-if="nx.text">{{ nx.text }}</p>

    <button v-if="nx.approval" class="btn allow" @click="allow(nx.approval)">Allow once</button>
    <button v-if="nx.markDone" class="btn ghost-border" @click="markDone(nx.markDone)">Mark done</button>

    <div v-if="t.archived && !t.worktreeRemoved" class="row">
      <button class="btn" @click="restore">Restore to Tasks</button>
      <button class="btn" :class="herd.confirmRemove === t.id ? 'danger' : 'transparent'" @click="removeWt">
        {{ herd.confirmRemove === t.id ? 'Remove worktree and branch?' : 'Remove worktree' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
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
  resize: vertical;
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 7px 9px;
  font-size: 13px;
  line-height: 1.45;
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
</style>
