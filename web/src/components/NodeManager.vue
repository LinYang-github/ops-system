<template>
  <div class="view-container">
    
    <!-- ========================= -->
    <!-- 1. 顶部操作栏 (Header)     -->
    <!-- ========================= -->
    <div class="header">
      <div class="header-left">
        <h2>基础设施节点监控</h2>
        <el-tag type="info" round effect="plain" class="count-tag">
          {{ filteredNodes.length }} Nodes
        </el-tag>
      </div>
      <div class="header-right">
        <el-input 
          v-model="keyword" 
          placeholder="搜索 IP / 名称 / MAC / ID..." 
          prefix-icon="Search" 
          class="search-input"
          clearable 
          @input="handleSearch"
        />
        <el-button type="primary" icon="Plus" @click="addDialogVisible = true">
          添加节点
        </el-button>
        <el-button 
          icon="Refresh" 
          circle 
          @click="fetchNodes" 
          :loading="loading" 
          title="强制刷新数据" 
        />
      </div>
    </div>

    <!-- ========================= -->
    <!-- 2. 核心数据表格 (Table)    -->
    <!-- ========================= -->
    <el-card shadow="never" class="table-card">
      <div class="table-wrapper">
        <el-table 
          :data="pagedNodes" 
          style="width: 100%; height: 100%" 
          stripe 
          highlight-current-row
          size="small"
          row-key="id"
        >
          <!-- 列: 节点标识 (图标+名称+IP) -->
          <el-table-column label="节点名称 / IP" min-width="240">
            <template #default="scope">
              <div class="node-identity">
                <!-- 状态图标 -->
                <div class="icon-wrapper" :class="scope.row.status">
                  <el-icon><Monitor /></el-icon>
                </div>
                <!-- 名称与IP信息 -->
                <div class="node-text">
                  <div class="node-name-row">
                    <span class="node-name" :title="scope.row.id">{{ scope.row.name }}</span>
                    <el-icon class="edit-icon" @click.stop="openRename(scope.row)">
                      <Edit />
                    </el-icon>
                  </div>
                  <div class="node-ip">
                    {{ scope.row.ip }}
                  </div>
                </div>
              </div>
            </template>
          </el-table-column>

          <!-- 列: 硬件指纹 -->
          <el-table-column label="硬件信息" min-width="180" show-overflow-tooltip>
            <template #default="scope">
              <div class="hw-info">
                <div><span class="label">Host:</span> {{ scope.row.hostname }}</div>
                <div>
                  <span class="label">OS:</span> 
                  {{ formatOS(scope.row.os) }} ({{ scope.row.arch }})
                </div>
                <div v-if="scope.row.mac_addr">
                  <span class="label">MAC:</span> {{ scope.row.mac_addr }}
                </div>
              </div>
            </template>
          </el-table-column>

          <!-- 列: 运行状态 -->
          <el-table-column label="状态" width="100" align="center">
            <template #default="scope">
              <el-tag 
                :type="getStatusType(scope.row.status)" 
                effect="dark" 
                size="small" 
                class="status-tag"
              >
                {{ scope.row.status }}
              </el-tag>
            </template>
          </el-table-column>

          <!-- 列: 资源监控条 -->
          <el-table-column label="资源使用率" width="220">
            <template #default="scope">
              <div v-if="scope.row.status === 'online'" class="resource-bar">
                <!-- CPU -->
                <div class="bar-row">
                  <span class="label">CPU</span>
                  <el-progress 
                    :percentage="parseUsage(scope.row.cpu_usage)" 
                    :stroke-width="6" 
                    :show-text="false" 
                    class="progress" 
                    :color="customColors"
                  />
                  <span class="val">{{ parseUsage(scope.row.cpu_usage).toFixed(0) }}%</span>
                </div>
                <!-- Memory -->
                <div class="bar-row">
                  <span class="label">MEM</span>
                  <el-progress 
                    :percentage="parseUsage(scope.row.mem_usage)" 
                    :stroke-width="6" 
                    :show-text="false" 
                    class="progress" 
                    :color="customColors"
                  />
                  <span class="val">{{ parseUsage(scope.row.mem_usage).toFixed(0) }}%</span>
                </div>
                <!-- Disk Text -->
                <div class="disk-info">
                  Disk Total: {{ formatBytes(scope.row.disk_total) }}
                </div>
              </div>
              <span v-else class="text-gray">-</span>
            </template>
          </el-table-column>

          <!-- 列: 心跳时间 -->
          <el-table-column label="最后心跳" width="140" align="right">
            <template #default="scope">
              <span class="time-text">{{ formatTime(scope.row.last_heartbeat) }}</span>
            </template>
          </el-table-column>

          <!-- 列: 更多操作 -->
          <el-table-column label="操作" width="120" fixed="right" align="center">
            <template #default="scope">
              <el-dropdown trigger="click" @command="(cmd) => handleCommand(cmd, scope.row)">
                <el-button link type="primary">
                  管理 <el-icon class="el-icon--right"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="detail" icon="View">查看详情</el-dropdown-item>
                    <el-dropdown-item command="terminal" icon="Platform">Web 终端</el-dropdown-item>
                    <el-dropdown-item command="reset" icon="RefreshLeft">重置名称</el-dropdown-item>
                    <el-dropdown-item command="delete" icon="Delete" class="text-danger" divided>
                      删除节点
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 3. 分页控件 -->
      <div class="pagination-bar">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100, 200]"
          :background="true"
          layout="total, sizes, prev, pager, next, jumper"
          :total="filteredNodes.length"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    <!-- ========================= -->
    <!-- 弹窗组件区域               -->
    <!-- ========================= -->

    <!-- 弹窗: Web 终端 (CMD Shell) -->
    <el-dialog 
      v-model="cmdDialog.visible" 
      :title="`远程终端 - ${cmdDialog.targetIP}`" 
      width="700px"
    >
      <div class="cmd-container">
        <el-input 
          v-model="cmdDialog.command" 
          placeholder="请输入系统命令 (例如: ipconfig, ls -la)" 
          @keyup.enter="execCmd"
          class="cmd-input"
        >
          <template #prepend>></template>
          <template #append>
            <el-button @click="execCmd" :loading="cmdDialog.loading">执行</el-button>
          </template>
        </el-input>
        
        <div 
          class="console-output" 
          v-loading="cmdDialog.loading" 
          element-loading-background="rgba(0, 0, 0, 0.8)"
        >
          <div v-if="!cmdDialog.result && !cmdDialog.error" class="console-placeholder">
            等待命令执行...
          </div>
          <pre v-if="cmdDialog.result" class="success-output">{{ cmdDialog.result }}</pre>
          <pre v-if="cmdDialog.error" class="error-output">{{ cmdDialog.error }}</pre>
        </div>
      </div>
    </el-dialog>

    <!-- 弹窗: 添加节点 -->
    <el-dialog v-model="addDialogVisible" title="添加规划节点" width="400px">
      <el-form :model="newNode" label-width="80px">
        <el-form-item label="IP 地址">
          <el-input v-model="newNode.ip" placeholder="192.168.1.100" />
        </el-form-item>
        <el-form-item label="节点名称">
          <el-input v-model="newNode.name" placeholder="自定义别名 (可选)" />
        </el-form-item>
      </el-form>
      <div class="dialog-tip">
        <el-icon><InfoFilled /></el-icon> 
        <span>初始状态为 "planned"，Worker 启动后自动变更为 "online"。</span>
      </div>
      <template #footer>
        <el-button @click="addDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="addNode">确定</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗: 重命名 -->
    <el-dialog v-model="renameDialog.visible" title="修改节点名称" width="400px">
      <el-form label-width="80px">
        <el-form-item label="节点 ID">
          <el-input v-model="renameDialog.id" disabled />
        </el-form-item>
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
    
    <!-- 详情抽屉 & 实时终端组件 -->
    <NodeDetailDrawer v-model="detailVisible" :nodeInfo="currentNode" />
    <WebTerminal v-model="termDialog.visible" :nodeIP="termDialog.nodeIP" />

  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElMessageBox } from 'element-plus'
