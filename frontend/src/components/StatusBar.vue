<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../lib/state'

const counts = computed(() => {
  const c = { running: 0, waiting: 0, approval: 0 }
  const projects = new Set<string>()
  for (const t of herd.tasks) {
    if (t.archived) continue
    const k = t.status.kind
    if (k === 'running' || k === 'waiting' || k === 'approval') c[k]++
    if (k === 'running') projects.add(t.project)
  }
  return { ...c, projects: projects.size }
})
const modelLabel = (m: string) => (m ? m[0].toUpperCase() + m.slice(1) : '')
const right = computed(() => {
  const t = herd.selTask
  if (!t) return ''
  const parts = [t.branch, t.projectName]
  if (herd.selStep) parts.unshift(modelLabel(herd.selStep.model))
  return parts.filter(Boolean).join(' · ')
})
const autoReview = computed(() => herd.settings?.agents.some((a) => a.trigger.startsWith('after:')) ?? false)
</script>

<template>
  <footer>
    <span v-if="counts.running" class="item"><span class="dot s-running"></span>{{ counts.running }} running</span>
    <span v-if="counts.waiting" class="item"><span class="dot s-waiting"></span>{{ counts.waiting }} waiting for input</span>
    <span v-if="counts.approval" class="item"><span class="dot s-approval"></span>{{ counts.approval }} needs approval</span>
    <span v-if="counts.projects">across {{ counts.projects }} project{{ counts.projects > 1 ? 's' : '' }}</span>
    <span class="grow"></span>
    <span>{{ right }}</span>
    <span v-if="autoReview">auto-review on</span>
  </footer>
</template>

<style scoped>
footer {
  display: flex;
  align-items: center;
  gap: 14px;
  flex: none;
  height: 26px;
  padding: 0 12px;
  background: var(--chrome);
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
}
.item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.dot {
  background: var(--dot) !important;
}
.grow {
  flex: 1;
}
</style>
