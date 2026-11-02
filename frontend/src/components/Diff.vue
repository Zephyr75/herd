<script setup lang="ts">
// A read-only unified diff: hunks with old and new line numbers.
import { computed } from 'vue'

const props = defineProps<{ diff: string }>()

interface Row {
  kind: 'hunk' | 'add' | 'del' | 'ctx' | 'note'
  old: number | null
  new: number | null
  text: string
}

const rows = computed(() => {
  const out: Row[] = []
  let o = 0
  let n = 0
  let inHunk = false
  for (const line of props.diff.split('\n')) {
    const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/.exec(line)
    if (m) {
      o = Number(m[1])
      n = Number(m[2])
      inHunk = true
      out.push({ kind: 'hunk', old: null, new: null, text: m[3].trim() })
      continue
    }
    if (!inHunk) continue // diff --git, index, ---, +++ headers
    if (line.startsWith('+')) out.push({ kind: 'add', old: null, new: n++, text: line.slice(1) })
    else if (line.startsWith('-')) out.push({ kind: 'del', old: o++, new: null, text: line.slice(1) })
    else if (line.startsWith('\\')) out.push({ kind: 'note', old: null, new: null, text: line.slice(2) })
    else if (line.startsWith('diff --git')) inHunk = false
    else out.push({ kind: 'ctx', old: o++, new: n++, text: line.slice(1) })
  }
  return out
})
</script>

<template>
  <div class="diff">
    <table>
      <tbody>
        <tr v-for="(r, i) in rows" :key="i" :class="r.kind">
          <template v-if="r.kind === 'hunk'">
            <td class="ln" colspan="2">⋯</td>
            <td class="code">{{ r.text }}</td>
          </template>
          <template v-else>
            <td class="ln">{{ r.old ?? '' }}</td>
            <td class="ln">{{ r.new ?? '' }}</td>
            <td class="code"><span class="sign">{{ r.kind === 'add' ? '+' : r.kind === 'del' ? '−' : ' ' }}</span>{{ r.text }}</td>
          </template>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.diff {
  width: max-content; /* the scrolling parent scrolls long lines sideways */
  min-width: 100%;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.55;
  tab-size: 4;
}
table {
  border-collapse: collapse;
  min-width: 100%;
}
td {
  padding: 0;
  vertical-align: top;
}
.ln {
  width: 1%;
  padding: 0 6px;
  text-align: right;
  color: var(--faint);
  user-select: none;
  white-space: nowrap;
}
.code {
  padding: 0 12px 0 4px;
  white-space: pre;
}
.sign {
  display: inline-block;
  width: 14px;
  color: var(--muted);
  user-select: none;
}
.add {
  background: var(--add-bg);
}
.add .sign {
  color: var(--add);
}
.del {
  background: var(--del-bg);
}
.del .sign {
  color: var(--del);
}
.hunk td {
  padding-top: 4px;
  padding-bottom: 4px;
  background: var(--surface);
  color: var(--muted);
}
.note .code {
  color: var(--muted);
  font-style: italic;
}
</style>
