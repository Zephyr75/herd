<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../../lib/state'
import { keyLabel } from '../../lib/keymap'
// Shows an action's first binding from keymap.json, or literal text.
const props = defineProps<{ action?: string; text?: string }>()
const shown = computed(() => props.text || keyLabel(herd.settings?.keymap, props.action ?? ''))
</script>

<template>
  <span v-if="shown" class="kbd">{{ shown }}</span>
</template>
