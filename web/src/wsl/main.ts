import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import App from './App.vue'
import './theme/lumenwave/system.css'
import './style.css'
import './theme/product.css'

createApp(App).use(createPinia()).use(ElementPlus, { locale: zhCn }).mount('#app')
