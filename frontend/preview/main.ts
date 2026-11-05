import { createApp } from 'vue'
import { initializeI18n } from '@/i18n'
import App from './App.vue'
import '@/assets/main.css'

void initializeI18n().then(() => createApp(App).mount('#app'))
