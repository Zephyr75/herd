<script setup lang="ts">
// The task's artifacts, large: every step's markdown listed on the left, the
// selected one rendered on the right for easier reading.
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { herd } from '../../lib/state'
import type { Artifact } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import ArtifactView from '../ArtifactView.vue'

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
const close = () => {
  herd.overlay = 'none'
  herd.artifactOf = ''
}
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
  <button class="scrim dim" aria-label="Close" @click="close"></button>
  <div ref="box" class="box popup" role="dialog" aria-label="Artifacts" tabindex="-1" @keydown="key">
    <div class="head">
      <Icon name="file" />
      <span class="title">{{ art?.file || step?.artifact || 'Artifact' }}</span>
      <span class="muted small">{{ step?.name }}</span>
      <span class="grow"></span>
      <button class="x" aria-label="Close" title="Close (Esc)" @click="close"><Icon name="close" :size="14" /></button>
    </div>
    <div class="body">
      <div class="side">
        <button v-for="s in steps" :key="s.id" class="item" :class="{ on: s.id === herd.artifactOf }" @click="pick(s.id)">
          <span class="name">{{ s.name }}</span>
          <span class="file mono">{{ s.artifact }}</span>
        </button>
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
    position: absolute;
    z-index: 21;
    top: 48px;
    bottom: 40px;
    left: 50%;
    transform: translateX(-50%);
    width: min(1400px, calc(100% - 48px));
    display: flex;
    flex-direction: column;
    outline: none;
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
  .side {
    flex: none;
    width: 220px;
    padding: 10px 8px;
    border-right: 1px solid var(--border);
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
