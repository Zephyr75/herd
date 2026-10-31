import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '@xterm/xterm/css/xterm.css'
import './themes/fleet-dark.css'
import './themes/flatwhite.css'
import './themes/base.css'
import { createApp } from 'vue'
import App from './App.vue'
import { herd } from './lib/state'

void herd.init()
createApp(App).mount('#app')
