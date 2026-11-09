<script setup lang="ts">
// "#4356" or "LOCAL", in mono and gray. With task, a task that has sister
// tasks shows it in its sister group's color instead.
import { computed } from 'vue'
import { groupOf, sisterTitle } from '../../lib/sisters'

const props = defineProps<{ issue: string; width?: string; task?: string }>()
const g = computed(() => (props.task ? groupOf.value.get(props.task) : undefined))
</script>

<template>
  <span :class="['issue-ref', g ? 'sis sis-' + g.color : '']" :style="width ? { width } : undefined" :title="task ? sisterTitle(task) : ''">{{ issue ? '#' + issue : 'LOCAL' }}</span>
</template>
