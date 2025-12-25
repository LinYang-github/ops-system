import axios from 'axios'
import { ElMessage } from 'element-plus'

// 1. 创建 axios 实例
const service = axios.create({
  // 如果你的 API 地址不是当前域名，这里写完整路径，例如 'http://127.0.0.1:8080'
  // 开发环境通常通过 Vite proxy 代理，所以这里留空或写 '/' 即可
  baseURL: '/', 
  timeout: 15000, // 请求超时时间
  headers: { 'Content-Type': 'application/json' }
})

// 2. 请求拦截器 (Request Interceptor)
service.interceptors.request.use(
  config => {
    // 可以在这里统一添加 Token
    // const token = localStorage.getItem('token')
    // if (token) {
    //   config.headers['Authorization'] = 'Bearer ' + token
    // }
    const token = "ops-system-secret-key" 
    config.headers['Authorization'] = 'Bearer ' + token
    return config
  },
  error => {
    console.error('Request Error:', error)
    return Promise.reject(error)
  }
)

// 3. 响应拦截器 (Response Interceptor)
service.interceptors.response.use(
  response => {
    // response.data 是后端返回的原始 JSON: { code: 0, msg: "...", data: ... }
    const res = response.data

    // 特殊处理：如果是二进制文件流 (Blob)，直接返回整个 response
    if (response.config.responseType === 'blob' || response.config.responseType === 'arraybuffer') {
      return response
    }

    // 兼容处理：如果后端没有按照标准信封格式返回 (比如旧接口)，直接返回
    if (res.code === undefined) {
      return res
    }

    // --- 核心逻辑：判断业务状态码 ---
    if (res.code === 0) {
      // 成功：直接解包，返回 data 里的内容
      // 这样组件里拿到的就是纯业务数据，不需要再 .data.data
      return res.data
    } else {
      // 失败：统一弹出错误提示
      ElMessage({
        message: res.msg || '系统未知错误',
        type: 'error',
        duration: 5 * 1000
      })
      
      // 返回 reject，这样组件里的 catch 可以捕获到错误（如果需要特殊处理）
      // 这里的 new Error(res.msg) 会被组件的 catch(e) 拿到
      return Promise.reject(new Error(res.msg || 'Error'))
    }
  },
  error => {
    // --- 处理 HTTP 协议级别的错误 (404, 500, 网络断开等) ---
    console.error('HTTP Error:', error)
    let message = error.message
    
    if (error.response) {
      switch (error.response.status) {
        case 400: message = '请求参数错误 (400)'; break
        case 401: message = '未授权，请重新登录 (401)'; break
        case 403: message = '拒绝访问 (403)'; break
        case 404: message = '请求地址不存在 (404)'; break
        case 408: message = '请求超时 (408)'; break
        case 500: message = '服务器内部错误 (500)'; break
        case 502: message = '网关错误 (502)'; break
        case 503: message = '服务不可用 (503)'; break
        case 504: message = '网关超时 (504)'; break
        default: message = `连接错误 (${error.response.status})`;
      }
    } else if (error.message.includes('timeout')) {
      message = '网络请求超时'
    } else if (error.message.includes('Network Error')) {
      message = '网络连接失败'
    }

    ElMessage({
      message: message,
      type: 'error',
      duration: 5 * 1000
    })
    
    return Promise.reject(error)
  }
)

export default service