<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../../lib/state'
import { actions, label } from '../../lib/keymap'

const km = computed(() => herd.settings?.keymap ?? {})
const rows = computed(() =>
  actions
    .map((a) => {
      let keys = (km.value[a.id] ?? []).map(label)
      if (a.id === 'tab1') keys = km.value.tab1 ? [label(km.value.tab1[0]), '…', label(km.value.tab9?.[0])] : []
      return { ...a, keys }
    })
    .filter((r) => r.keys.length),
)
// Actions without a binding, which keymap.json can bind.
const unbound = computed(() => actions.filter((a) => !km.value[a.id]?.length))
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
  <div v-if="unbound.length" class="more">
    <p class="muted small">Not bound (add them to keymap.json, e.g. <code>"palette": ["Mod+P"]</code>):</p>
    <div class="uns muted small"><span v-for="a in unbound" :key="a.id" class="un"><code>{{ a.id }}</code> {{ a.action }}</span></div>
  </div>
</template>

<style scoped>
  .more {
    max-width: 720px;
    margin-top: 14px;
  }
  .more p {
    margin: 0 0 6px;
    line-height: 1.7;
  }
  .uns {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 16px;
  }
  .un {
    white-space: nowrap;
  }
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
