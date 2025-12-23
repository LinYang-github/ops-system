<template>
  <el-dialog 
    v-model="visible" 
    :title="`实时日志: ${instanceName}`" 
    width="1000px" 
    :before-close="handleClose"
    top="5vh"
    custom-class="log-dialog"
  >
    <!-- 1. 顶部工具栏 -->
    <div class="log-toolbar">
      <!-- 文件选择 -->
      <el-select v-model="currentFile" placeholder="选择日志文件" size="small" style="width: 180px" @change="connectWs">
        <el-option v-for="f in fileList" :key="f" :label="f" :value="f" />
      </el-select>
      
      <!-- 搜索过滤 -->
      <el-input 
        v-model="filterKeyword" 
        placeholder="关键字高亮/过滤..." 
        size="small" 
        style="width: 200px" 
        clearable
        prefix-icon="Search"
      />

      <!-- 自动滚动开关 -->
      <el-checkbox v-model="autoScroll" size="small" border>自动滚动</el-checkbox>

      <div class="spacer"></div>

      <!-- 状态指示 -->
      <div class="status-indicator">
        <span class="dot" :class="{ active: isConnected }"></span>
        {{ isConnected ? '已连接' : '已断开' }}
      </div>
      
      <el-button size="small" type="info" plain @click="clearLogs">清屏</el-button>
    </div>

    <!-- 2. 日志内容区域 -->
    <div class="log-container" ref="logContainer">
      <!-- 使用 v-html 渲染经过处理的带颜色的 HTML -->
      <!-- 增加 v-memo 优化性能，仅当行内容或关键词变化时重新渲染 -->
      <div 
        v-for="(line, i) in filteredLogs" 
        :key="i" 
        class="log-line"
        :class="getLevelClass(line.text)"
        v-html="formatLine(line.text)"
      ></div>
      
      <div v-if="logs.length === 0" class="empty-tip">等待日志数据...</div>
      
      <!-- 底部锚点，用于自动滚动 -->
      <div ref="bottomAnchor"></div>
    </div>
  </el-dialog>
</template>

<script setup>
import { ref, watch, nextTick, computed, onUnmounted } from 'vue'
import axios from 'axios'
import { Search } from '@element-plus/icons-vue'

const props = defineProps(['modelValue', 'instanceId', 'instanceName'])
const emit = defineEmits(['update:modelValue'])

// --- 状态 ---
const visible = ref(false)
const fileList = ref([])
const currentFile = ref('')
const logs = ref([]) // 存储原始日志对象 { id: 1, text: "..." }
const isConnected = ref(false)
const autoScroll = ref(true)
const filterKeyword = ref('')

const logContainer = ref(null)
const bottomAnchor = ref(null)
let socket = null
let logIdCounter = 0

// --- 监听打开 ---
watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.instanceId) {
    loadFiles()
  } else {
    closeWs()
  }
})

// --- 自动滚动逻辑 ---
watch(() => logs.value.length, () => {
  if (autoScroll.value) {
    nextTick(() => {
      bottomAnchor.value?.scrollIntoView({ behavior: "smooth" })
    })
  }
})

// --- 过滤逻辑 ---
// 如果你希望“只显示匹配行”，使用这个 computed
// 如果你希望“显示所有行但高亮匹配词”，则直接遍历 logs，filterKeyword 仅用于高亮
// 这里采用：显示所有行 + 高亮
const filteredLogs = computed(() => {
  // 如果你想做过滤显示，可以在这里 filter
  // return logs.value.filter(l => l.text.includes(filterKeyword.value))
  return logs.value
})

const handleClose = () => {
  closeWs()
  logs.value = []
  emit('update:modelValue', false)
}

// 1. 获取文件列表
const loadFiles = async () => {
  try {
    const res = await axios.get(`/api/instance/logs/files?instance_id=${props.instanceId}`)
    fileList.value = res.data.files || []
    if (fileList.value.length > 0) {
      currentFile.value = fileList.value[0]
      connectWs()
    }
  } catch (e) {
    logs.value.push({ id: logIdCounter++, text: `[System] 获取文件列表失败: ${e.message}` })
  }
}

