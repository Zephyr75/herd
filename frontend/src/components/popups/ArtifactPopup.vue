<script setup lang="ts">
// The task's artifacts, large: every step's markdown listed on the left, the
// selected one rendered on the right for easier reading.
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { herd } from '../../lib/state'
import type { Artifact } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import ArtifactView from '../ArtifactView.vue'
import Splitter from '../ui/Splitter.vue'
import { SIDEBAR, useSidebar } from '../../lib/sidebar'

const task = computed(() => herd.tasks.find((t) => t.steps.some((s) => s.id === herd.artifactOf)))
const steps = computed(() => task.value?.steps ?? [])
const step = computed(() => steps.value.find((s) => s.id === herd.artifactOf))
const art = ref<Artifact | null>(null)

// Reload as Claude rewrites it.
let seq = 0
watch(
  () => [herd.artifactOf, step.value?.state, herd.artifactRev] as const,
  async ([id]) => {
    const mine = ++seq
    const a = await api.ReadArtifact(id).catch(() => null)
    if (mine === seq) art.value = a
  },
  { immediate: true },
)
watch(
  () => herd.artifactOf,
  () => scroll.value?.scrollTo({ top: 0 }),
)

const pick = (id: string) => (herd.artifactOf = id)
const sb = useSidebar('artifact', 220)
const close = () => herd.closeArtifactPopup()
const box = ref<HTMLDivElement>()
const scroll = ref<HTMLDivElement>()
onMounted(() => box.value?.focus())

// Up and down move through the steps.
function key(e: KeyboardEvent) {
  const i = steps.value.findIndex((s) => s.id === herd.artifactOf)
  if (e.key === 'ArrowDown' && i < steps.value.length - 1) pick(steps.value[i + 1].id)
  else if (e.key === 'ArrowUp' && i > 0) pick(steps.value[i - 1].id)
  else return
  e.preventDefault()
}
</script>

<template>
  <div ref="box" class="box popup" role="dialog" aria-label="Artifacts" tabindex="-1" @keydown="key">
    <div class="head">
      <button class="toggle" :aria-label="sb.hidden.value ? 'Show the side bar' : 'Hide the side bar'" :title="sb.hidden.value ? 'Show the side bar' : 'Hide the side bar'" @click="sb.hidden.value = !sb.hidden.value"><Icon :name="sb.hidden.value ? 'panel-left-off' : 'panel-left'" :size="16" /></button>
      <Icon name="file" />
      <span class="title">{{ art?.file || step?.artifact || 'Artifact' }}</span>
      <span class="muted small">{{ step?.name }}</span>
      <span class="grow"></span>
      <button v-if="!herd.diffOf" class="other" title="Open this step's changes beside the artifact" @click="herd.showDiffToo(herd.artifactOf)"><Icon name="eye" :size="13" />Changes</button>
      <button class="x" aria-label="Close" title="Close (Esc closes both)" @click="close"><Icon name="close" :size="14" /></button>
    </div>
    <div class="body">
      <div v-if="!sb.hidden.value" class="side-wrap" :style="{ width: sb.width.value + 'px' }">
        <div class="side">
          <button v-for="s in steps" :key="s.id" class="item" :class="{ on: s.id === herd.artifactOf }" @click="pick(s.id)">
            <span class="name">{{ s.name }}</span>
            <span class="file mono">{{ s.artifact }}</span>
          </button>
        </div>
        <Splitter side="left" :width="sb.width.value" :min="SIDEBAR.min" :max="SIDEBAR.max" :def="sb.def" label="Resize the side bar" @resize="(w) => (sb.width.value = w)" />
      </div>
      <div ref="scroll" class="scroll">
        <div class="page">
          <p v-if="!art" class="empty">Loading…</p>
          <p v-else-if="!art.exists" class="empty">{{ art.file || 'The artifact' }} has not been written yet.</p>
          <ArtifactView v-else :art="art" :zoom="1.6" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .box {
    flex: 1 1 0;
    min-width: 0;
    max-width: 1400px;
    display: flex;
    flex-direction: column;
    outline: none;
  }
  .other {
    flex: none;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    margin-right: 4px;
    padding: 0 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
  .other:hover {
    background: var(--hover);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: none;
    height: 40px;
    padding: 0 8px 0 14px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    font-weight: 600;
  }
  .small {
    font-size: 12px;
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
  .side-wrap {
    position: relative;
    flex: none;
    max-width: 50%;
    border-right: 1px solid var(--border);
  }
  .toggle {
    width: 26px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 4px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .toggle:hover {
    background: var(--hover);
    color: var(--text);
  }
  .side {
    height: 100%;
    padding: 10px 8px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .item {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 6px 8px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text);
    text-align: left;
  }
  .item:hover {
    background: var(--hover);
  }
  .item.on {
    background: var(--sel);
  }
  .name {
    font-size: 13px;
    font-weight: 500;
  }
  .file {
    font-size: 11px;
    color: var(--muted);
  }
  .scroll {
    flex: 1;
    min-width: 0;
    overflow: auto;
  }
  .page {
    padding: 22px 32px 48px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  /* Bigger type than the Preview tab. */
  .page :deep(.md) {
    font-size: 15.5px;
    line-height: 1.65;
  }
  .page :deep(.md h1) {
    font-size: 21px;
  }
  .page :deep(.md h2) {
    font-size: 18px;
  }
  .page :deep(.md h3),
  .page :deep(.md h4) {
    font-size: 16px;
  }
  .page :deep(.md code),
  .page :deep(.md pre:not(.mermaid)) {
    font-size: 13.5px;
  }
  .page :deep(.md table) {
    font-size: 14.5px;
  }
  .page :deep(.fm) {
    font-size: 12.5px;
  }
  .empty {
    margin: 0;
    color: var(--muted);
    font-size: 13px;
  }
</style>
