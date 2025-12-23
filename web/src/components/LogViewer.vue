<template>
    <el-dialog 
      v-model="visible" 
      :title="`实时日志: ${instanceName}`" 
      width="900px" 
      :before-close="handleClose"
      top="5vh"
    >
      <div class="log-toolbar">
        <el-select v-model="currentFile" placeholder="选择日志文件" style="width: 200px" @change="connectWs">
          <el-option v-for="f in fileList" :key="f" :label="f" :value="f" />
        </el-select>
        
        <div class="status-indicator">
          <span class="dot" :class="{ active: isConnected }"></span>
          {{ isConnected ? '实时连接中' : '连接断开' }}
        </div>
        
        <el-button size="small" @click="clearLogs">清空屏幕</el-button>
      </div>
  
      <div class="log-container" ref="logContainer">
        <div v-for="(line, i) in logs" :key="i" class="log-line">{{ line }}</div>
        <div v-if="logs.length === 0" class="empty-tip">暂无日志数据...</div>
      </div>
    </el-dialog>
  </template>
  
  <script setup>
  import { ref, watch, nextTick, onUnmounted } from 'vue'
  import axios from 'axios'
  
  const props = defineProps(['modelValue', 'instanceId', 'instanceName'])
  const emit = defineEmits(['update:modelValue'])
  
  const visible = ref(false)
  const fileList = ref([])
  const currentFile = ref('')
  const logs = ref([])
  const isConnected = ref(false)
  const logContainer = ref(null)
  
  let socket = null
  
  watch(() => props.modelValue, (val) => {
    visible.value = val
    if (val && props.instanceId) {
      loadFiles()
    } else {
      closeWs()
    }
  })
  
  const handleClose = () => {
    closeWs()
    logs.value = []
    emit('update:modelValue', false)
  }
  
  // 1. 获取日志文件列表
  const loadFiles = async () => {
    try {
      const res = await axios.get(`/api/instance/logs/files?instance_id=${props.instanceId}`)
      fileList.value = res.data.files || []
      // 默认选中第一个 (通常是 Console Log)
      if (fileList.value.length > 0) {
        currentFile.value = fileList.value[0]
        connectWs()
      }
    } catch (e) {
      logs.value.push(`[System] 获取文件列表失败: ${e.message}`)
    }
  }
  
  // 2. 连接 WebSocket
  const connectWs = () => {
    closeWs() // 切换文件前先断开旧连接
    logs.value = [] // 清空屏幕
    
    if (!currentFile.value) return
  
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    // 适配开发环境端口转发
    const targetHost = import.meta.env.DEV ? 'localhost:8080' : host
    
    const url = `${protocol}//${targetHost}/api/instance/logs/stream?instance_id=${props.instanceId}&log_key=${encodeURIComponent(currentFile.value)}`
    
    socket = new WebSocket(url)
  
    socket.onopen = () => {
      isConnected.value = true
      logs.value.push(`[System] 已连接至 ${currentFile.value}...`)
    }
  
    socket.onmessage = (event) => {
      const text = event.data
      // 限制前端日志行数，防止浏览器卡死 (保留最近 1000 行)
      if (logs.value.length > 1000) {
        logs.value.shift()
      }
      logs.value.push(text)
      scrollToBottom()
    }
  
    socket.onclose = () => {
      isConnected.value = false
      logs.value.push(`[System] 连接已断开`)
    }
    
    socket.onerror = () => {
      logs.value.push(`[System] 连接错误`)
    }
  }
  
  const closeWs = () => {
    if (socket) {
      socket.close()
      socket = null
    }
    isConnected.value = false
  }
  
  const clearLogs = () => logs.value = []
  
  const scrollToBottom = () => {
    nextTick(() => {
      if (logContainer.value) {
        logContainer.value.scrollTop = logContainer.value.scrollHeight
      }
    })
  }
  
  onUnmounted(() => closeWs())
  </script>
  
  <style scoped>
  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 15px;
    margin-bottom: 10px;
    padding-bottom: 10px;
    border-bottom: 1px solid #eee;
  }
  
  .status-indicator {
    font-size: 12px;
    display: flex;
    align-items: center;
    gap: 5px;
    color: #666;
    flex: 1;
  }
  .dot {
    width: 8px; height: 8px; border-radius: 50%; background: #ccc;
  }
  .dot.active { background: #67C23A; }
  
  .log-container {
    height: 500px;
    background-color: #1e1e1e;
    color: #d4d4d4;
    padding: 15px;
    overflow-y: auto;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 13px;
    line-height: 1.5;
    border-radius: 4px;
  }
  
  .log-line {
    white-space: pre-wrap; /* 保留换行 */
    word-break: break-all;
  }
  
  .empty-tip {
    color: #555;
    text-align: center;
    margin-top: 100px;
  }
  </style>