import { wsStore } from '../store/wsStore'
import { 
  Search, Plus, Refresh, Monitor, Edit, ArrowDown, 
  Platform, RefreshLeft, Delete, InfoFilled, View 
} from '@element-plus/icons-vue'
import NodeDetailDrawer from './NodeDetailDrawer.vue'
import WebTerminal from './WebTerminal.vue'

// ==========================================
// 1. 状态定义 (State)
// ==========================================

const loading = ref(false)
const keyword = ref('')

// 分页状态
const currentPage = ref(1)
const pageSize = ref(20)

// 弹窗控制
const addDialogVisible = ref(false)
const detailVisible = ref(false)
const currentNode = ref(null)

// 表单数据
const newNode = reactive({ ip: '', name: '' })
const renameDialog = reactive({ visible: false, id: '', ip: '', name: '' })

// 终端相关
const cmdDialog = reactive({
  visible: false, targetIP: '', targetID: '', command: '', result: '', error: '', loading: false
})
const termDialog = reactive({ visible: false, nodeIP: '' })

// 进度条颜色阈值
const customColors = [
  { color: '#67C23A', percentage: 60 },
  { color: '#E6A23C', percentage: 80 },
  { color: '#F56C6C', percentage: 100 },
]

// ==========================================
// 2. 计算属性 (Computed)
// ==========================================

