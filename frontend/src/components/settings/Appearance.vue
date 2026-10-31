<script setup lang="ts">
// Settings > Appearance: Dark (Fleet Dark), Light (Flatwhite) or System,
// then fonts and the Zed command.
import { computed } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'

type Mode = 'dark' | 'light' | 'system'
const ap = computed(() => herd.settings?.config.appearance)
const cards = computed<{ id: Mode; name: string; note: string; panes: string[] }[]>(() => [
  { id: 'dark', name: 'Dark', note: 'Fleet Dark', panes: ['fleet-dark'] },
  { id: 'light', name: 'Light', note: 'Flatwhite', panes: ['flatwhite'] },
  { id: 'system', name: 'System', note: `now ${herd.systemLight ? 'Light' : 'Dark'}, follows the OS`, panes: ['fleet-dark', 'flatwhite'] },
])

async function pick(id: Mode) {
  if (!herd.settings) return
  herd.settings.config.appearance.theme = id
  herd.edited()
  await herd.attempt('Could not save the theme', () => api.SetTheme(id))
}

const save = debounce(() => {
  const s = herd.settings
  if (!s) return
  const c = JSON.parse(JSON.stringify(s.config))
  void herd.attempt('Could not save appearance', () => api.SaveAppearance(c.appearance, c.zedCommand))
}, 600)

function setFont(kind: 'ui' | 'term', e: Event) {
  const a = herd.settings?.config.appearance
  if (!a) return
  const v = (e.target as HTMLInputElement).value.trim()
  const m = /^(.*?)\s+([\d.]+)$/.exec(v)
  const name = m ? m[1] : v
  const size = m ? Number(m[2]) : kind === 'ui' ? a.uiFontSize : a.termFontSize
  if (!name) return
  if (kind === 'ui') {
    a.uiFont = name
    a.uiFontSize = size
  } else {
    a.termFont = name
    a.termFontSize = size
  }
  herd.edited()
  save()
}
function setZed(e: Event) {
  if (!herd.settings) return
  herd.settings.config.zedCommand = (e.target as HTMLInputElement).value
  herd.edited()
  save()
}
</script>

<template>
  <div class="intro">
    <h1>Appearance</h1>
    <p>Dark uses Fleet Dark and Light uses Flatwhite, the themes you use in Zed. System follows your OS setting.</p>
  </div>

  <div class="cards">
    <button v-for="c in cards" :key="c.id" class="tcard" :class="{ on: herd.themeMode === c.id }" @click="pick(c.id)">
      <span class="shot">
        <span v-for="th in c.panes" :key="th" class="pane" :data-theme="th">
          <span class="pchrome"></span>
          <span class="pbody">
            <span class="pside">
              <span class="bar" style="width: 80%; background: var(--muted)"></span>
              <span class="bar" style="width: 55%; background: var(--running-dot)"></span>
              <span class="bar" style="width: 70%; background: var(--muted)"></span>
              <span class="bar" style="width: 50%; background: var(--waiting-dot)"></span>
            </span>
            <span class="pmain">
              <span class="bar" style="width: 60%; background: var(--faint)"></span>
              <span class="blk" style="width: 85%; background: var(--add-bg)"></span>
              <span class="blk" style="width: 70%; background: var(--del-bg)"></span>
              <span class="bar" style="width: 40%; background: var(--accent)"></span>
            </span>
          </span>
        </span>
      </span>
      <span class="label">
        <span class="radio" :class="{ on: herd.themeMode === c.id }"></span>
        <span class="name">{{ c.name }}</span>
        <span class="muted small">{{ c.note }}</span>
      </span>
    </button>
  </div>

  <div v-if="ap" class="rows card">
    <label class="r">
      <span class="grow">UI font</span>
      <input class="val" :value="`${ap.uiFont} ${ap.uiFontSize}`" @change="setFont('ui', $event)" />
    </label>
    <label class="r">
      <span class="grow">Terminal font</span>
      <input class="val" :value="`${ap.termFont} ${ap.termFontSize}`" @change="setFont('term', $event)" />
    </label>
    <label class="r">
      <span class="grow">Zed command</span>
      <input class="val" :value="herd.settings?.config.zedCommand" @change="setZed" />
    </label>
  </div>
</template>

<style scoped>
  .cards {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 14px;
    max-width: 900px;
  }
  .tcard {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px;
    border-radius: 8px;
    text-align: left;
    font-size: 13px;
    color: var(--text);
    border: 1px solid var(--border);
    background: transparent;
  }
  .tcard.on {
    background: var(--sel);
  }
  .shot {
    display: flex;
    height: 130px;
    border-radius: 6px;
    overflow: hidden;
    border: 1px solid var(--border);
  }
  .pane {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg);
  }
  .pchrome {
    height: 16px;
    background: var(--chrome);
    border-bottom: 1px solid var(--border);
  }
  .pbody {
    flex: 1;
    display: flex;
  }
  .pside {
    width: 34%;
    border-right: 1px solid var(--border);
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .pmain {
    flex: 1;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .bar {
    height: 6px;
    border-radius: 3px;
  }
  .blk {
    height: 8px;
    border-radius: 2px;
  }
  .label {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .radio {
    flex: none;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 1px solid var(--muted);
  }
  .radio.on {
    border: 4px solid var(--text);
  }
  .name {
    font-weight: 600;
  }
  .small {
    font-size: 12px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    max-width: 720px;
  }
  .r {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
  }
  .r + .r {
    border-top: 1px solid var(--border);
  }
  .grow {
    flex: 1;
  }
  .val {
    font-family: var(--mono);
    font-size: 12.5px;
    padding: 3px 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    text-align: right;
    width: 220px;
  }
  .val:focus {
    outline: none;
  }
</style>
