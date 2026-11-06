<script setup lang="ts">
// A task's diagram pages (drawn by its agents with archify), full size with
// archify's own viewer: zoom, focus, Live/Still, export. Every diagram of the
// task is listed on the left, in step order, to compare the plan with what
// each Implement built. Sandboxed: a page cannot reach the app.
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { herd } from '../../lib/state'
import { diagramURL } from '../../lib/markdown'
import type { Diagram } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import Splitter from '../ui/Splitter.vue'
import { SIDEBAR, useSidebar } from '../../lib/sidebar'

const list = ref<Diagram[]>([])
const cur = computed(() => list.value.find((d) => d.file === herd.diagramOf?.file))
watch(
  () => [herd.diagramOf?.task, herd.diagramRev] as const,
  async ([task]) => {
    if (task) list.value = await api.Diagrams(task).catch(() => [])
  },
  { immediate: true },
)
// The page reloads when its agent rewrites it.
const src = computed(() => (herd.diagramOf ? diagramURL(herd.diagramOf.task, herd.diagramOf.file, herd.theme === 'fleet-dark') + '&v=' + (cur.value?.mod ?? '') : ''))

const pick = (file: string) => herd.diagramOf && (herd.diagramOf = { ...herd.diagramOf, file })
const sb = useSidebar('diagram', 220)
const box = ref<HTMLDivElement>()
onMounted(() => box.value?.focus())

// Up and down move through the diagrams (while the list has focus: inside the
// page, keys belong to archify's viewer).
function key(e: KeyboardEvent) {
  const i = list.value.findIndex((d) => d.file === herd.diagramOf?.file)
  if (e.key === 'ArrowDown' && i < list.value.length - 1) pick(list.value[i + 1].file)
  else if (e.key === 'ArrowUp' && i > 0) pick(list.value[i - 1].file)
  else return
  e.preventDefault()
}
</script>

<template>
  <div ref="box" class="box popup" role="dialog" aria-label="Diagram" tabindex="-1" @keydown="key">
    <div class="head">
      <button class="toggle" :aria-label="sb.hidden.value ? 'Show the side bar' : 'Hide the side bar'" :title="sb.hidden.value ? 'Show the side bar' : 'Hide the side bar'" @click="sb.hidden.value = !sb.hidden.value"><Icon :name="sb.hidden.value ? 'panel-left-off' : 'panel-left'" :size="16" /></button>
      <Icon name="eye" />
      <span class="title">{{ cur?.title || herd.diagramOf?.file }}</span>
      <span class="muted small">{{ cur?.step }}{{ cur?.step ? ' · ' : '' }}<span class="mono">{{ herd.diagramOf?.file }}</span></span>
      <span class="grow"></span>
      <button class="x" aria-label="Close" title="Close (Esc)" @click="herd.closeDiagram()"><Icon name="close" :size="14" /></button>
    </div>
    <div class="body">
      <div v-if="!sb.hidden.value" class="side-wrap" :style="{ width: sb.width.value + 'px' }">
        <div class="side">
          <button v-for="d in list" :key="d.file" class="item" :class="{ on: d.file === herd.diagramOf?.file }" @click="pick(d.file)">
            <span class="name">{{ d.title }}</span>
            <span class="file mono">{{ d.step ? d.step + ' · ' : '' }}{{ d.file }}</span>
          </button>
        </div>
        <Splitter side="left" :width="sb.width.value" :min="SIDEBAR.min" :max="SIDEBAR.max" :def="sb.def" label="Resize the side bar" @resize="(w) => (sb.width.value = w)" />
      </div>
      <iframe v-if="src" :key="src" class="page" :src="src" :title="cur?.title || herd.diagramOf?.file" sandbox="allow-scripts allow-downloads"></iframe>
    </div>
  </div>
</template>

<style scoped>
  .box {
    flex: 1 1 0;
    min-width: 0;
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
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .small {
    font-size: 12px;
    white-space: nowrap;
  }
  .grow {
    flex: 1;
  }
  .x,
  .toggle {
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
  .toggle {
    margin-right: 4px;
  }
  .x:hover,
  .toggle:hover {
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
    max-width: 40%;
    border-right: 1px solid var(--border);
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
  .page {
    flex: 1;
    min-width: 0;
    border: 0;
    border-bottom-right-radius: 8px;
    background: var(--bg);
  }
</style>
