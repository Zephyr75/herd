<script setup lang="ts">
// herd's top bar is the window's titlebar: drag it to move the window,
// double-click to maximise. On Linux and Windows the window is frameless and
// the controls are drawn here; macOS keeps its traffic lights.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { herd } from '../lib/state'
import { isMaximised, minimise, quit, toggleMaximise } from '../lib/window'
import Icon from './ui/Icon.vue'
import Kbd from './ui/Kbd.vue'

const t = computed(() => herd.selTask)
const scopeOpen = (m: string) => herd.overlay === 'scope' && herd.scope === m
const controls = computed(() => herd.os !== 'darwin')

const maximised = ref(false)
const sync = async () => (maximised.value = await isMaximised())
onMounted(() => {
  void sync()
  window.addEventListener('resize', sync)
})
onBeforeUnmount(() => window.removeEventListener('resize', sync))

// Double-click on the bar itself (not on a button) maximises, like a native titlebar.
function dblclick(e: MouseEvent) {
  if (!controls.value) return
  if ((e.target as HTMLElement).closest('button, input')) return
  toggleMaximise()
}
</script>

<template>
  <header :class="{ mac: herd.os === 'darwin' }" @dblclick="dblclick">
    <button class="gear" :class="{ on: herd.view === 'settings' }" aria-label="Settings" title="Settings" @click="herd.command('settings')">
      <Icon name="gear" :size="15" />
    </button>
    <span class="mark">herd</span>
    <template v-if="herd.view === 'board' && t">
      <button class="crumb project" :class="{ on: scopeOpen('project') }" @click="herd.openOverlay('scope', 'project')">
        {{ t.projectName }}<Icon name="chevron" :size="10" :stroke="2.4" />
      </button>
      <span class="sep" aria-hidden="true">/</span>
      <button class="crumb" :class="{ on: scopeOpen('worktree') }" @click="herd.openOverlay('scope', 'worktree')">
        <Icon name="folder" />{{ t.worktreeRel }}
      </button>
      <span class="sep" aria-hidden="true">/</span>
      <button class="crumb" :class="{ on: scopeOpen('branch') }" @click="herd.openOverlay('scope', 'branch')">
        <Icon name="branch" />{{ t.branch }}
      </button>
    </template>
    <span v-else-if="herd.view === 'settings'" class="muted label">Settings</span>
    <span class="grow"></span>
    <button class="search" @click="herd.openOverlay('palette')">
      <Icon name="search" /><span class="grow">Search tasks</span><Kbd action="palette" />
    </button>
    <button class="new" @click="herd.openOverlay('new')">
      <Icon name="plus" :size="12" :stroke="2.2" />New task<Kbd action="newTask" />
    </button>
    <div v-if="controls" class="win" aria-label="Window">
      <button class="wc" aria-label="Minimise" title="Minimise" @click="minimise()">
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M1 5.5h8" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
      <button class="wc" :aria-label="maximised ? 'Restore' : 'Maximise'" :title="maximised ? 'Restore' : 'Maximise'" @click="toggleMaximise()">
        <svg v-if="maximised" width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true"><rect x="1" y="3" width="6" height="6" /><path d="M3 3V1h6v6H7" /></svg>
        <svg v-else width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true"><rect x="1.5" y="1.5" width="7" height="7" /></svg>
      </button>
      <button class="wc close" aria-label="Close" title="Close" @click="quit()">
        <svg width="10" height="10" viewBox="0 0 10 10" stroke="currentColor" stroke-width="1.2" aria-hidden="true"><path d="M1.5 1.5l7 7M8.5 1.5l-7 7" /></svg>
      </button>
    </div>
  </header>
</template>

<style scoped>
header {
  --wails-draggable: drag;
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
  height: 38px;
  padding: 0 8px;
  background: var(--chrome);
  border-bottom: 1px solid var(--border);
  user-select: none;
}
header.mac {
  padding-left: 78px;
}
header:not(.mac) {
  padding-right: 4px;
}
button {
  --wails-draggable: no-drag;
}
.mark {
  padding: 0 10px 0 6px;
  font-weight: 600;
}
.crumb {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 8px;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
  font-size: 13px;
  white-space: nowrap;
}
.crumb.project {
  color: var(--text);
  font-weight: 500;
}
.crumb:hover,
.crumb.on {
  background: var(--hover);
  color: var(--text);
}
.sep {
  color: var(--faint);
}
.label {
  padding: 0 6px;
}
.grow {
  flex: 1;
}
.search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 26px;
  min-width: 220px;
  padding: 0 6px 0 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--muted);
  text-align: left;
}
.new {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 6px 0 10px;
  margin-left: 6px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
}
.gear {
  width: 28px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
}
.gear.on,
.gear:hover {
  color: var(--text);
  background: var(--hover);
}
.win {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-left: 10px;
  padding-left: 8px;
  border-left: 1px solid var(--border);
  height: 20px;
}
.wc {
  width: 30px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--muted);
}
.wc:hover {
  background: var(--hover);
  color: var(--text);
}
.wc.close:hover {
  background: var(--approval-bg);
  color: var(--approval);
}
</style>
