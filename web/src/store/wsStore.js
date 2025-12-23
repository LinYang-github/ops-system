import { reactive } from 'vue'
import { ElNotification } from 'element-plus'
import axios from 'axios' // 引入 axios 用于初始化获取数量

export const wsStore = reactive({
  nodes: [],
  systems: [],
  activeAlertCount: 0, // [新增] 活跃告警数量
  connected: false
})

let socket = null
let reconnectTimer = null

// [新增] 获取当前告警数量 (初始化用)
export async function fetchAlertCount() {
  try {
    const res = await axios.get('/api/alerts/events')
    if (res.data && res.data.active) {
      wsStore.activeAlertCount = res.data.active.length
    }
  } catch (e) {
    console.error("Failed to fetch alert count", e)
  }
}

export function connectWebSocket() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const target = import.meta.env.DEV ? 'ws://localhost:8080/api/ws' : `${protocol}//${host}/api/ws`

  socket = new WebSocket(target)

  socket.onopen = () => {
    console.log('WS Connected')
    wsStore.connected = true
    if (reconnectTimer) clearInterval(reconnectTimer)
    // 连接成功后，同步一次告警数量
    fetchAlertCount()
  }

  socket.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      
      if (msg.type === 'nodes') {
        wsStore.nodes = (msg.data || []).sort((a, b) => {
          const ipA = a.ip || ''
          const ipB = b.ip || ''
          return ipA.localeCompare(ipB)
        })
      } else if (msg.type === 'systems') {
        wsStore.systems = msg.data || []
      } else if (msg.type === 'alert') {
        // [新增] 处理告警推送
        const data = msg.data
        if (data.type === 'fire') {
            wsStore.activeAlertCount++ // 数量+1
            ElNotification({
                title: '⚠️ 告警触发',
                message: data.message,
                type: 'error',
                duration: 0
            })
        } else if (data.type === 'resolve') {
            wsStore.activeAlertCount = Math.max(0, wsStore.activeAlertCount - 1) // 数量-1
            ElNotification({
                title: '✅ 告警恢复',
                message: `Event ID ${data.id} 已恢复正常`,
                type: 'success'
            })
        }
        // 为了数据绝对准确，也可以选择在这里重新 fetchAlertCount()
      }
    } catch (e) {
      console.error('WS Parse Error', e)
    }
  }

  socket.onclose = () => {
    wsStore.connected = false
    reconnectTimer = setTimeout(connectWebSocket, 3000)
  }

  socket.onerror = () => {
    socket.close()
  }
}