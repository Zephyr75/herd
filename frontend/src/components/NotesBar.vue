<script setup lang="ts">
// The user's comments on the task's diff, waiting to be sent: Send to
// <agent> starts that agent with them as guidance, then clears them.
import { computed, ref } from 'vue'
import { herd } from '../lib/state'
import type { Agent, Note, Task } from '../lib/types'
import Icon from './ui/Icon.vue'

const props = defineProps<{ task: Task }>()
const n = computed(() => props.task.notes.length)
const confirmClear = ref(false)

const span = (x: Note) => (x.from === x.to ? `line ${x.to}` : `lines ${x.from}–${x.to}`) + (x.side === 'old' ? ' (removed lines)' : '')
async function send(agent: Agent) {
  const comments = props.task.notes.map((x) => `### ${x.path}, ${span(x)}\n\`\`\`diff\n${x.code}\n\`\`\`\n${x.text}`).join('\n\n')
  if (await herd.fixWith(props.task, herd.prompt('diffNotes', { comments }), agent)) await herd.setNotes(props.task, [])
}
function clear() {
  if (!confirmClear.value) return void (confirmClear.value = true)
  confirmClear.value = false
  void herd.setNotes(props.task, [])
}
</script>

<template>
  <div v-if="n" class="notes-bar">
    <span class="count"><Icon name="pencil" :size="12" />{{ n }} {{ n > 1 ? 'comments' : 'comment' }} on the diff</span>
    <span class="grow"></span>
    <template v-if="herd.canFix(task)">
      <button v-for="a in herd.fixers()" :key="a.id" class="b" :title="`Start a ${a.name} step with your comments as guidance; they are cleared once it starts`" @click="send(a)">Send to {{ a.name }}</button>
    </template>
    <span v-else class="muted">a step is running</span>
    <button class="b ghost" :class="{ danger: confirmClear }" @blur="confirmClear = false" @click="clear">{{ confirmClear ? 'Delete them?' : 'Clear' }}</button>
  </div>
</template>

<style scoped>
  .notes-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: 4px;
    background: var(--surface);
    font-size: 12px;
  }
  .count {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .grow {
    flex: 1;
  }
  .b {
    height: 24px;
    padding: 0 9px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 12px;
  }
  .b:hover {
    background: var(--hover);
  }
  .b.ghost {
    background: transparent;
  }
  .b.danger {
    color: var(--approval);
  }
</style>
