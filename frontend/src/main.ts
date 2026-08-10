import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from '@/App.vue'
import router from '@/router'
import { initializeI18n } from '@/i18n'
import './assets/main.css'

async function bootstrap() {
  await initializeI18n()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.mount('#app')
  void router.isReady()
    .then(() => import('@/telemetry/webVitals'))
    .then(({ startWebVitalsReportingAfterRouterReady }) => startWebVitalsReportingAfterRouterReady(router))
    .catch(() => undefined)
}

void bootstrap()
