import { reactive } from 'vue'

export const wsStore = reactive({
  nodes: [],
  systems: [],
  connected: false
})

let socket = null
let reconnectTimer = null

export function connectWebSocket() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const target = import.meta.env.DEV ? 'ws://localhost:8080/api/ws' : `${protocol}//${host}/api/ws`

  socket = new WebSocket(target)

  socket.onopen = () => {
    console.log('WS Connected')
    wsStore.connected = true
    if (reconnectTimer) clearInterval(reconnectTimer)
  }

  socket.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      
      if (msg.type === 'nodes') {
        // 【核心修复点】
        // 旧代码: a.info.ip.localeCompare(b.info.ip)
        // 新代码: a.ip.localeCompare(b.ip)
        // 因为现在后端返回的数据结构是扁平的，没有 info 嵌套了
        wsStore.nodes = (msg.data || []).sort((a, b) => {
          const ipA = a.ip || ''
          const ipB = b.ip || ''
          return ipA.localeCompare(ipB)
        })
      } else if (msg.type === 'systems') {
        wsStore.systems = msg.data || []
      }
    } catch (e) {
      console.error('WS Parse Error', e)
    }
  }

  socket.onclose = () => {
    console.log('WS Disconnected')
    wsStore.connected = false
    reconnectTimer = setTimeout(connectWebSocket, 3000)
  }

  socket.onerror = (err) => {
    console.error('WS Error', err)
    socket.close()
  }
}