<template>
  <div class="view-container">
    
    <!-- Header -->
    <div class="header">
      <div class="header-left">
        <h2>基础设施节点监控</h2>
        <el-tag type="info" round effect="plain" class="count-tag">{{ filteredNodes.length }} Nodes</el-tag>
      </div>
      <div class="header-right">
        <el-input 
          v-model="keyword" 
          placeholder="搜索 IP / 名称 / MAC..." 
          prefix-icon="Search" 
          style="width: 240px; margin-right: 12px;" 
          clearable 
        />
        <el-button type="primary" icon="Plus" @click="addDialogVisible = true">添加节点</el-button>
        <el-button icon="Refresh" circle @click="fetchNodes" :loading="loading" title="强制刷新" />
      </div>
    </div>

    <!-- 节点表格 -->
    <el-card shadow="never" class="table-card">
      <el-table 
        :data="filteredNodes" 
        style="width: 100%; height: 100%" 
        stripe 
        highlight-current-row
      >
        <!-- 1. 节点标识 -->
        <el-table-column label="节点名称 / IP" min-width="220">
          <template #default="scope">
            <div class="node-identity">
              <div class="icon-wrapper" :class="scope.row.status">
                <el-icon><Monitor /></el-icon>
              </div>
              <div class="node-text">
                <div class="node-name-row">
                  <span class="node-name">{{ scope.row.name }}</span>
                  <el-icon class="edit-icon" @click.stop="openRename(scope.row)"><Edit /></el-icon>
                </div>
                <div class="node-ip">
                  {{ scope.row.ip }}
                  <span v-if="scope.row.port > 0" class="node-port">:{{ scope.row.port }}</span>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>

        <!-- 2. 硬件信息 -->
        <el-table-column label="硬件信息" min-width="180" show-overflow-tooltip>
          <template #default="scope">
            <div class="hw-info">
              <div><span class="label">Host:</span> {{ scope.row.hostname }}</div>
              <div><span class="label">OS:</span> {{ formatOS(scope.row.os) }} ({{ scope.row.arch }})</div>
              <div v-if="scope.row.mac_addr"><span class="label">MAC:</span> {{ scope.row.mac_addr }}</div>
            </div>
          </template>
        </el-table-column>

        <!-- 3. 状态 -->
        <el-table-column label="状态" width="100" align="center">
          <template #default="scope">
            <el-tag :type="getStatusType(scope.row.status)" effect="dark" size="small" class="status-tag">
              {{ scope.row.status }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 4. 资源监控 (实时跳动) -->
        <el-table-column label="资源使用率" width="200">
          <template #default="scope">
            <div v-if="scope.row.status === 'online'" class="resource-bar">
              <div class="bar-row">
                <span class="label">CPU</span>
                <el-progress 
                  :percentage="Number(scope.row.cpu_usage?.toFixed(1) || 0)" 
                  :stroke-width="6" :show-text="false" class="progress" :color="customColors"
                />
                <span class="val">{{ scope.row.cpu_usage?.toFixed(0) }}%</span>
              </div>
              <div class="bar-row">
                <span class="label">MEM</span>
                <el-progress 
                  :percentage="Number(scope.row.mem_usage?.toFixed(1) || 0)" 
                  :stroke-width="6" :show-text="false" class="progress" :color="customColors"
                />
                <span class="val">{{ scope.row.mem_usage?.toFixed(0) }}%</span>
              </div>
              <div class="disk-info">
                Disk: {{ (scope.row.disk_total / 1024 / 1024 / 1024).toFixed(0) }} GB
              </div>
            </div>
            <span v-else class="text-gray">-</span>
          </template>
        </el-table-column>

        <!-- 5. 最后心跳 -->
        <el-table-column label="最后心跳" width="140" align="right">
          <template #default="scope">
            <span class="time-text">{{ formatTime(scope.row.last_heartbeat) }}</span>
          </template>
        </el-table-column>

        <!-- 6. 操作 -->
        <el-table-column label="操作" width="120" fixed="right" align="center">
          <template #default="scope">
            <el-button link type="primary" @click="openDetail(scope.row)">详情</el-button>
            <el-dropdown trigger="click" @command="(cmd) => handleCommand(cmd, scope.row)">
              <el-button link type="primary">
                管理 <el-icon class="el-icon--right"><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="terminal" icon="Terminal">Web 终端</el-dropdown-item>
                  <el-dropdown-item command="reset" icon="RefreshLeft">重置名称</el-dropdown-item>
                  <el-dropdown-item command="delete" icon="Delete" style="color: var(--el-color-danger)" divided>删除节点</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 弹窗1：Web 终端 (找回功能) -->
    <el-dialog v-model="cmdDialog.visible" :title="`远程终端 - ${cmdDialog.targetIP}`" width="700px">
      <div class="cmd-container">
        <el-input 
          v-model="cmdDialog.command" 
          placeholder="请输入系统命令 (例如: ipconfig, ls -la, ps aux)" 
          @keyup.enter="execCmd"
          class="cmd-input"
        >
          <template #prepend>></template>
          <template #append>
            <el-button @click="execCmd" :loading="cmdDialog.loading">执行</el-button>
          </template>
        </el-input>
        
        <div class="console-output" v-loading="cmdDialog.loading" element-loading-background="rgba(0, 0, 0, 0.8)">
          <div v-if="!cmdDialog.result && !cmdDialog.error" class="console-placeholder">等待命令执行...</div>
          <pre v-if="cmdDialog.result" class="success-output">{{ cmdDialog.result }}</pre>
          <pre v-if="cmdDialog.error" class="error-output">{{ cmdDialog.error }}</pre>
        </div>
      </div>
    </el-dialog>

    <!-- 弹窗2：添加节点 -->
    <el-dialog v-model="addDialogVisible" title="添加规划节点" width="400px">
      <el-form :model="newNode" label-width="80px">
        <el-form-item label="IP 地址">
          <el-input v-model="newNode.ip" placeholder="192.168.1.100" />
        </el-form-item>
        <el-form-item label="节点名称">
          <el-input v-model="newNode.name" placeholder="自定义别名 (可选)" />
        </el-form-item>
      </el-form>
      <div class="dialog-tip"><el-icon><InfoFilled /></el-icon> 节点状态初始为 "planned"，待 Worker 启动并上报后变更为 "online"。</div>
      <template #footer>
        <el-button @click="addDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="addNode">确定</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗3：重命名 -->
    <el-dialog v-model="renameDialog.visible" title="修改节点名称" width="400px">
      <el-form label-width="80px">
        <el-form-item label="当前 IP">
          <el-input v-model="renameDialog.ip" disabled />
        </el-form-item>
        <el-form-item label="新名称">
          <el-input v-model="renameDialog.name" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="renameDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="confirmRename">保存</el-button>
      </template>
    </el-dialog>
    <NodeDetailDrawer v-model="detailVisible" :nodeInfo="currentNode" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'
import { wsStore } from '../store/wsStore'
import { Search, Plus, Refresh, Monitor, Edit, ArrowDown, Terminal, RefreshLeft, Delete, InfoFilled } from '@element-plus/icons-vue'

import NodeDetailDrawer from './NodeDetailDrawer.vue'

// --- 状态定义 ---
const loading = ref(false)
const keyword = ref('')
const addDialogVisible = ref(false)
const newNode = reactive({ ip: '', name: '' })
const renameDialog = reactive({ visible: false, ip: '', name: '' })

const detailVisible = ref(false)
const currentNode = ref(null)
    
const openDetail = (row) => {
  currentNode.value = row
  detailVisible.value = true
}

// 终端状态
const cmdDialog = reactive({
  visible: false,
  targetIP: '',
  command: '',
  result: '',
  error: '',
  loading: false
})

const customColors = [
  { color: '#67C23A', percentage: 60 },
  { color: '#E6A23C', percentage: 80 },
  { color: '#F56C6C', percentage: 100 },
]

// --- 数据源 (实时) ---
// 使用计算属性绑定 WS Store，确保数据实时更新
const nodes = computed(() => wsStore.nodes)

const filteredNodes = computed(() => {
  if (!keyword.value) return nodes.value
  const kw = keyword.value.toLowerCase()
  return nodes.value.filter(n => 
    n.ip.includes(kw) || 
    n.name.toLowerCase().includes(kw) || 
    (n.mac_addr && n.mac_addr.toLowerCase().includes(kw))
  )
})

// --- API ---

const fetchNodes = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/nodes')
    wsStore.nodes = res.data || []
    ElMessage.success('数据已刷新')
  } catch(e) { ElMessage.error('刷新失败') } 
  finally { loading.value = false }
}

