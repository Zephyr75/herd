<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import { render, renderMermaid } from '../lib/markdown'
import Icon from './ui/Icon.vue'
import ChangesView from './Changes.vue'
import type { Artifact, Changes } from '../lib/types'

const step = computed(() => herd.selStep)
const art = ref<Artifact | null>(null)
const body = ref<HTMLDivElement>()

let seq = 0
watch(
  () => [step.value?.id, step.value?.state, herd.artifactRev] as const,
  ([id]) => {
    const mine = ++seq
    if (!id) {
      art.value = null
      return
    }
    api.ReadArtifact(id).then(
      (a) => {
        if (mine === seq) art.value = a
      },
      () => {
        if (mine === seq) art.value = null
      },
    )
  },
  { immediate: true },
)

// The step's changed files, refreshed as the task changes (debounced: hooks
// fire often while a step works).
const changes = ref<Changes | null>(null)
let cseq = 0
let ctimer: ReturnType<typeof setTimeout> | undefined
function loadChanges(id: string | undefined, delay: number) {
  clearTimeout(ctimer)
  const mine = ++cseq
  if (!id) {
    changes.value = null
    return
  }
  ctimer = setTimeout(() => {
    api.StepChanges(id).then(
      (c) => mine === cseq && (changes.value = c),
      () => mine === cseq && (changes.value = { source: '', sha: '', via: '', files: [] }),
    )
  }, delay)
}
watch(
  () => step.value?.id,
  (id) => {
    changes.value = null
    loadChanges(id, 0)
  },
  { immediate: true },
)
watch(
  () => [herd.selTask, step.value?.sha, step.value?.state] as const,
  () => loadChanges(step.value?.id, 800),
)

const html = computed(() => (art.value?.exists ? render(art.value.body) : ''))
// The body div is recreated when the Preview tab comes back, so redraw then too.
watch(
  () => [html.value, herd.theme, herd.rightTab] as const,
  async () => {
    await nextTick()
    if (!body.value || !html.value) return
    // v-html leaves rendered diagrams in place; reset them so a theme change redraws.
    body.value.innerHTML = html.value
    void renderMermaid(body.value, herd.theme === 'fleet-dark')
  },
  { immediate: true },
)

const file = computed(() => art.value?.file || step.value?.preview || '')
const vcls = (v: string) => (v === 'changes' ? 'c-waiting' : v === 'approve' || v === 'done' ? 'c-done' : 'c-text')
</script>

<template>
  <div class="wrap">
    <div class="bar">
      <button class="tab" :class="{ on: herd.rightTab === 'preview' }" @click="herd.rightTab = 'preview'"><Icon name="file" />Preview {{ file }}</button>
      <button v-if="step" class="tab" :class="{ on: herd.rightTab === 'changes' }" @click="herd.rightTab = 'changes'">
        Changes<span v-if="changes?.files.length" class="count">{{ changes.files.length }}</span>
      </button>
      <span class="grow"></span>
    </div>
    <div class="content">
      <ChangesView v-if="step && herd.rightTab === 'changes'" :changes="changes" />
      <p v-else-if="!step" class="empty">Open a step to preview the artifact it wrote.</p>
      <p v-else-if="!art || !art.exists" class="empty">{{ file || 'The artifact' }} has not been written yet. It appears here as soon as Claude writes it.</p>
      <template v-else>
        <div v-if="art.front?.length" class="front">
          <span v-for="f in art.front" :key="f.k" class="fm">{{ f.k }}: <span :class="vcls(f.v)">{{ f.v }}</span></span>
        </div>
        <div ref="body" class="md"></div>
      </template>
    </div>
  </div>
</template>

<style scoped>

  .wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border);
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: stretch;
    flex: none;
    height: 34px;
    background: var(--chrome);
    border-bottom: 1px solid var(--border);
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 0 14px;
    border: 0;
    border-right: 1px solid var(--border);
    background: var(--chrome);
    color: var(--muted);
    white-space: nowrap;
    font-size: 13px;
  }
  .tab.on {
    background: var(--bg);
    color: var(--text);
    margin-bottom: -1px;
  }
  .count {
    font-family: var(--mono);
    font-size: 11px;
    padding: 0 5px;
    border-radius: 8px;
    background: var(--surface);
    color: var(--muted);
  }
  .grow {
    flex: 1;
  }
  .content {
    flex: 1;
    min-height: 0;
    padding: 16px 20px 28px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .empty {
    margin: 0;
    color: var(--muted);
    font-size: 12.5px;
    line-height: 1.5;
  }
  .front {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .fm {
    font-family: var(--mono);
    font-size: 11.5px;
    padding: 2px 7px;
    border-radius: 4px;
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--muted);
  }
  .c-waiting { color: var(--waiting); }
  .c-done { color: var(--done); }
  .c-text { color: var(--text); }

  .md {
    font-size: 13px;
    line-height: 1.6;
  }
  .md :deep(h1),
  .md :deep(h2),
  .md :deep(h3),
  .md :deep(h4) {
    margin: 14px 0 8px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
    font-size: 15px;
    font-weight: 600;
  }
  .md :deep(h1) {
    font-size: 16px;
  }
  .md > :deep(:first-child) {
    margin-top: 0;
  }
  .md :deep(p) {
    margin: 0 0 10px;
  }
  .md :deep(ul),
  .md :deep(ol) {
    margin: 0 0 10px;
    padding-left: 18px;
  }
  .md :deep(li) {
    margin: 3px 0;
  }
  .md :deep(code) {
    font-family: var(--mono);
    font-size: 12px;
    padding: 1px 4px;
    border-radius: 4px;
    background: var(--surface);
  }
  .md :deep(pre:not(.mermaid)) {
    margin: 0 0 10px;
    padding: 10px 12px;
    border-radius: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
  }
  .md :deep(pre code) {
    padding: 0;
    background: none;
  }
  .md :deep(a) {
    color: var(--accent);
  }
  .md :deep(blockquote) {
    margin: 0 0 10px;
    padding-left: 10px;
    border-left: 2px solid var(--border);
    color: var(--muted);
  }
  .md :deep(table) {
    border-collapse: collapse;
    margin-bottom: 10px;
    font-size: 12.5px;
  }
  .md :deep(th),
  .md :deep(td) {
    border: 1px solid var(--border);
    padding: 4px 8px;
  }
  .md :deep(.mermaid-fig) {
    margin: 0 0 10px;
    padding: 16px;
    border-radius: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  .md :deep(pre.mermaid) {
    margin: 0;
    background: none;
    border: 0;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--muted);
    white-space: pre-wrap;
  }
  .md :deep(figcaption) {
    margin-top: 10px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--muted);
  }
</style>
