// A pop-up's side bar: its width and whether it is hidden, remembered per
// pop-up in localStorage (a per-viewer convenience; defaults without it).
import { ref, watch } from 'vue'

export const SIDEBAR = { min: 160, max: 560 }

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function useSidebar(name: string, def: number) {
  const key = 'herd.popup.' + name
  const w = Number(read(key + '.width'))
  const width = ref(w >= SIDEBAR.min && w <= SIDEBAR.max ? w : def)
  const hidden = ref(read(key + '.hidden') === '1')
  watch([width, hidden], () => {
    try {
      localStorage.setItem(key + '.width', String(width.value))
      localStorage.setItem(key + '.hidden', hidden.value ? '1' : '0')
    } catch {
      /* storage unavailable */
    }
  })
  return { width, hidden, def }
}
