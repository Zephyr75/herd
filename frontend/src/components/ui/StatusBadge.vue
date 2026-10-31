<script setup lang="ts">
import type { StatusKind } from '../../lib/types'
defineProps<{ kind: StatusKind; label: string }>()
</script>

<template>
  <span :class="['badge', `s-${kind}`]"><span class="dot"></span>{{ label }}</span>
</template>