const addNode = async () => {
  if(!newNode.ip) return ElMessage.warning('请输入 IP')
  try {
    await axios.post('/api/nodes/add', { ip: newNode.ip, name: newNode.name || newNode.ip })
    addDialogVisible.value = false
    newNode.ip = ''; newNode.name = ''
    ElMessage.success('添加成功')
    fetchNodes()
  } catch(e) { ElMessage.error(e.message) }
}

const handleCommand = (cmd, row) => {
  if (cmd === 'reset') {
    axios.post('/api/nodes/reset_name', { ip: row.ip }).then(() => { ElMessage.success('重置成功'); fetchNodes() })
  } else if (cmd === 'delete') {
    ElMessageBox.confirm(`确定删除节点 ${row.ip}?`, '警告', { type: 'warning' }).then(async () => {
        await axios.post('/api/nodes/delete', { ip: row.ip })
        ElMessage.success('已删除')
        fetchNodes()
    })
  } else if (cmd === 'terminal') {
    openCmdDialog(row)
  }
}

// 终端逻辑 (找回)
const openCmdDialog = (row) => {
  if (row.status !== 'online') {
    return ElMessage.warning('节点不在线，无法连接终端')
  }
  cmdDialog.targetIP = row.ip
  cmdDialog.command = ''
  cmdDialog.result = ''
  cmdDialog.error = ''
  cmdDialog.visible = true
}