// 数据源直接来自 WebSocket Store
const nodes = computed(() => wsStore.nodes)

// 过滤逻辑
const filteredNodes = computed(() => {
  if (!keyword.value) return nodes.value
  const kw = keyword.value.toLowerCase()
  return nodes.value.filter(n => 
    n.ip.includes(kw) || 
    (n.name && n.name.toLowerCase().includes(kw)) || 
    (n.id && n.id.toLowerCase().includes(kw)) ||
    (n.mac_addr && n.mac_addr.toLowerCase().includes(kw))
  )
})

// 分页逻辑
const pagedNodes = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return filteredNodes.value.slice(start, end)
})

// ==========================================
// 3. 核心交互方法 (Actions)
// ==========================================

// --- 数据刷新 ---
const fetchNodes = async () => {
  loading.value = true
  try {
    const res = await request.get('/api/nodes')
    wsStore.nodes = res || []
    ElMessage.success('数据已刷新')
  } catch(e) { 
    // request.js 拦截器会处理错误提示，此处仅需关闭 loading
  } finally { 
    loading.value = false 
  }
}

// --- 添加节点 ---
const addNode = async () => {
  if(!newNode.ip) return ElMessage.warning('请输入 IP')
  try {
    // 规划节点暂时只需要 IP 和名称，ID 由后端生成或 Worker 上报时确定
    await request.post('/api/nodes/add', { 
      ip: newNode.ip, 
      name: newNode.name || newNode.ip 
    })
    addDialogVisible.value = false
    newNode.ip = ''; newNode.name = ''
    ElMessage.success('添加成功')
    fetchNodes()
  } catch(e) { 
    // Error handled by interceptor
  }
}

// --- 下拉菜单操作分发 ---
const handleCommand = (cmd, row) => {
  if (cmd === 'reset') {
    handleResetName(row)
  } else if (cmd === 'delete') {
    handleDelete(row)
  } else if (cmd === 'terminal') {
    openTerminal(row)
  } else if (cmd === 'detail') {
    openDetail(row)
  }
}

// --- 重命名 ---
const openRename = (row) => { 
  renameDialog.id = row.id // 关键：使用 ID
  renameDialog.ip = row.ip
  renameDialog.name = row.name
  renameDialog.visible = true 
}

const confirmRename = async () => { 
  try {
    await request.post('/api/nodes/rename', { 
      id: renameDialog.id, // 使用 ID
      name: renameDialog.name 
    })
    renameDialog.visible = false
    ElMessage.success('名称已更新')
    fetchNodes()
  } catch(e) {}
}

// --- 重置名称 ---
const handleResetName = async (row) => {
  try {
    await request.post('/api/nodes/reset_name', { id: row.id }) // 使用 ID
    ElMessage.success('重置成功')
    fetchNodes()
  } catch(e) {}
}

// --- 删除节点 ---
const handleDelete = (row) => {
  ElMessageBox.confirm(`确定删除节点 ${row.name} (${row.ip})?`, '高危操作', { 
    type: 'warning',
    confirmButtonText: '确定删除',
    cancelButtonText: '取消'
  }).then(async () => {
    try {
      await request.post('/api/nodes/delete', { id: row.id }) // 使用 ID
      ElMessage.success('节点已删除')
      fetchNodes()
    } catch(e) {}
  })
}

// --- 打开终端 ---
const openTerminal = (row) => {
  // 传递 IP 用于显示或连接参数，传递 ID 用于鉴权（视后端实现而定）
  // 这里 WebTerminal 组件 props 定义的是 nodeIP
  termDialog.nodeIP = row.ip 
  termDialog.visible = true
}

const openDetail = (row) => {
  currentNode.value = row
  detailVisible.value = true
}

// --- CMD 执行逻辑 (可选功能) ---
const execCmd = async () => {
  if (!cmdDialog.command) return
  cmdDialog.loading = true
  cmdDialog.result = ''
  cmdDialog.error = ''
  
  try {
    // 假设后端接口支持 target_id
    const res = await request.post('/api/ctrl/cmd', {
      target_id: cmdDialog.targetID, // 优先使用 ID
      target_ip: cmdDialog.targetIP, // 兼容旧接口
      command: cmdDialog.command
    })
    cmdDialog.result = res.output
    cmdDialog.error = res.error
  } catch (e) {
    cmdDialog.error = "请求失败: " + e.message
  } finally {
    cmdDialog.loading = false
  }
}

// ==========================================
// 4. 辅助函数 (Utils)
// ==========================================

// 分页事件
const handleSearch = () => { currentPage.value = 1 }
const handleSizeChange = (val) => { pageSize.value = val; currentPage.value = 1 }
const handleCurrentChange = (val) => { currentPage.value = val }

