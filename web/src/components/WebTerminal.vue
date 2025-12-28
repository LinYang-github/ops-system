<template>
  <el-dialog
    v-model="visible"
    :title="`Web Terminal: ${nodeIP}`"
    width="900px"
    :before-close="handleClose"
    custom-class="terminal-dialog"
    destroy-on-close
  >
    <div ref="terminalContainer" class="terminal-container"></div>
  </el-dialog>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'

const props = defineProps(['modelValue', 'nodeIP'])
const emit = defineEmits(['update:modelValue'])

const visible = ref(false)
const terminalContainer = ref(null)

let term = null
let socket = null
let fitAddon = null

watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.nodeIP) {
    nextTick(() => initTerminal())
  } else {
    closeTerm()
  }
})

const handleClose = () => {
  closeTerm()
  emit('update:modelValue', false)
}

const initTerminal = () => {
  // 1. 初始化 xterm
  term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: {
      background: '#1e1e1e',
      foreground: '#d4d4d4',
    }
  })
  
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(terminalContainer.value)
  fitAddon.fit()

  // 2. 连接 WebSocket
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const targetHost = import.meta.env.DEV ? 'localhost:8080' : host
  const url = `${protocol}//${targetHost}/api/node/terminal?ip=${props.nodeIP}`
  
  socket = new WebSocket(url)

  socket.onopen = () => {
    term.write('\r\n\x1b[32m[System] Connection established.\x1b[0m\r\n')
    // 发送初始尺寸
    sendResize(term.rows, term.cols)
  }

  socket.onmessage = (ev) => {
    // 接收二进制数据 (blob) 或 文本
    if (ev.data instanceof Blob) {
      const reader = new FileReader()
      reader.onload = () => {
        // xterm write 可以接收 string 或 Uint8Array
        term.write(new Uint8Array(reader.result))
      }
      reader.readAsArrayBuffer(ev.data)
    } else {
      term.write(ev.data)
    }
  }

  socket.onclose = () => {
    term.write('\r\n\x1b[31m[System] Connection closed.\x1b[0m\r\n')
  }

  // 3. 监听输入
  term.onData(data => {
    if (socket && socket.readyState === WebSocket.OPEN) {
      // 发送原始数据 (二进制)
      socket.send(new TextEncoder().encode(data))
    }
  })

  // 4. 监听 Resize
  term.onResize(size => {
    sendResize(size.rows, size.cols)
  })
}

const sendResize = (rows, cols) => {
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify({
      type: "resize",
      rows: rows,
      cols: cols
    }))
  }
}

const closeTerm = () => {
  if (socket) {
    socket.close()
    socket = null
  }
  if (term) {
    term.dispose()
    term = null
  }
}

onUnmounted(() => closeTerm())
</script>

<style scoped>
.terminal-container {
  height: 500px;
  background-color: #1e1e1e;
  padding: 10px;
  border-radius: 4px;
}
</style>