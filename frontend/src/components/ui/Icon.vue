<script setup lang="ts">
// Stroke icons from the UI preview.
withDefaults(defineProps<{ name: string; size?: number; stroke?: number }>(), { size: 13, stroke: 2 })
</script>

<template>
  <svg v-if="name === 'play'" :width="size" :height="size" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 4l13 8-13 8z" /></svg>
  <svg v-else :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" :stroke-width="stroke" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <template v-if="name === 'logo'"><path d="M4 6h10" /><path d="M4 12h16" /><path d="M4 18h7" /><circle cx="17" cy="6" r="2" /><circle cx="14" cy="18" r="2" /></template>
    <path v-else-if="name === 'chevron'" d="M6 9l6 6 6-6" />
    <path v-else-if="name === 'chevron-right'" d="M9 6l6 6-6 6" />
    <path v-else-if="name === 'folder'" d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    <template v-else-if="name === 'branch'"><circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="7" r="2" /><path d="M6 7v10" /><path d="M18 9c0 5-6 4-12 8" /></template>
    <template v-else-if="name === 'search'"><circle cx="11" cy="11" r="6" /><path d="M20 20l-4-4" /></template>
    <template v-else-if="name === 'plus'"><path d="M12 5v14" /><path d="M5 12h14" /></template>
    <template v-else-if="name === 'close'"><path d="M6 6l12 12" /><path d="M18 6L6 18" /></template>
    <template v-else-if="name === 'external'"><path d="M14 4h6v6" /><path d="M20 4l-9 9" /><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" /></template>
    <template v-else-if="name === 'file'"><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" /><path d="M14 3v6h6" /></template>
    <template v-else-if="name === 'terminal'"><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M7 9l3 3-3 3" /><path d="M12 15h5" /></template>
    <template v-else-if="name === 'archive'"><rect x="3" y="4" width="18" height="4" rx="1" /><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" /><path d="M10 12h4" /></template>
    <path v-else-if="name === 'check'" d="M5 12l5 5 9-10" />
    <template v-else-if="name === 'warn'"><path d="M12 4l9 16H3z" /><path d="M12 10v4" /><path d="M12 17h.01" /></template>
    <template v-else-if="name === 'gear'"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></template>
  </svg>
</template>
