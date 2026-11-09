<script setup lang="ts">
// herd's icons, from Lucide (lucide.dev), by the names herd uses.
import { computed, type Component } from 'vue'
import {
  Archive, Check, ChevronDown, ChevronRight, Copy, Diff, ExternalLink, Eye, File, Folder, FolderGit2, GitBranch, GitPullRequest, Maximize2, Menu, Minus,
  PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Pencil, Play, Plus, RotateCw, Search, Settings, Square, SquareTerminal, TriangleAlert, Undo2, X,
} from '@lucide/vue'

const props = withDefaults(defineProps<{ name: string; size?: number; stroke?: number }>(), { size: 14, stroke: 2 })

const icons: Record<string, Component> = {
  play: Play,
  stop: Square,
  chevron: ChevronDown,
  'chevron-right': ChevronRight,
  folder: Folder,
  branch: GitBranch,
  search: Search,
  plus: Plus,
  close: X,
  external: ExternalLink,
  file: File,
  terminal: SquareTerminal,
  archive: Archive,
  eye: Eye,
  diff: Diff,
  revert: Undo2,
  pr: GitPullRequest,
  rerun: RotateCw,
  pencil: Pencil,
  check: Check,
  warn: TriangleAlert,
  copy: Copy,
  worktree: FolderGit2,
  maximize: Maximize2,
  menu: Menu,
  gear: Settings,
  // window controls
  'win-min': Minus,
  'win-max': Square,
  'win-restore': Copy,
  'win-close': X,
  // side panels: shown (click hides) or hidden (click shows)
  'panel-left': PanelLeftClose,
  'panel-left-off': PanelLeftOpen,
  'panel-right': PanelRightClose,
  'panel-right-off': PanelRightOpen,
}
const filled = ['play', 'stop'] // solid, like media buttons

const icon = computed(() => icons[props.name])
</script>

<template>
  <component :is="icon" v-if="icon" :size="size" :stroke-width="stroke" :fill="filled.includes(name) ? 'currentColor' : 'none'" aria-hidden="true" />
</template>
