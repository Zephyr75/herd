<script setup lang="ts">
import { watchEffect } from 'vue'
import { herd } from './lib/state'
import TitleBar from './components/TitleBar.vue'
import StatusBar from './components/StatusBar.vue'
import LeftPanel from './components/LeftPanel.vue'
import Center from './components/Center.vue'
import RightPanel from './components/RightPanel.vue'
import Settings from './components/settings/Settings.vue'
import Palette from './components/popups/Palette.vue'
import ScopePicker from './components/popups/ScopePicker.vue'
import NewTask from './components/popups/NewTask.vue'
import Activity from './components/popups/Activity.vue'
import ViewPopups from './components/popups/ViewPopups.vue'

watchEffect(() => {
  document.documentElement.dataset.theme = herd.theme
})
watchEffect(() => {
  const ap = herd.settings?.config.appearance
  if (!ap) return
  const root = document.documentElement.style
  root.setProperty('--ui-font', `'${ap.uiFont}', 'IBM Plex Sans', system-ui, sans-serif`)
  root.setProperty('--mono', `'${ap.termFont}', 'IBM Plex Mono', ui-monospace, monospace`)
  root.setProperty('--ui-size', `${ap.uiFontSize}px`)
})
</script>

<template>
  <div v-if="herd.failed" class="fatal">
    <strong>herd could not load</strong>
    <span class="mono">{{ herd.failed }}</span>
  </div>
  <div v-else-if="herd.ready" class="app">
    <TitleBar />
    <main>
      <template v-if="herd.view === 'board'">
        <LeftPanel v-if="!herd.leftHidden" />
        <Center />
        <RightPanel v-if="!herd.rightHidden" />
      </template>
      <Settings v-else />
    </main>
    <StatusBar />

    <Palette v-if="herd.overlay === 'palette'" />
    <ScopePicker v-if="herd.overlay === 'scope'" />
    <NewTask v-if="herd.overlay === 'new'" />
    <Activity v-if="herd.overlay === 'activity'" />
    <ViewPopups v-if="herd.overlay === 'views' && (herd.diffOf || herd.artifactOf || herd.diagramOf)" />
  </div>
</template>

<style scoped>
.app {
  position: relative;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg);
  color: var(--text);
}
main {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: stretch;
}
.fatal {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
}
.fatal strong {
  color: var(--text);
}
</style>
