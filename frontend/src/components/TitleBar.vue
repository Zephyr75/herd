<script setup lang="ts">
// herd's top bar is the window's titlebar: drag it to move the window,
// double-click to maximise. On Linux and Windows the window is frameless and
// the controls are drawn here; macOS keeps its traffic lights.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { herd } from '../lib/state'
import { isMaximised, minimise, quit, toggleMaximise } from '../lib/window'
import Icon from './ui/Icon.vue'

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
    <button class="menu" :class="{ on: herd.view === 'settings' }" aria-label="Settings" title="Settings" @click="herd.command('settings')">
      <Icon name="menu" :size="15" />
    </button>
    <template v-if="herd.view === 'board' && t">
      <button class="crumb project" :class="{ on: scopeOpen('project') }" @click="herd.openOverlay('scope', 'project')">
        {{ t.projectName }}<Icon name="chevron" :size="13" />
      </button>
      <span class="sep" aria-hidden="true">/</span>
      <button class="crumb" :class="{ on: scopeOpen('worktree') }" @click="herd.openOverlay('scope', 'worktree')">
        <Icon name="worktree" /><span class="ct">{{ t.worktreeRel }}</span>
      </button>
      <span class="sep" aria-hidden="true">/</span>
      <button class="crumb" :class="{ on: scopeOpen('branch') }" @click="herd.openOverlay('scope', 'branch')">
        <Icon name="branch" /><span class="ct">{{ t.branch }}</span>
      </button>
    </template>
    <span v-else-if="herd.view === 'settings'" class="muted label">Settings</span>
    <span class="grow"></span>
    <button class="search" @click="herd.openOverlay('palette')">
      <Icon name="search" /><span class="grow stext">Search tasks</span>
    </button>
    <button class="new" @click="herd.openOverlay('new')">
      <Icon name="plus" :size="14" />New task
    </button>
    <div v-if="controls" class="win" aria-label="Window">
      <button class="wc" aria-label="Minimise" title="Minimise" @click="minimise()">
        <Icon name="win-min" :size="16" />
      </button>
      <button class="wc" :aria-label="maximised ? 'Restore' : 'Maximise'" :title="maximised ? 'Restore' : 'Maximise'" @click="toggleMaximise()">
        <Icon v-if="maximised" name="win-restore" :size="16" />
        <Icon v-else name="win-max" :size="16" />
      </button>
      <button class="wc close" aria-label="Close" title="Close" @click="quit()">
        <Icon name="win-close" :size="16" />
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
  height: 34px;
  padding: 0 6px;
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
.crumb {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 8px;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
  font-size: 13px;
  white-space: nowrap;
  min-width: 0; /* long worktree and branch names shrink first */
  overflow: hidden;
  text-overflow: ellipsis;
}
.crumb > :deep(svg) {
  flex: none;
}
.ct {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.crumb.project {
  flex: none;
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
.stext {
  overflow: hidden;
  text-overflow: ellipsis;
}
.search > :deep(svg) {
  flex: none;
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
  height: 24px;
  flex: 0 1 130px;
  min-width: 34px; /* the icon alone when space is short */
  overflow: hidden;
  white-space: nowrap;
  padding: 0 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--muted);
  text-align: left;
}
.new {
  flex: none;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 10px;
  margin-left: 6px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
}
.menu {
  width: 28px;
  height: 24px;
  margin-right: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
}
.menu.on,
.menu:hover {
  color: var(--text);
  background: var(--hover);
}
.win {
  flex: none;
  display: flex;
  align-items: center;
  gap: 2px;
  margin-left: 6px;
  height: 20px;
}
.wc {
  width: 30px;
  height: 24px;
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
