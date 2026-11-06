<script setup lang="ts">
// An artifact rendered: its markdown with mermaid diagrams and embedded
// diagram pages (the front matter is left out: the step list already shows
// it). Used by the Preview tab and the artifact pop-up.
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { herd } from '../lib/state'
import { render, renderMermaid } from '../lib/markdown'
import type { Artifact } from '../lib/types'

// zoom enlarges mermaid diagrams up to that many times their natural size,
// as far as the width allows (they render small by default).
const props = withDefaults(defineProps<{ art: Artifact; task: string; zoom?: number }>(), { zoom: 1.3 })
const body = ref<HTMLDivElement>()

const html = computed(() => render(props.art.body, { task: props.task, dark: herd.theme === 'fleet-dark', rev: herd.diagramRev }))
// An embedded diagram page reports its size (see embedFit in diagrams.go):
// its iframe takes the diagram's shape, with no empty band under it.
function fit(e: MessageEvent) {
  const d = (e.data as { herdDiagram?: { ratio?: number; height?: number } })?.herdDiagram
  const frame = d && [...(body.value?.querySelectorAll('iframe') ?? [])].find((f) => f.contentWindow === e.source)
  if (!frame) return
  if (d.ratio && d.ratio > 0) frame.style.aspectRatio = String(d.ratio)
  else if (d.height && d.height > 0) frame.style.height = Math.min(d.height, 4000) + 'px'
}
window.addEventListener('message', fit)
onBeforeUnmount(() => window.removeEventListener('message', fit))

// "Open full size" on an embedded diagram page.
function click(e: MouseEvent) {
  const file = (e.target as HTMLElement).closest<HTMLElement>('[data-diagram]')?.dataset.diagram
  if (file) herd.openDiagram(props.task, file)
}
watch(
  () => [html.value, herd.theme, body.value] as const,
  async () => {
    await nextTick()
    if (!body.value) return
    // v-html leaves rendered diagrams in place; reset them so a theme change redraws.
    body.value.innerHTML = html.value
    const el = body.value
    await renderMermaid(el, herd.theme === 'fleet-dark')
    el.querySelectorAll<SVGSVGElement>('pre.mermaid svg').forEach((svg) => {
      const w = svg.viewBox?.baseVal?.width
      if (!w) return
      svg.setAttribute('width', '100%')
      svg.removeAttribute('height')
      svg.style.maxWidth = `${Math.round(w * props.zoom)}px`
    })
  },
  { immediate: true },
)
</script>

<template>
  <div ref="body" class="md" @click="click"></div>
</template>

<style scoped>
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
  .md :deep(.diagram-fig) {
    margin: 0 0 12px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--surface);
    overflow: hidden;
  }
  .md :deep(.diagram-fig iframe) {
    display: block;
    width: 100%;
    aspect-ratio: 16 / 10; /* until the page reports its own shape */
    border: 0;
    background: var(--bg);
  }
  .md :deep(.diagram-fig figcaption) {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px 6px 12px;
    border-top: 1px solid var(--border);
    font-size: 12px;
    color: var(--muted);
  }
  .md :deep(.diagram-fig figcaption span) {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .md :deep(.diagram-fig button) {
    flex: none;
    height: 22px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--text);
    font-size: 12px;
  }
  .md :deep(.diagram-fig button:hover) {
    background: var(--hover);
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
    align-self: stretch; /* full width, so the diagram can grow to its zoom */
  }
  .md :deep(pre.mermaid svg) {
    display: block;
    margin: 0 auto;
  }
  .md :deep(figcaption) {
    margin-top: 10px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--muted);
  }
</style>
