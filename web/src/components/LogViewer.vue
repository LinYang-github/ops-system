<template>
  <el-dialog 
    v-model="visible" 
    :title="`实时日志: ${instanceName}`" 
    width="1000px" 
    :before-close="handleClose"
    top="5vh"
    destroy-on-close
    class="log-dialog"
  >
    <!-- 1. 顶部工具栏 -->
    <div class="log-toolbar">
      <el-select v-model="currentFile" placeholder="选择日志文件" size="small" style="width: 180px" @change="connectWs">
        <el-option v-for="f in fileList" :key="f" :label="f" :value="f" />
      </el-select>
      
      <!-- 搜索框 -->
      <el-input 
        v-model="searchKeyword" 
        placeholder="搜索..." 
        size="small" 
        style="width: 200px" 
        clearable
        @input="handleSearch"
        @keyup.enter="findNext"
      >
        <template #append>
          <el-button :icon="Search" @click="findNext" />
        </template>
      </el-input>

      <el-checkbox v-model="autoScroll" size="small" border>自动滚动</el-checkbox>

      <div class="spacer"></div>

      <div class="status-indicator">
        <span class="dot" :class="{ active: isConnected }"></span>
        {{ isConnected ? '实时连通' : '连接断开' }}
      </div>
      
      <el-button size="small" type="info" plain @click="clearLogs">清屏</el-button>
    </div>

    <!-- 2. xterm 容器 -->
    <div class="terminal-wrapper" v-loading="loading">
      <div ref="terminalContainer" class="xterm-container"></div>
    </div>
  </el-dialog>
</template>

<script setup>
import { ref, watch, nextTick, onUnmounted } from 'vue'
// 【修复点 1】使用封装的 request
import request from '../utils/request'
import { Search } from '@element-plus/icons-vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { SearchAddon } from 'xterm-addon-search'
import 'xterm/css/xterm.css'

const props = defineProps(['modelValue', 'instanceId', 'instanceName'])
const emit = defineEmits(['update:modelValue'])

// 状态
const visible = ref(false)
const loading = ref(false)
const fileList = ref([])
const currentFile = ref('')
const isConnected = ref(false)
const autoScroll = ref(true)
const searchKeyword = ref('')

const terminalContainer = ref(null)
let term = null
let socket = null
let fitAddon = null
let searchAddon = null

watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.instanceId) {
    // 弹窗动画结束后再初始化
    setTimeout(() => {
      initTerminal()
      loadFiles()
    }, 100)
  } else {
    closeWs()
    disposeTerminal()
  }
})

const handleClose = () => {
  closeWs()
  disposeTerminal()
  emit('update:modelValue', false)
}

// --- xterm 初始化 ---
const initTerminal = () => {
  if (term) return

  term = new Terminal({
    cursorBlink: false,
    disableStdin: true, // 只读
    fontSize: 13,
    lineHeight: 1.4,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: {
      background: '#1e1e1e',
      foreground: '#d4d4d4',
      selectionBackground: 'rgba(255, 255, 255, 0.3)'
    },
    scrollback: 10000,
    convertEol: true,
  })

  fitAddon = new FitAddon()
  searchAddon = new SearchAddon()
  term.loadAddon(fitAddon)
  term.loadAddon(searchAddon)

  term.open(terminalContainer.value)
  fitAddon.fit()

  term.onScroll(e => {
    // 简单的判断：如果不在底部，停止自动滚动
    if (term.buffer.active.viewportY < term.buffer.active.baseY) {
      autoScroll.value = false
    } else {
      autoScroll.value = true
    }
  })
}

const disposeTerminal = () => {
  if (term) {
    term.dispose()
    term = null
  }
}

// --- 业务逻辑 ---

const loadFiles = async () => {
  try {
    loading.value = true
    // 【修复点 2】使用 request.get，返回的直接是 payload
    const res = await request.get(`/api/instance/logs/files?instance_id=${props.instanceId}`)
    
    // request.js 已经解包了 code/msg，直接取 files
    fileList.value = res.files || []
    
    if (fileList.value.length > 0) {
      currentFile.value = fileList.value[0]
      connectWs()
    } else {
       term?.writeln('\x1b[33m[System] 该实例暂无日志文件\x1b[0m')
    }
  } catch (e) {
    term?.writeln(`\x1b[31m[System] 获取文件列表失败: ${e.message}\x1b[0m`)
  } finally {
    loading.value = false
  }
}

const connectWs = () => {
  closeWs()
  term?.clear()
  
  if (!currentFile.value) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  // 适配开发环境端口转发
  const targetHost = import.meta.env.DEV ? 'localhost:8080' : host
  
  const url = `${protocol}//${targetHost}/api/instance/logs/stream?instance_id=${props.instanceId}&log_key=${encodeURIComponent(currentFile.value)}`
  
  console.log("Connecting WS:", url) // 调试日志

  socket = new WebSocket(url)

  socket.onopen = () => {
    isConnected.value = true
    term?.writeln(`\x1b[32m[System] 已连接至 ${currentFile.value}...\x1b[0m`)
  }

  socket.onmessage = (event) => {
    const text = event.data
    
    // 1. 处理颜色
    const coloredText = colorizeLog(text)
    
    // 2. 【关键修复】使用 writeln 而不是 write
    // writeln 会自动在末尾追加 \r\n，解决日志挤在一行的问题
    if (term) {
        term.writeln(coloredText)
    }
    
    // 3. 自动滚动
    if (autoScroll.value) {
      term?.scrollToBottom()
    }
  }

  socket.onclose = (e) => {
    isConnected.value = false
    // 1000 是正常关闭，其他是非正常
    if (e.code !== 1000) {
        term?.writeln(`\r\n\x1b[31m[System] 连接断开 (Code: ${e.code})，请检查后端日志\x1b[0m`)
    } else {
        term?.writeln(`\r\n\x1b[33m[System] 连接关闭\x1b[0m`)
    }
  }
  
  socket.onerror = () => {
    term?.writeln(`\r\n\x1b[31m[System] 连接错误\x1b[0m`)
  }
}

const closeWs = () => {
  if (socket) {
    socket.close()
    socket = null
  }
  isConnected.value = false
}

const clearLogs = () => term?.clear()

// --- 搜索 ---
const handleSearch = (val) => {
  if (!val) {
    searchAddon?.clearDecoration()
    return
  }
  searchAddon?.findNext(val, {
    decorations: {
      matchBackground: '#f5c356',
      matchBorder: '#b38600',
      activeMatchBackground: '#ff0000',
      activeMatchColor: '#ffffff'
    }
  })
}

const findNext = () => {
  if(searchKeyword.value) {
    searchAddon?.findNext(searchKeyword.value)
  }
}

// --- 辅助：日志着色 ---
const colorizeLog = (text) => {
  return text
    .replace(/(ERROR|FAIL|EXCEPTION)/ig, '\x1b[1;31m$1\x1b[0m')
    .replace(/(WARN|WARNING)/ig, '\x1b[1;33m$1\x1b[0m')
    .replace(/(INFO)/ig, '\x1b[1;32m$1\x1b[0m')
    .replace(/(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})/g, '\x1b[36m$1\x1b[0m')
}

onUnmounted(() => {
  closeWs()
  disposeTerminal()
})
</script>

<style scoped>
.log-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid #333;
  background-color: #1e1e1e;
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

.terminal-wrapper {
  height: 600px;
  background-color: #1e1e1e;
  padding: 5px;
}

.xterm-container {
  width: 100%;
  height: 100%;
}
</style>