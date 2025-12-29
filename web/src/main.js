import { createApp } from 'vue'
import App from './App.vue'

// 引入 Element Plus
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 引入暗黑模式变量
import 'element-plus/theme-chalk/dark/css-vars.css'
// 引入图标
import * as ElementPlusIconsVue from '@element-plus/icons-vue'

// 引入 Axios 和 Auth 工具 (用于应用启动前的隐式登录)
import axios from 'axios'
import { setToken } from './utils/auth'

const app = createApp(App)

// 注册所有图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

app.use(ElementPlus)

// 定义初始化函数
// 目的：在渲染页面前获取 Token，防止页面加载后的第一个请求因为无 Token 报 401
const initApp = async () => {
  try {
    // 1. 发起自动登录 (用户名密码目前硬编码在前端，模拟隐式登录)
    // 注意：这里使用原生 axios 而不是封装的 request.js，避免拦截器逻辑干扰初始化
    const res = await axios.post('/api/login', {
      username: 'admin',
      password: '123456'
    })

    // 2. 处理响应
    // 后端返回结构: { code: 0, msg: "success", data: { token: "..." } }
    if (res.data && res.data.code === 0) {
      const token = res.data.data.token
      setToken(token) // 存入 LocalStorage
      console.log('✅ Auto login success, token refreshed.')
    } else {
      console.warn('⚠️ Auto login failed:', res.data.msg)
    }
  } catch (e) {
    console.error('❌ Auto login network error:', e)
    // 即使登录失败（如后端未启动），也继续挂载应用，以免页面白屏
    // 后续的用户操作会由 request.js 拦截器处理错误
  } finally {
    // 3. 挂载 Vue 应用
    app.mount('#app')
  }
}

// 启动应用
initApp()