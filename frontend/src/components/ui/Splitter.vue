<script setup lang="ts">
// Drag handle on a side panel's inner edge. Drag to resize, double-click to
// reset, or focus it and use the arrow keys.
import { ref } from 'vue'
import { herd, PANELS, type PanelSide } from '../../lib/state'

const props = defineProps<{ side: PanelSide }>()
const dragging = ref(false)

const width = () => (props.side === 'left' ? herd.leftWidth : herd.rightWidth)

function down(e: PointerEvent) {
  if (e.button !== 0) return
  e.preventDefault()
  const el = e.currentTarget as HTMLElement
  el.setPointerCapture(e.pointerId)
  dragging.value = true
  const startX = e.clientX
  const startW = width()
  const move = (ev: PointerEvent) => {
    const dx = ev.clientX - startX
    herd.setPanelWidth(props.side, props.side === 'left' ? startW + dx : startW - dx)
  }
  const up = () => {
    dragging.value = false
    el.removeEventListener('pointermove', move)
    el.removeEventListener('pointerup', up)
    el.removeEventListener('pointercancel', up)
  }
  el.addEventListener('pointermove', move)
  el.addEventListener('pointerup', up)
  el.addEventListener('pointercancel', up)
}

function key(e: KeyboardEvent) {
  const step = e.shiftKey ? 40 : 10
  const grow = props.side === 'left' ? 'ArrowRight' : 'ArrowLeft'
  const shrink = props.side === 'left' ? 'ArrowLeft' : 'ArrowRight'
  if (e.key === grow) herd.setPanelWidth(props.side, width() + step)
  else if (e.key === shrink) herd.setPanelWidth(props.side, width() - step)
  else return
  e.preventDefault()
}
</script>

<template>
  <div
    class="splitter"
    :class="[side, { dragging }]"
    role="separator"
    aria-orientation="vertical"
    :aria-label="side === 'left' ? 'Resize the tasks panel' : 'Resize the steps panel'"
    :aria-valuenow="width()"
    :aria-valuemin="PANELS[side].min"
    :aria-valuemax="PANELS[side].max"
    tabindex="0"
    title="Drag to resize · double-click to reset"
    @pointerdown="down"
    @dblclick="herd.setPanelWidth(side, PANELS[side].def)"
    @keydown="key"
  ></div>
</template>

<style scoped>
.splitter {
  position: absolute;
  top: 0;
  bottom: 0;
  z-index: 5;
  width: 7px;
  cursor: col-resize;
  touch-action: none;
}
.splitter.left {
  right: -4px;
}
.splitter.right {
  left: -4px;
}
.splitter::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 3px;
  width: 1px;
  background: transparent;
  transition: background 0.12s;
}
.splitter:hover::after,
.splitter:focus-visible::after,
.splitter.dragging::after {
  background: var(--focus);
  width: 2px;
  left: 2.5px;
}
.splitter:focus-visible {
  outline: none;
}
</style>
