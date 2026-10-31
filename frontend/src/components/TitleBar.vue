<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import Kbd from './ui/Kbd.vue'

const t = computed(() => herd.selTask)
const scopeOpen = (m: string) => herd.overlay === 'scope' && herd.scope === m
</script>

<template>
  <header :class="{ mac: herd.os === 'darwin' }">
    <span class="mark"><span class="logo"><Icon name="logo" :size="16" /></span>herd</span>
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
    <button class="gear" :class="{ on: herd.view === 'settings' }" aria-label="Settings" @click="herd.command('settings')">
      <Icon name="gear" :size="15" />
    </button>
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
button {
  --wails-draggable: no-drag;
}
.mark {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px 0 4px;
  font-weight: 600;
}
.logo {
  display: flex;
  color: var(--accent);
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
  margin-left: 4px;
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
</style>
