<script setup lang="ts">
// Push: the pull request title, "fix(vehicles): take a vehicle out of QA #9726",
// chosen before the push step runs.
import { computed, onMounted, ref } from 'vue'
import { herd } from '../../lib/state'
import { isMac } from '../../lib/keymap'
import type { PRKind } from '../../lib/types'
import Icon from '../ui/Icon.vue'

const kinds: PRKind[] = ['feat', 'fix', 'perf', 'test']
const t = computed(() => herd.task(herd.pushOpt?.task ?? ''))

// The description defaults to the branch without its prefix, dashes as spaces.
const defaultDesc = computed(() => {
  const branch = t.value?.branch ?? ''
  const prefix = herd.settings?.config.branchPrefix ?? ''
  let b = branch.startsWith(prefix) ? branch.slice(prefix.length) : branch
  if (b === branch && b.includes('/')) b = b.slice(b.lastIndexOf('/') + 1)
  return b.replace(/[-_]+/g, ' ').trim()
})

const last = t.value?.prTitle
const kind = ref<PRKind>(last?.kind ?? 'feat')
const scope = ref(last?.scope ?? '')
const desc = ref(last?.desc || defaultDesc.value)
const busy = ref(false)
const error = ref('')
const scopeInput = ref<HTMLInputElement>()
onMounted(() => scopeInput.value?.focus())

const nScope = computed(() => scope.value.trim())
const nDesc = computed(() => desc.value.trim().replace(/\s+/g, ' ') || defaultDesc.value)
const scopeOk = computed(() => /^[A-Za-z0-9._/-]+$/.test(nScope.value))
const preview = computed(() => `${kind.value}(${nScope.value || '…'}): ${nDesc.value}${t.value?.issue ? ' #' + t.value.issue : ''}`)

async function push() {
  if (!scopeOk.value || busy.value) return
  busy.value = true
  error.value = await herd.startPush({ kind: kind.value, scope: nScope.value, desc: nDesc.value })
  busy.value = false
}
function cancel() {
  herd.pushOpt = null
  herd.overlay = 'none'
}
function key(e: KeyboardEvent) {
  if (e.key === 'Enter' && (isMac() ? e.metaKey : e.ctrlKey || e.target instanceof HTMLInputElement)) {
    e.preventDefault()
    void push()
  }
}
</script>

<template>
  <button class="scrim dim" aria-label="Cancel push" @click="cancel"></button>
  <div class="box popup" role="dialog" aria-label="Pull request title" tabindex="-1" @keydown="key">
    <div class="head">
      <span class="title">{{ herd.pushOpt?.label ?? 'Push' }}: pull request title</span>
    </div>
    <div class="body">
      <div class="group">
        <span class="lbl">Type</span>
        <div class="pill kinds" role="radiogroup" aria-label="Type">
          <button v-for="k in kinds" :key="k" role="radio" :aria-checked="kind === k" :class="{ on: kind === k }" @click="kind = k">{{ k }}</button>
        </div>
      </div>
      <div class="group">
        <label for="prscope" class="lbl">Scope <span class="faint">the main part of the code it changes</span></label>
        <input id="prscope" ref="scopeInput" v-model="scope" class="input mono" placeholder="vehicles" autocomplete="off" spellcheck="false" />
        <span v-if="nScope && !scopeOk" class="err">One word: letters, digits, . _ / -</span>
      </div>
      <div class="group">
        <label for="prdesc" class="lbl">Description <span class="faint">optional</span></label>
        <input id="prdesc" v-model="desc" class="input" :placeholder="defaultDesc" autocomplete="off" />
        <span class="faint small">Empty uses the branch name: {{ defaultDesc }}</span>
      </div>
      <div class="group">
        <span class="lbl">Pull request</span>
        <div class="preview mono">{{ preview }}</div>
      </div>
      <p v-if="error" class="err">{{ error }}</p>
    </div>
    <div class="foot">
      <span class="muted small">Pushes {{ t?.branch }} and opens the PR on {{ t?.baseBranch }}</span>
      <span class="grow"></span>
      <button class="btn ghost" @click="cancel">Cancel</button>
      <button class="btn primary" :disabled="!scopeOk || busy" @click="push"><Icon name="play" :size="13" />Push</button>
    </div>
  </div>
</template>

<style scoped>
  .box {
    position: absolute;
    z-index: 21;
    top: 64px;
    left: 50%;
    transform: translateX(-50%);
    width: 560px;
    max-width: calc(100% - 32px);
    display: flex;
    flex-direction: column;
    outline: none;
  }
  .head {
    display: flex;
    align-items: center;
    height: 40px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    font-weight: 600;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 14px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: 12px;
    color: var(--muted);
  }
  .faint {
    color: var(--faint);
  }
  .kinds {
    width: 280px;
    height: 28px;
  }
  .kinds > button {
    font-family: var(--mono);
  }
  .input {
    height: 30px;
  }
  .preview {
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--surface);
    font-size: 12.5px;
    line-height: 1.5;
    word-break: break-word;
  }
  .err {
    margin: 0;
    font-size: 12px;
    color: var(--approval);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-top: 1px solid var(--border);
  }
  .grow {
    flex: 1;
  }
  .small {
    font-size: 12px;
  }
</style>
