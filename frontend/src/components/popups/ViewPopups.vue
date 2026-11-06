<script setup lang="ts">
// The artifact and changes pop-ups: one alone is large, both open side by
// side with half the width each. A click around them closes both.
import { herd } from '../../lib/state'
import ArtifactPopup from './ArtifactPopup.vue'
import DiffPopup from './DiffPopup.vue'
import DiagramPopup from './DiagramPopup.vue'
// A diagram opened full size covers them; closing it goes back to them.
</script>

<template>
  <button class="scrim dim" aria-label="Close" @click="herd.closeViews()"></button>
  <div v-if="herd.diagramOf" class="pair">
    <DiagramPopup />
  </div>
  <div v-else class="pair">
    <ArtifactPopup v-if="herd.artifactOf" />
    <DiffPopup v-if="herd.diffOf" />
  </div>
</template>

<style scoped>
  .pair {
    position: absolute;
    z-index: 21;
    top: 48px;
    bottom: 40px;
    left: 24px;
    right: 24px;
    display: flex;
    justify-content: center;
    gap: 16px;
    pointer-events: none; /* the gaps let clicks reach the scrim */
  }
  .pair > * {
    pointer-events: auto;
  }
</style>