const execCmd = async () => {
  if (!cmdDialog.command) return
  cmdDialog.loading = true
  cmdDialog.result = ''
  cmdDialog.error = ''
  
  try {
    const res = await axios.post('/api/ctrl/cmd', {
      target_ip: cmdDialog.targetIP,
      command: cmdDialog.command
    })
    cmdDialog.result = res.data.output
    cmdDialog.error = res.data.error
  } catch (e) {
    cmdDialog.error = "请求失败: " + e.message
  } finally {
    cmdDialog.loading = false
  }
}

// Rename
const openRename = (row) => { renameDialog.ip = row.ip; renameDialog.name = row.name; renameDialog.visible = true }
const confirmRename = async () => { await axios.post('/api/nodes/rename', { ip: renameDialog.ip, name: renameDialog.name }); renameDialog.visible = false; fetchNodes() }

// Helpers
const getStatusType = (s) => s==='online'?'success':(s==='planned'?'info':'danger')
const formatTime = (ts) => { if(!ts)return'-'; const d=Math.floor(Date.now()/1000-ts); if(d<60)return'刚刚'; if(d<3600)return`${Math.floor(d/60)}分前`; return new Date(ts*1000).toLocaleDateString() }
const formatOS = (s) => s && s.length>20 ? s.substring(0,20)+'...' : s

onMounted(() => {
  if(wsStore.nodes.length === 0) fetchNodes()
})
</script>

<style scoped>
.view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
.header { padding: 15px 20px; border-bottom: 1px solid var(--el-border-color-light); display: flex; justify-content: space-between; align-items: center; background: var(--el-bg-color); flex-shrink: 0; }
.header-left { display: flex; align-items: center; gap: 12px; }
.header-left h2 { margin: 0; font-size: 18px; color: var(--el-text-color-primary); }
.count-tag { font-family: monospace; }
.header-right { display: flex; align-items: center; }

.table-card { border: none; flex: 1; display: flex; flex-direction: column; overflow: hidden; background: transparent; }

/* 节点列样式 */
.node-identity { display: flex; align-items: center; gap: 12px; }
.icon-wrapper { width: 36px; height: 36px; border-radius: 8px; display: flex; align-items: center; justify-content: center; font-size: 20px; background: var(--el-fill-color-light); color: var(--el-text-color-secondary); }
.icon-wrapper.online { background: var(--el-color-success-light-9); color: var(--el-color-success); }
.icon-wrapper.offline { background: var(--el-color-danger-light-9); color: var(--el-color-danger); }
.icon-wrapper.planned { background: var(--el-fill-color); color: var(--el-text-color-placeholder); }

.node-text { display: flex; flex-direction: column; line-height: 1.3; }
.node-name-row { display: flex; align-items: center; gap: 6px; }
.node-name { font-weight: 600; font-size: 14px; color: var(--el-text-color-primary); }
.edit-icon { cursor: pointer; font-size: 12px; color: var(--el-text-color-secondary); }
.edit-icon:hover { color: var(--el-color-primary); }
.node-ip { font-family: monospace; color: var(--el-text-color-regular); font-size: 12px; }
.node-port { color: var(--el-text-color-secondary); }

.hw-info { font-size: 12px; color: var(--el-text-color-regular); line-height: 1.4; }
.hw-info .label { color: var(--el-text-color-secondary); margin-right: 4px; }

.resource-bar { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.bar-row { display: flex; align-items: center; gap: 8px; }
.bar-row .label { width: 30px; color: var(--el-text-color-secondary); }
.bar-row .progress { flex: 1; width: 80px; }
.bar-row .val { width: 35px; text-align: right; font-family: monospace; }
.disk-info { margin-top: 2px; color: var(--el-text-color-secondary); font-size: 11px; }

.status-tag { width: 60px; text-align: center; }
.time-text { font-size: 12px; color: var(--el-text-color-secondary); }
.text-gray { color: var(--el-text-color-placeholder); }
.dialog-tip { margin-top: 10px; font-size: 12px; color: var(--el-text-color-secondary); display: flex; align-items: center; gap: 5px; }

/* 终端样式 */
.cmd-container { display: flex; flex-direction: column; gap: 10px; }
.console-output { background-color: #1e1e1e; border-radius: 4px; padding: 12px; min-height: 300px; max-height: 500px; overflow-y: auto; font-family: 'Consolas', 'Monaco', monospace; font-size: 13px; line-height: 1.5; }
.console-placeholder { color: #555; text-align: center; margin-top: 100px; }
.success-output { color: #d4d4d4; margin: 0; white-space: pre-wrap; word-wrap: break-word; }
.error-output { color: #f56c6c; margin-top: 10px; white-space: pre-wrap; }
</style>