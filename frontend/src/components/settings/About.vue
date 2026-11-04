<script setup lang="ts">
import { api } from '../../lib/api'
import { herd } from '../../lib/state'
import Logo from '../ui/Logo.vue'

const repo = 'https://github.com/Zephyr75/herd'
const open = () => herd.attempt('Could not open the link', () => api.OpenURL(repo))
</script>

<template>
  <div class="hero card">
    <Logo :size="44" :class="herd.theme === 'fleet-dark' ? 'on-dark' : 'on-light'" />
    <div class="id">
      <h1>herd</h1>
      <p class="muted">Runs Claude Code through a pipeline of agents (Plan → Implement → Review → Test → Commit → Push), each task in its own git worktree.</p>
    </div>
  </div>

  <div class="table card">
    <div class="r">
      <span class="grow">Source code</span>
      <button class="link" @click="open">github.com/Zephyr75/herd</button>
    </div>
    <div class="r">
      <span class="grow">Settings, tasks and runs</span>
      <code class="muted">{{ herd.settings?.dir ?? '~/.herd' }}</code>
    </div>
    <div class="r">
      <span class="grow">License</span>
      <span class="muted">MIT</span>
    </div>
  </div>
</template>

<style scoped>
  .hero,
  .table {
    max-width: 720px;
  }
  .hero {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px 18px;
  }
  .id {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .id p {
    margin: 0;
    line-height: 1.5;
  }
  .on-dark {
    color: #fff;
  }
  .on-light {
    color: #000;
  }
  .table {
    display: flex;
    flex-direction: column;
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
  code {
    font-family: var(--mono);
    font-size: 12px;
  }
  .link {
    border: 0;
    background: none;
    padding: 0;
    color: var(--accent);
    font-size: 13px;
  }
  .link:hover {
    text-decoration: underline;
  }
</style>
