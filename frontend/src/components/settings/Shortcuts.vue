<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../../lib/state'
import { actions, label } from '../../lib/keymap'

const rows = computed(() =>
  actions.map((a) => {
    const km = herd.settings?.keymap ?? {}
    let keys = (km[a.id] ?? []).map(label)
    if (a.id === 'tab1') keys = [label(km.tab1?.[0]), '…', label(km.tab9?.[0])]
    return { ...a, keys }
  }),
)
</script>

<template>
  <div class="intro">
    <h1>Shortcuts</h1>
    <p>Ctrl replaces ⌘ on Linux and Windows. Bindings live in {{ herd.settings?.dir ?? '~/.herd' }}/keymap.json, where “Mod” means ⌘ or Ctrl.</p>
  </div>

  <div class="table card">
    <div v-for="r in rows" :key="r.id" class="r">
      <span class="grow">{{ r.action }}</span>
      <span class="muted small">{{ r.where }}</span>
      <span class="keys">
        <span v-for="(k, i) in r.keys" :key="i" class="kbd">{{ k }}</span>
      </span>
    </div>
  </div>
</template>

<style scoped>
  .table {
    display: flex;
    flex-direction: column;
    max-width: 720px;
  }
  .r {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
  }
  .r + .r {
    border-top: 1px solid var(--border);
  }
  .grow {
    flex: 1;
  }
  .small {
    font-size: 12px;
  }
  .keys {
    display: flex;
    gap: 4px;
    min-width: 70px;
    justify-content: flex-end;
  }
</style>
