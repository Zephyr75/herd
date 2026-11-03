<script setup lang="ts">
import { computed } from 'vue'
import { herd, type SettingsTab } from '../../lib/state'
import Toasts from '../Toasts.vue'
import Agents from './Agents.vue'
import ContextPane from './Context.vue'
import Projects from './Projects.vue'
import Appearance from './Appearance.vue'
import Shortcuts from './Shortcuts.vue'
import About from './About.vue'

const nav: [SettingsTab, string][] = [['about', 'About'], ['context', 'Context'], ['agents', 'Agents'], ['projects', 'Projects'], ['appearance', 'Appearance'], ['shortcuts', 'Shortcuts']]
const file = computed(
  () => ({ agents: 'agents.toml', context: 'context/', projects: 'config.toml', appearance: 'config.toml', shortcuts: 'keymap.json', about: '' })[herd.settingsTab],
)
</script>

<template>
  <aside>
    <span class="head">Settings</span>
    <button v-for="[id, label] in nav" :key="id" class="item" :class="{ on: herd.settingsTab === id }" @click="herd.settingsTab = id">{{ label }}</button>
    <span class="grow"></span>
    <span v-if="file" class="path">{{ herd.settings?.dir ?? '~/.herd' }}/{{ file }}</span>
    <button class="back" @click="herd.view = 'board'">Back to board</button>
  </aside>

  <section>
    <div class="col">
      <Agents v-if="herd.settingsTab === 'agents'" />
      <ContextPane v-else-if="herd.settingsTab === 'context'" />
      <Projects v-else-if="herd.settingsTab === 'projects'" />
      <Appearance v-else-if="herd.settingsTab === 'appearance'" />
      <Shortcuts v-else-if="herd.settingsTab === 'shortcuts'" />
      <About v-else />
    </div>
    <Toasts />
  </section>
</template>

<style scoped>
  aside {
    flex: 1 1 220px;
    max-width: 240px;
    min-width: 200px;
    background: var(--bg);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    padding: 10px 6px;
  }
  .head {
    padding: 4px 10px 10px;
    font-size: 12px;
    color: var(--muted);
  }
  .item {
    height: 30px;
    padding: 0 10px;
    border-radius: 6px;
    text-align: left;
    font-size: 13px;
    color: var(--text);
    border: 1px solid transparent;
    background: transparent;
  }
  .item:hover {
    background: var(--hover);
  }
  .item.on {
    background: var(--sel);
  }
  .grow {
    flex: 1;
  }
  .path {
    padding: 10px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--muted);
    word-break: break-all;
  }
  .back {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    margin: 0 4px 4px;
    height: 26px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
  }
  section {
    position: relative;
    flex: 999 1 640px;
    min-width: 0;
    padding: 28px 40px 48px;
    overflow: auto;
  }
  .col {
    max-width: 980px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .col :deep(h1) {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
  }
  .col :deep(.intro) {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .col :deep(.intro p) {
    margin: 0;
    color: var(--muted);
    line-height: 1.5;
  }
  .col :deep(.list-btn) {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    width: 100%;
    padding: 7px 10px;
    border-radius: 6px;
    text-align: left;
    font-size: 13px;
    color: var(--text);
    border: 1px solid transparent;
    background: transparent;
  }
  .col :deep(.list-btn:hover) {
    background: var(--hover);
  }
  .col :deep(.list-btn.on) {
    background: var(--sel);
  }
  .col :deep(.list-btn .sub) {
    font-size: 12px;
    color: var(--muted);
  }
  .col :deep(.card) {
    border: 1px solid var(--border);
    border-radius: 8px;
  }
</style>
