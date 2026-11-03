<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import ChangesView from './Changes.vue'
import ArtifactView from './ArtifactView.vue'
import type { Artifact, Changes } from '../lib/types'

const step = computed(() => herd.selStep)
const art = ref<Artifact | null>(null)

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
      () => mine === cseq && (changes.value = { source: '', sha: '', via: '', files: [], repos: [] }),
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

const file = computed(() => art.value?.file || step.value?.artifact || '')
// The Changes tab only shows when the step changed files; Preview stands in otherwise.
const tab = computed(() => (step.value && changes.value?.files.length && herd.rightTab === 'changes' ? 'changes' : 'preview'))
</script>

<template>
  <div class="wrap">
    <div class="bar">
      <div class="tab" role="tab" tabindex="0" :class="{ on: tab === 'preview' }" @click="herd.rightTab = 'preview'" @keydown.enter="herd.rightTab = 'preview'">
        <Icon name="file" />Preview {{ file }}
        <button v-if="step && art?.exists" class="big" aria-label="Open in a large pop-up" title="Open in a large pop-up" @click.stop="herd.openArtifactPopup(step.id)">
          <Icon name="maximize" :size="16" />
        </button>
      </div>
      <button v-if="step && changes?.files.length" class="tab" :class="{ on: tab === 'changes' }" @click="herd.rightTab = 'changes'">
        Changes<span class="count">{{ changes.files.length }}</span>
      </button>
      <span class="grow"></span>
    </div>
    <div class="content">
      <ChangesView v-if="tab === 'changes'" :changes="changes" />
      <p v-else-if="!step" class="empty">Open a step to preview the artifact it wrote.</p>
      <p v-else-if="!art || !art.exists" class="empty">{{ file || 'The artifact' }} has not been written yet. It appears here as soon as {{ step.external ? "Copilot's review arrives" : 'Claude writes it' }}.</p>
      <ArtifactView v-else :art="art" />
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
  .tab {
    cursor: default;
  }
  .big {
    flex: none;
    width: 24px;
    height: 24px;
    margin: 0 -6px 0 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .big:hover {
    background: var(--hover);
    color: var(--text);
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
</style>