// 格式化状态样式
const getStatusType = (s) => {
  if (s === 'online') return 'success'
  if (s === 'planned') return 'info'
  return 'danger'
}

// 格式化时间
const formatTime = (ts) => { 
  if(!ts) return '-'
  const d = Math.floor(Date.now()/1000 - ts)
  if(d < 60) return '刚刚'
  if(d < 3600) return `${Math.floor(d/60)}分前`
  return new Date(ts*1000).toLocaleDateString() 
}

// 格式化 OS 字符串
const formatOS = (s) => {
  return s && s.length > 20 ? s.substring(0, 20) + '...' : s
}

// 格式化字节
const formatBytes = (bytes) => {
  if (!bytes) return '0 GB'
  return (bytes / 1024 / 1024 / 1024).toFixed(0) + ' GB'
}

// 安全解析使用率
const parseUsage = (val) => {
  return Number(val?.toFixed(1) || 0)
}

// ==========================================
// 5. 生命周期
// ==========================================

onMounted(() => {
  // 如果 Store 没数据，主动拉取一次
  if(wsStore.nodes.length === 0) fetchNodes()
})
</script>

<style scoped>
/* 容器布局 */
.view-container {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color);
}

/* 顶部 Header */
.header {
  padding: 15px 20px;
  border-bottom: 1px solid var(--el-border-color-light);
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: var(--el-bg-color);
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-left h2 {
  margin: 0;
  font-size: 18px;
  color: var(--el-text-color-primary);
}

.count-tag {
  font-family: monospace;
}

.header-right {
  display: flex;
  align-items: center;
}

.search-input {
  width: 240px;
  margin-right: 12px;
}

/* 表格卡片容器 */
.table-card {
  border: none;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: transparent;
}

/* 覆盖 Card Body 样式以适应 Flex */
.table-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0; /* 移除内边距以贴合边框 */
}

.table-wrapper {
  flex: 1;
  overflow: hidden;
  padding: 0 20px; /* 恢复两侧边距 */
}

/* 分页栏 */
.pagination-bar {
  padding: 10px 20px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  display: flex;
  justify-content: flex-end;
}

/* --- 表格内容样式 --- */

/* 节点标识列 */
.node-identity {
  display: flex;
  align-items: center;
  gap: 12px;
}

.icon-wrapper {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
}

.icon-wrapper.online {
  background: var(--el-color-success-light-9);
  color: var(--el-color-success);
}

.icon-wrapper.offline {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
}

.icon-wrapper.planned {
  background: var(--el-fill-color);
  color: var(--el-text-color-placeholder);
}

.node-text {
  display: flex;
  flex-direction: column;
  line-height: 1.3;
}

.node-name-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.node-name {
  font-weight: 600;
  font-size: 14px;
  color: var(--el-text-color-primary);
}

.edit-icon {
  cursor: pointer;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.edit-icon:hover {
  color: var(--el-color-primary);
}

.node-ip {
  font-family: monospace;
  color: var(--el-text-color-regular);
  font-size: 12px;
}

.node-port {
  color: var(--el-text-color-secondary);
}

/* 硬件信息列 */
.hw-info {
  font-size: 12px;
  color: var(--el-text-color-regular);
  line-height: 1.4;
}

.hw-info .label {
  color: var(--el-text-color-secondary);
  margin-right: 4px;
}

/* 资源监控列 */
.resource-bar {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
}

.bar-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bar-row .label {
  width: 30px;
  color: var(--el-text-color-secondary);
}

.bar-row .progress {
  flex: 1;
  width: 80px;
}

.bar-row .val {
  width: 35px;
  text-align: right;
  font-family: monospace;
}

.disk-info {
  margin-top: 2px;
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

/* 通用文本样式 */
.status-tag {
  width: 60px;
  text-align: center;
}

.time-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.text-gray {
  color: var(--el-text-color-placeholder);
}

.text-danger {
  color: var(--el-color-danger);
}

/* 弹窗样式 */
.dialog-tip {
  margin-top: 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  display: flex;
  align-items: center;
  gap: 5px;
}

.cmd-container {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cmd-input :deep(.el-input-group__prepend) {
  padding: 0 10px;
  background-color: #f5f7fa;
  color: #909399;
  font-weight: bold;
}

.console-output {
  background-color: #1e1e1e;
  border-radius: 4px;
  padding: 12px;
  min-height: 300px;
  max-height: 500px;
  overflow-y: auto;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 13px;
  line-height: 1.5;
}

.console-placeholder {
  color: #555;
  text-align: center;
  margin-top: 100px;
}

.success-output {
  color: #d4d4d4;
  margin: 0;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.error-output {
  color: #f56c6c;
  margin-top: 10px;
  white-space: pre-wrap;
}
</style>