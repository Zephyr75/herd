<script setup lang="ts">
// herd's dropdown: a dashed chip that opens a small menu. The chip shows the
// picked item, or the slot's content (e.g. "+ Add").
import { onBeforeUnmount, ref, watch } from 'vue'
import Icon from './Icon.vue'

export interface DropItem {
  value: string
  label: string
  disabled?: boolean
  title?: string
}

const props = withDefaults(defineProps<{ items: DropItem[]; value?: string; placeholder?: string; wide?: boolean; strong?: boolean; disabled?: boolean }>(), {
  value: '',
  placeholder: '',
})
const emit = defineEmits<{ pick: [value: string] }>()
const open = ref(false)
const root = ref<HTMLElement>()

function choose(it: DropItem) {
  if (it.disabled) return
  open.value = false
  emit('pick', it.value)
}
// Closes on a click anywhere else (WebKit does not focus buttons on click, so
// focus events cannot tell).
const outside = (e: PointerEvent) => root.value && !root.value.contains(e.target as Node) && (open.value = false)
watch(open, (on) => (on ? window.addEventListener('pointerdown', outside, true) : window.removeEventListener('pointerdown', outside, true)))
onBeforeUnmount(() => window.removeEventListener('pointerdown', outside, true))
</script>

<template>
  <span ref="root" class="dd" :class="{ wide }" @keydown.esc.stop="open = false">
    <button type="button" class="chip" :class="{ on: open, strong }" :disabled="disabled" aria-haspopup="menu" :aria-expanded="open" @click="open = !open">
      <slot>
        <span class="label">{{ items.find((i) => i.value === props.value)?.label ?? placeholder }}</span>
        <Icon name="chevron" :size="12" />
      </slot>
    </button>
    <span v-if="open" class="menu popup" role="menu">
      <button v-for="it in items" :key="it.value" type="button" class="item" :class="{ cur: it.value === value }" :disabled="it.disabled" :title="it.title" role="menuitem" @click="choose(it)">
        {{ it.label }}
      </button>
    </span>
  </span>
</template>

<style scoped>
  .dd {
    position: relative;
    display: inline-flex;
    min-width: 0;
  }
  .dd.wide,
  .dd.wide .chip {
    width: 100%;
  }
  .chip {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    height: 26px;
    padding: 0 8px 0 10px;
    border-radius: 4px;
    border: 1px dashed var(--border);
    background: transparent;
    color: var(--text);
    font-size: 12.5px;
  }
  .chip.strong {
    font-weight: 600;
    font-size: 13px;
  }
  .chip:hover:not(:disabled),
  .chip.on {
    border-style: solid;
    background: var(--hover);
  }
  .chip:disabled {
    color: var(--muted);
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: left;
  }
  .menu {
    position: absolute;
    z-index: 30;
    top: calc(100% + 4px);
    left: 0;
    min-width: 100%;
    max-width: 560px;
    max-height: 50vh;
    overflow: auto;
    padding: 4px;
    display: flex;
    flex-direction: column;
  }
  .item {
    padding: 5px 8px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text);
    text-align: left;
    font-size: 12.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item:hover:not(:disabled) {
    background: var(--hover);
  }
  .item.cur {
    background: var(--sel);
  }
  .item:disabled {
    color: var(--faint);
  }
</style>