// 2. WebSocket 连接
const connectWs = () => {
  closeWs()
  logs.value = []
  
  if (!currentFile.value) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const targetHost = import.meta.env.DEV ? 'localhost:8080' : host
  
  const url = `${protocol}//${targetHost}/api/instance/logs/stream?instance_id=${props.instanceId}&log_key=${encodeURIComponent(currentFile.value)}`
  
  socket = new WebSocket(url)

  socket.onopen = () => {
    isConnected.value = true
    logs.value.push({ id: logIdCounter++, text: `[System] 已连接至 ${currentFile.value}...` })
  }

  socket.onmessage = (event) => {
    const text = event.data
    // 限制前端日志缓存行数 (防止内存溢出)
    if (logs.value.length > 2000) {
      logs.value.shift()
    }
    logs.value.push({ id: logIdCounter++, text: text })
  }

  socket.onclose = (e) => {
    isConnected.value = false
    logs.value.push({ id: logIdCounter++, text: `[System] 连接断开 (Code: ${e.code})` })
    // 断开时自动停止滚动，方便查看错误
    autoScroll.value = false
  }
  
  socket.onerror = () => {
    logs.value.push({ id: logIdCounter++, text: `[System] 连接发生错误` })
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

// --- 样式处理核心逻辑 ---

// 1. 获取行级样式 (整行变色)
const getLevelClass = (text) => {
  const t = text.toUpperCase()
  if (t.includes('ERROR') || t.includes('FAIL') || t.includes('EXCEPTION')) return 'line-error'
  if (t.includes('WARN')) return 'line-warn'
  if (t.includes('INFO')) return 'line-info'
  if (t.includes('DEBUG')) return 'line-debug'
  return ''
}

// 2. 格式化行内容 (转义HTML + 关键字高亮)
const formatLine = (text) => {
  if (!text) return ''
  
  // XSS 防护：先转义 HTML 标签
  let escaped = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;")

  // 关键字高亮
  if (filterKeyword.value) {
    // 使用正则全局替换，忽略大小写
    try {
      const reg = new RegExp(`(${filterKeyword.value})`, 'gi')
      escaped = escaped.replace(reg, '<mark class="highlight">$1</mark>')
    } catch (e) {
      // 防止正则语法错误 (如用户输入了 "[")
    }
  }

  return escaped
}

onUnmounted(() => closeWs())
</script>

<style scoped>
.log-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid #333; /* 深色边框适配深色背景 */
  background-color: #1e1e1e; /* 工具栏也用深色，或者用弹窗默认色 */
}

.spacer { flex: 1; }

.status-indicator {
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 6px;
  color: #909399;
  margin-right: 10px;
}
.dot { width: 8px; height: 8px; border-radius: 50%; background: #909399; }
.dot.active { background: #67C23A; }

/* 日志容器 */
.log-container {
  height: 600px;
  background-color: #1e1e1e; /* 终端黑 */
  color: #d4d4d4;            /* 默认字色 */
  padding: 15px;
  overflow-y: auto;
  font-family: 'Menlo', 'Monaco', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.6;
  border-radius: 0 0 4px 4px;
}

/* 滚动条美化 */
.log-container::-webkit-scrollbar { width: 10px; background: #1e1e1e; }
.log-container::-webkit-scrollbar-thumb { background: #444; border-radius: 5px; }
.log-container::-webkit-scrollbar-thumb:hover { background: #555; }

/* 日志行样式 */
.log-line {
  white-space: pre-wrap; /* 保留换行 */
  word-break: break-all; /* 防止长单词撑开 */
  border-bottom: 1px solid transparent; /* 占位防止抖动 */
}
.log-line:hover { background-color: #2a2d2e; }

/* 日志级别配色 (仿 JetBrains IDEA Console) */
.line-error { color: #f56c6c; font-weight: bold; } /* 鲜红 */
.line-warn  { color: #e6a23c; } /* 橙黄 */
.line-info  { color: #a9b7c6; } /* 默认灰白 */
.line-debug { color: #909399; } /* 深灰 */

/* 关键字高亮 */
:deep(.highlight) {
  background-color: #ffe793;
  color: #000;
  font-weight: bold;
  padding: 0 2px;
  border-radius: 2px;
}

.empty-tip { color: #555; text-align: center; margin-top: 150px; }
</style>