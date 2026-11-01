import type { Usage } from './types'

/** 1234 → 1.2k, 2_400_000 → 2.4M */
export function fmtTokens(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + 'k'
  return String(n)
}

export const tokensOf = (u?: Usage) => (u ? u.input + u.output + u.cacheRead + u.cacheWrite : 0)

/** Short label: "182k tok", or "" for no usage. */
export function usageLabel(u?: Usage): string {
  const t = tokensOf(u)
  return t ? fmtTokens(t) + ' tok' : ''
}

/** Tooltip with the breakdown. */
export function usageTitle(u?: Usage): string {
  if (!u || !tokensOf(u)) return ''
  return `Input ${fmtTokens(u.input)} · output ${fmtTokens(u.output)} · cache read ${fmtTokens(u.cacheRead)} · cache write ${fmtTokens(u.cacheWrite)}`
}
