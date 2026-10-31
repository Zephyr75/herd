// Artifact preview: markdown-it, with ```mermaid fences rendered by mermaid.

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

function firstLine(s: string) {
  return s.trim().split('\n')[0] ?? ''
}

// Links open outside the app.
md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noreferrer')
  return self.renderToken(tokens, idx, options)
}

export function render(src: string): string {
  return md.render(src)
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
