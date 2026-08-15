import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import 'vuetify/styles'
import '@mdi/font/css/materialdesignicons.css'
import App from './App.vue'
import DeviceGrid from './views/DeviceGrid.vue'
import DeviceConsole from './views/DeviceConsole.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: DeviceGrid },
    { path: '/devices/:id', component: DeviceConsole, props: true },
  ],
})

createApp(App)
  .use(createPinia())
  .use(router)
  .use(createVuetify({ components, directives }))
  .mount('#app')
