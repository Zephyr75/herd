// Artifact preview: markdown-it, with ```mermaid fences rendered by mermaid
// and diagram pages (![title](PLAN.html), drawn with archify) embedded.

import MarkdownIt from 'markdown-it'

const md = new MarkdownIt({ html: false, linkify: true, typographer: false })

const defaultFence = md.renderer.rules.fence!
md.renderer.rules.fence = (tokens, idx, options, env, self) => {
  const tok = tokens[idx]
  if (tok.info.trim().split(/\s+/)[0] === 'mermaid') {
    return `<figure class="mermaid-fig"><pre class="mermaid">${md.utils.escapeHtml(tok.content)}</pre><figcaption>mermaid · ${md.utils.escapeHtml(firstLine(tok.content))}</figcaption></figure>`
  }
  return defaultFence(tokens, idx, options, env, self)
}

// A diagram page in the run directory: embedded, sandboxed (no access to the
// app), with a button to open it full size.
const defaultImage = md.renderer.rules.image!
md.renderer.rules.image = (tokens, idx, options, env, self) => {
  const tok = tokens[idx]
  const file = String(tok.attrGet('src') ?? '')
  const e = env as RenderEnv
  if (!e?.task || !/^[\w.-]+\.html$/.test(file) || file.startsWith('.')) return defaultImage(tokens, idx, options, env, self)
  const title = md.utils.escapeHtml(tok.content || file)
  const f = md.utils.escapeHtml(file)
  return (
    `<figure class="diagram-fig"><iframe src="${md.utils.escapeHtml(diagramURL(e.task, file, !!e.dark, true) + '&v=' + (e.rev ?? 0))}" title="${title}" sandbox="allow-scripts" loading="lazy"></iframe>` +
    `<figcaption><span>${title}</span><button type="button" data-diagram="${f}" title="Open ${f} full size">Open full size</button></figcaption></figure>`
  )
}

/** diagramURL is where herd serves a task's diagram page to the webview. */
export function diagramURL(task: string, file: string, dark: boolean, embed = false) {
  return `/run/${encodeURIComponent(task)}/${encodeURIComponent(file)}?theme=${dark ? 'dark' : 'light'}${embed ? '&embed=1' : ''}`
}

export interface RenderEnv {
  task?: string
  dark?: boolean
  rev?: number // bumped when a diagram page changes, to reload it
  [k: string]: unknown
}

function firstLine(s: string) {
  return s.trim().split('\n')[0] ?? ''
}

// Links open outside the app.
md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noreferrer')
  return self.renderToken(tokens, idx, options)
}

export function render(src: string, env: RenderEnv = {}): string {
  return md.render(src, { ...env })
}

let mermaidMod: typeof import('mermaid').default | null = null

function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

/** renderMermaid renders every pending mermaid block inside root. */
export async function renderMermaid(root: HTMLElement, dark: boolean) {
  const nodes = root.querySelectorAll<HTMLElement>('pre.mermaid:not([data-processed])')
  if (!nodes.length) return
  mermaidMod ??= (await import('mermaid')).default
  mermaidMod.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'base',
    fontFamily: 'IBM Plex Sans, system-ui, sans-serif',
    themeVariables: {
      darkMode: dark,
      background: token('--surface'),
      primaryColor: token('--bg'),
      primaryTextColor: token('--text'),
      primaryBorderColor: token('--border'),
      lineColor: token('--faint'),
      secondaryColor: token('--surface'),
      tertiaryColor: token('--surface'),
      fontSize: '12.5px',
    },
  })
  try {
    await mermaidMod.run({ nodes: Array.from(nodes) })
  } catch {
    // A bad diagram stays as its source text.
  }
}
