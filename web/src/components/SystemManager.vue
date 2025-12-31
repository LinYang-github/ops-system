
Model
ThinkingThoughts
Expand to view model thoughts

chevron_right
这是一个非常重要的补全。纳管服务（External Register）同样需要适配 NodeID 架构，否则后端无法根据 IP 准确找到节点（特别是当 IP 发生变动时）。

以下是 **修复后完整、未省略** 的 `SystemManager.vue`。

**主要修改点：**
1.  **纳管弹窗表单 (`adoptForm`)**：将绑定的字段从 `nodeIP` 改为 `nodeID`。
2.  **节点选择下拉框**：`:value` 绑定为 `n.id`。
3.  **提交逻辑 (`registerExternal`)**：向后端发送 `node_id`，不再依赖 `node_ip`。

```vue
<template>
  <div class="view-container">
    
    <!-- 1. 主内容区域 -->
    <div class="sys-content" v-loading="loading">
      
      <!-- 有选中系统时显示 -->
      <template v-if="currentSystem">
        <!-- 1.1 顶部 Header -->
        <div class="content-header">
          <div class="header-left">
            <h2 class="sys-title">{{ currentSystem.name }}</h2>
            <el-tag size="small" type="info" effect="plain" class="sys-id-tag">
              {{ currentSystem.id }}
            </el-tag>
          </div>
          
          <div class="header-right">
            <!-- 批量操作按钮 -->
            <el-button-group style="margin-right: 12px">
              <el-tooltip content="启动所有停止的实例" placement="bottom">
                <el-button 
                  size="small" 
                  type="success" 
                  icon="VideoPlay" 
                  @click="handleBatchAction('start')" 
                  :loading="batchLoading"
                >
                  全启
                </el-button>
              </el-tooltip>
              <el-tooltip content="停止所有运行的实例" placement="bottom">
                <el-button 
                  size="small" 
                  type="warning" 
                  icon="VideoPause" 
                  @click="handleBatchAction('stop')" 
                  :loading="batchLoading"
                >
                  全停
                </el-button>
              </el-tooltip>
            </el-button-group>

            <!-- 列显示配置 -->
            <el-popover placement="bottom-end" title="列显示配置" :width="200" trigger="click">
              <template #reference>
                <el-button icon="Setting" circle size="small" title="显示设置" />
              </template>
              <div class="col-setting">
                <el-checkbox 
                  v-for="col in tableColumns" 
                  :key="col.prop" 
                  v-model="col.visible" 
                  :label="col.label" 
                  size="small"
                  style="display: block; margin-right: 0;" 
                />
              </div>
            </el-popover>

            <el-divider direction="vertical" />
            
            <!-- 新增/纳管/刷新 -->
            <el-button type="primary" size="small" icon="Plus" @click="openAddModuleDialog">
              标准组件
            </el-button>
            <el-button type="warning" size="small" icon="Link" @click="openAdoptDialog">
              纳管服务
            </el-button>
            
            <el-button icon="Refresh" size="small" circle @click="refreshData" />
            
            <!-- 更多操作下拉 -->
            <el-dropdown trigger="click" @command="handleCommand" style="margin-left: 8px">
              <el-button link size="small"><el-icon><MoreFilled /></el-icon></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="export" icon="Download">导出单机版</el-dropdown-item>
                  <el-dropdown-item command="delete" style="color: var(--el-color-danger)">删除系统</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>

        <!-- 1.2 核心表格 -->
        <el-card shadow="never" class="table-card">
          <el-table 
            :data="treeData" 
            style="width: 100%; height: 100%;" 
            row-key="id"
            default-expand-all
            :tree-props="{ children: 'children', hasChildren: 'hasChildren' }"
            size="small"
            stripe
            class="custom-table"
          >
            <!-- 列 1: 组件/实例树形列 -->
            <el-table-column 
              label="组件名称 / 实例 ID" 
              min-width="260" 
              show-overflow-tooltip
              class-name="tree-col"
            >
              <template #default="scope">
                <div class="cell-content">
                  <!-- 组件行 (Module) -->
                  <template v-if="scope.row.rowType === 'module'">
                    <el-tag size="small" effect="dark" style="margin-right: 8px">
                      {{ scope.row.start_order }}
                    </el-tag>
                    <span class="module-name">{{ scope.row.module_name }}</span>
                    <span v-if="scope.row.children.length > 0" class="instance-count">
                      ({{ scope.row.children.length }})
                    </span>
                    
                    <span v-if="scope.row.is_external" class="tag-external">EXTERNAL</span>
                    <span v-else class="pkg-hint">{{ scope.row.package_name }} v{{ scope.row.package_version }}</span>
                  </template>
                  
                  <!-- 实例行 (Instance) -->
                  <template v-else>
                    <span class="inst-id">{{ scope.row.id }}</span>
                  </template>
                </div>
              </template>
            </el-table-column>

            <!-- 列 2: 节点 IP (已修改：调用 getNodeIP) -->
            <el-table-column v-if="colConf.ip" label="节点 IP" width="140">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance'" class="mono-text text-primary">
                  {{ getNodeIP(scope.row.node_ip) }}
                </span>
              </template>
            </el-table-column>

            <!-- 列 3: 状态 -->
            <el-table-column v-if="colConf.status" label="状态" width="90">
              <template #default="scope">
                <div v-if="scope.row.rowType === 'instance'" class="status-cell">
                  <el-icon v-if="scope.row.status === 'deploying'" class="is-loading" color="#409EFF" style="margin-right:4px">
                    <Loading />
                  </el-icon>
                  <span :class="['status-text', scope.row.status]">
                    {{ scope.row.status }}
                  </span>
                </div>
              </template>
            </el-table-column>

            <!-- 列 4: PID -->
            <el-table-column v-if="colConf.pid" label="PID" width="80" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">
                  {{ scope.row.pid }}
                </span>
                <span v-else-if="scope.row.rowType === 'instance'" class="text-placeholder">-</span>
              </template>
            </el-table-column>

            <!-- 列 5: 启动时间 -->
            <el-table-column v-if="colConf.uptime" label="启动时间" width="160" class-name="col-no-wrap">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text text-gray text-xs">
                  {{ formatTime(scope.row.uptime) }}
                </span>
              </template>
            </el-table-column>

            <!-- 列 6: CPU -->
            <el-table-column v-if="colConf.cpu" label="CPU" width="80" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">
                  {{ (scope.row.cpu_usage || 0).toFixed(1) }}%
                </span>
              </template>
            </el-table-column>

            <!-- 列 7: 内存 -->
            <el-table-column v-if="colConf.mem" label="内存" width="90" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">
                  {{ (scope.row.mem_usage || 0) }} MB
                </span>
              </template>
            </el-table-column>

            <!-- 列 8: IO -->
            <el-table-column v-if="colConf.io" label="IO R/W" width="130" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text text-gray text-xs">
                  {{ scope.row.io_read }}/{{ scope.row.io_write }} KB
                </span>
              </template>
            </el-table-column>

            <!-- 列 9: 操作按钮 -->
            <el-table-column label="操作" width="150" fixed="right" align="right">
              <template #default="scope">
                <!-- 组件级别操作 -->
                <div v-if="scope.row.rowType === 'module'">
                  <el-button 
                    v-if="!scope.row.is_external" 
                    link type="primary" size="small" 
                    @click="openDeployDialog(scope.row)"
                  >
                    部署
                  </el-button>
                  <el-popconfirm 
                    v-if="!scope.row.is_external" 
                    title="删除定义?" 
                    @confirm="deleteModule(scope.row.id)"
                  >
                    <template #reference>
                      <el-button link type="info" size="small">删除</el-button>
                    </template>
                  </el-popconfirm>
                </div>
                <!-- 实例级别操作 -->
                <div v-else>
                  <el-button 
                    v-if="scope.row.status !== 'running'"
                    link type="success" size="small"
                    @click="handleAction(scope.row.id, 'start')"
                  >
                    启动
                  </el-button>
                  <el-button 
                    v-if="scope.row.status === 'running'"
                    link type="warning" size="small"
                    @click="handleAction(scope.row.id, 'stop')"
                  >
                    停止
                  </el-button>
                  <el-button 
                    link type="primary" size="small" icon="Document" 
                    @click="openLog(scope.row)"
                  >
                    日志
                  </el-button>
                  <el-dropdown 
                    trigger="click" size="small" 
                    @command="(cmd) => handleInstanceCommand(cmd, scope.row.id)"
                  >
                    <span class="el-dropdown-link action-more">
                      <el-icon><More /></el-icon>
                    </span>
                    <template #dropdown>
                      <el-dropdown-menu>
                        <el-dropdown-item command="destroy" style="color: var(--el-color-danger)">
                          销毁实例
                        </el-dropdown-item>
                      </el-dropdown-menu>
                    </template>
                  </el-dropdown>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </template>

      <!-- 2. 无数据/未选择时显示 -->
      <el-empty v-else-if="!loading" description="请从左侧选择一个业务系统">
        <template #extra>
          <div v-if="targetSystemId" style="color: #999; font-size: 12px;">
            系统 ID: {{ targetSystemId }} (未找到数据)
          </div>
        </template>
      </el-empty>
    </div>

    <!-- ========================================= -->
    <!-- 弹窗区域 (Dialogs) -->
    <!-- ========================================= -->

    <!-- 弹窗 1: 添加标准组件 -->
    <el-dialog 
      v-model="addModDialog.visible" 
      title="添加服务组件" 
      width="600px"
      destroy-on-close
    >
      <el-form label-width="100px" :model="addModDialog" size="small">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="组件名称">
              <el-input v-model="addModDialog.moduleName" placeholder="例如: 核心API" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="启动顺序">
               <el-input-number v-model="addModDialog.startOrder" :min="1" :max="99" />
               <div style="font-size:12px; color:#999">越小越先启动</div>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="服务包">
           <el-select 
             v-model="addModDialog.selectedPkg" 
             @change="updateModVersions" 
             style="width:100%"
             placeholder="请选择服务包"
           >
             <el-option v-for="p in packages" :key="p.name" :label="p.name" :value="p" />
           </el-select>
        </el-form-item>
        <el-form-item label="版本">
           <el-select v-model="addModDialog.version" style="width:100%" placeholder="请选择版本">
             <el-option v-for="v in addModDialog.versions" :key="v" :label="v" :value="v" />
           </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="addModDialog.desc" placeholder="备注信息" />
        </el-form-item>

        <el-divider content-position="left">健康检查覆盖 (可选)</el-divider>
        <div style="margin-bottom: 10px; color: #999; font-size: 12px; padding-left: 20px;">
          若不填写，将使用服务包中 service.json 的默认配置。
        </div>

        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="检测类型">
               <el-select v-model="addModDialog.readinessType" clearable placeholder="默认">
                 <el-option label="TCP端口" value="tcp" />
                 <el-option label="HTTP请求" value="http" />
                 <el-option label="固定延时" value="time" />
               </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="16">
            <el-form-item label="检测目标">
               <el-input v-model="addModDialog.readinessTarget" placeholder="e.g. :8080 or /health" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button type="primary" @click="addModule">确定</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗 2: 部署实例 -->
    <el-dialog v-model="deployDialog.visible" title="部署实例" width="400px">
      <div class="deploy-confirm-info">
        <p>服务：<b>{{ deployDialog.serviceName }}</b> (v{{ deployDialog.version }})</p>
      </div>
      <el-form label-width="80px">
        <el-form-item label="目标节点">
           <el-select v-model="deployDialog.nodeID" placeholder="请选择或自动调度" style="width: 100%">
             <!-- 选项 1: 自动选择 -->
             <el-option 
                label="🤖 自动选择 (负载最低)" 
                value="auto" 
                style="font-weight: bold; color: var(--el-color-primary);"
             />
             <!-- 选项 2: 在线节点列表 (使用 ID) -->
             <el-option 
               v-for="n in availableNodes" 
               :key="n.id" 
               :label="`${n.hostname} (${n.ip})`" 
               :value="n.id" 
             />
           </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" @click="deployInstance" :loading="deployDialog.loading">确定部署</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗 3: 纳管外部服务 (已修复适配 NodeID) -->
    <el-dialog v-model="adoptDialog.visible" title="纳管外部服务" width="500px">
      <el-form label-width="100px" size="small" :model="adoptForm">
        <el-form-item label="服务名称">
          <el-input v-model="adoptForm.name" placeholder="例如: 遗留网关" />
        </el-form-item>
        <el-form-item label="所在节点">
           <el-select v-model="adoptForm.nodeID" placeholder="选择目标服务器" style="width:100%">
             <el-option 
               v-for="n in availableNodes" 
               :key="n.id" 
               :label="`${n.hostname} (${n.ip})`" 
               :value="n.id" 
             />
           </el-select>
        </el-form-item>
        <el-divider content-position="left">运行配置</el-divider>
        <el-form-item label="工作目录">
          <el-input v-model="adoptForm.workDir" placeholder="绝对路径，如 /opt/nginx" />
        </el-form-item>
        <el-form-item label="启动命令">
          <el-input v-model="adoptForm.startCmd" placeholder="例如: ./nginx 或 start.bat" />
        </el-form-item>
        <el-form-item label="进程策略">
          <el-radio-group v-model="adoptForm.pidStrategy">
            <el-radio label="spawn">直接启动 (EXE)</el-radio>
            <el-radio label="match">脚本启动 + 查找 (Script)</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="进程关键字" v-if="adoptForm.pidStrategy === 'match'">
          <el-input v-model="adoptForm.processName" placeholder="进程名，如 nginx.exe" />
        </el-form-item>
        <el-form-item label="停止命令">
          <el-input v-model="adoptForm.stopCmd" placeholder="可选，如 ./nginx -s stop" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" size="small" @click="registerExternal" :loading="adoptDialog.loading">确定纳管</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗 4: 导出系统 -->
    <el-dialog v-model="exportDialog.visible" title="导出单机便携版" width="450px">
      <div class="export-body">
        <el-alert
          title="功能说明"
          type="info"
          :closable="false"
          description="将系统所需的所有服务包、配置和启动器打包为一个 ZIP 文件。解压后可脱离 Master 独立运行。"
          show-icon
          style="margin-bottom: 20px"
        />
        <el-form label-width="100px">
          <el-form-item label="目标平台">
            <el-radio-group v-model="exportDialog.os">
              <el-radio label="linux" border>Linux (x64)</el-radio>
              <el-radio label="windows" border>Windows (x64)</el-radio>
            </el-radio-group>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="exportDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="confirmExport" :loading="exportDialog.loading">
          <el-icon style="margin-right: 5px"><Download /></el-icon> 开始导出
        </el-button>
      </template>
    </el-dialog>

    <!-- 日志查看器组件 -->
    <LogViewer 
      v-model="logDialog.visible" 
      :instance-id="logDialog.instId" 
      :instance-name="logDialog.instName" 
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  Plus, MoreFilled, More, Link, VideoPlay, VideoPause, Loading, 
  Document, Download, Setting 
} from '@element-plus/icons-vue'
import { wsStore } from '../store/wsStore' // 引入 WebSocket Store
import LogViewer from './LogViewer.vue'

// ==========================================
// 1. Props & Emits
// ==========================================

const props = defineProps({
  targetSystemId: {
    type: String,
    required: false,
    default: ''
  }
})

const emit = defineEmits(['refresh-systems'])

// ==========================================
// 2. 状态定义 (State)
// ==========================================

const currentSystem = ref(null)
const loading = ref(false)
const batchLoading = ref(false)
const fullData = ref([])
const packages = ref([])

// 弹窗状态
const addModDialog = reactive({ 
  visible: false, moduleName: '', selectedPkg: null, version: '', versions: [], desc: '', 
  startOrder: 1, readinessType: '', readinessTarget: '' 
})
const deployDialog = reactive({ 
  visible: false, targetModule: null, nodeID: '', serviceName: '', version: '', loading: false 
})
const adoptDialog = reactive({ visible: false, loading: false })
// 【修改】adoptForm 使用 nodeID
const adoptForm = reactive({ 
  name: '', nodeID: '', workDir: '', startCmd: '', stopCmd: '', pidStrategy: 'spawn', processName: '' 
})
const exportDialog = reactive({ visible: false, os: 'linux', loading: false })
const logDialog = reactive({ visible: false, instId: '', instName: '' })

// 列表列配置
const tableColumns = reactive([
  { label: '节点 IP', prop: 'ip', visible: true },
  { label: '状态', prop: 'status', visible: true },
  { label: 'PID', prop: 'pid', visible: true },
  { label: '启动时间', prop: 'uptime', visible: false },
  { label: 'CPU', prop: 'cpu', visible: true },
  { label: '内存', prop: 'mem', visible: true },
  { label: 'IO R/W', prop: 'io', visible: false },
])

const colConf = computed(() => {
  const conf = {}
  tableColumns.forEach(c => conf[c.prop] = c.visible)
  return conf
})

// 可用在线节点 (使用 WebSocket Store 数据)
const availableNodes = computed(() => {
  return wsStore.nodes.filter(n => n.status === 'online')
})

let timer = null

// ==========================================
// 3. 核心计算属性：树形数据 (Tree Data)
// ==========================================

const treeData = computed(() => {
  if (!currentSystem.value) return []
  
  // A. 标准组件及其实例
  const standardModules = (currentSystem.value.modules || []).map(mod => {
    // 筛选属于该模块的实例
    const instances = (currentSystem.value.instances || [])
      .filter(inst => 
        inst.service_name === mod.package_name && 
        inst.service_version === mod.package_version
      )
      .map(inst => ({ 
        ...inst, 
        rowType: 'instance', 
        id: inst.id 
      }))

    return { 
      ...mod, 
      rowType: 'module', 
      is_external: false, 
      children: instances 
    }
  })

  // B. 纳管组件 (无预定义 Module，按名称聚合)
  const externalInstances = (currentSystem.value.instances || []).filter(inst => inst.service_version === 'external')
  const extGroups = {} // { ServiceName: [Instance,...] }
  
  externalInstances.forEach(inst => {
    if (!extGroups[inst.service_name]) extGroups[inst.service_name] = []
    extGroups[inst.service_name].push({ ...inst, rowType: 'instance', id: inst.id })
  })

  const extModules = Object.keys(extGroups).map(name => ({
    id: `ext_group_${name}`, // 虚拟 ID
    module_name: name,
    package_name: 'External',
    package_version: '-',
    rowType: 'module',
    is_external: true,
    children: extGroups[name]
  }))

  return [...standardModules, ...extModules]
})

// ==========================================
// 4. 数据获取与监听 (Data Fetching)
// ==========================================

// 监听 Prop 变化，自动刷新
watch(() => props.targetSystemId, (newId) => {
  if (newId) {
    refreshData()
  } else {
    currentSystem.value = null
  }
})

const refreshData = async () => {
  if (!props.targetSystemId) {
    currentSystem.value = null
    return
  }
  
  loading.value = true
  try {
    const res = await request.get('/api/systems')
    fullData.value = res || []
    
    // 使用宽松比较 (==) 兼容 String/Number ID
    const found = fullData.value.find(s => s.id == props.targetSystemId)
    currentSystem.value = found || null
    
    if (!found) {
      console.warn("System not found in list:", props.targetSystemId)
    }
  } catch (e) {
    console.error("Refresh failed:", e)
  } finally {
    loading.value = false
  }
}

// ==========================================
// 5. 交互操作 (Interactions)
// ==========================================

// --- 批量操作 ---
const handleBatchAction = async (action) => {
  if (!currentSystem.value?.instances?.length) {
    return ElMessage.warning('无实例可操作')
  }

  let count = 0
  if (action === 'start') {
    count = currentSystem.value.instances.filter(i => i.status !== 'running').length
  } else {
    count = currentSystem.value.instances.filter(i => i.status === 'running').length
  }
  
  if (count === 0) return ElMessage.info('没有需要操作的实例')

  try {
    await ElMessageBox.confirm(
      `确定要${action === 'start' ? '启动' : '停止'} ${count} 个实例吗？`,
      '批量操作确认',
      { type: 'warning', confirmButtonText: '确定', cancelButtonText: '取消' }
    )
    
    batchLoading.value = true
    await request.post('/api/systems/action', { 
      system_id: currentSystem.value.id, 
      action 
    })
    ElMessage.success('批量指令已下发')
    setTimeout(refreshData, 1500)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
  } finally {
    batchLoading.value = false
  }
}

// --- 系统级操作 ---
const handleCommand = (cmd) => {
  if (cmd === 'delete') handleDeleteSystem()
  else if (cmd === 'export') openExportDialog()
}

const handleDeleteSystem = async () => {
  try {
    await ElMessageBox.confirm(
      `确定删除系统 "${currentSystem.value.name}"? 此操作不可恢复！`, 
      '删除确认', 
      { type: 'error' }
    )
    await request.post('/api/systems/delete', { id: currentSystem.value.id })
    ElMessage.success('已删除')
    emit('refresh-systems') // 通知父组件刷新列表
  } catch(e) { /* ignore cancel */ }
}

const openExportDialog = () => {
  exportDialog.visible = true
}

const confirmExport = async () => {
  exportDialog.loading = true
  try {
    const res = await request.get('/api/systems/export', {
      params: { id: currentSystem.value.id, os: exportDialog.os },
      responseType: 'blob'
    })
    const url = window.URL.createObjectURL(new Blob([res.data], {type: 'application/zip'}))
    const link = document.createElement('a')
    link.href = url
    link.setAttribute('download', `export_${currentSystem.value.name}.zip`)
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url) // 释放资源
    exportDialog.visible = false
    ElMessage.success("导出请求已发送")
  } catch(e) {
    ElMessage.error('导出失败')
  } finally {
    exportDialog.loading = false
  }
}

// --- 组件管理 (Add/Delete Module) ---
const openAddModuleDialog = async () => { 
  addModDialog.visible = true
  const res = await request.get('/api/packages')
  packages.value = res || []
}

const updateModVersions = () => { 
  if (addModDialog.selectedPkg) {
    addModDialog.versions = addModDialog.selectedPkg.versions || []
    addModDialog.version = addModDialog.versions[0] || ''
    addModDialog.moduleName = addModDialog.moduleName || addModDialog.selectedPkg.name
  }
}

const addModule = async () => {
  try {
    await request.post('/api/systems/module/add', {
      system_id: currentSystem.value.id,
      module_name: addModDialog.moduleName,
      package_name: addModDialog.selectedPkg.name,
      package_version: addModDialog.version,
      description: addModDialog.desc,
      start_order: addModDialog.startOrder,
      readiness_type: addModDialog.readinessType,
      readiness_target: addModDialog.readinessTarget,
      readiness_timeout: 30
    })
    addModDialog.visible = false
    refreshData()
    ElMessage.success('组件添加成功')
  } catch(e) { /* interceptor handles error */ }
}

const deleteModule = async (moduleId) => { 
  try {
    await request.post('/api/systems/module/delete', { id: moduleId })
    ElMessage.success('组件已移除')
    refreshData()
  } catch(e) { ElMessage.error('删除失败') }
}

// --- 部署实例 (Deploy) ---
const openDeployDialog = (mod) => { 
  deployDialog.visible = true
  deployDialog.targetModule = mod
  deployDialog.serviceName = mod.package_name
  deployDialog.version = mod.package_version
  deployDialog.nodeID = 'auto' 
}

const deployInstance = async () => { 
  if (!deployDialog.nodeID) return ElMessage.warning('请选择目标节点')
  deployDialog.loading = true
  try {
    // 构造请求，兼容后端 NodeID 逻辑
    const payload = {
      system_id: currentSystem.value.id,
      service_name: deployDialog.targetModule.package_name,
      service_version: deployDialog.targetModule.package_version,
      // 如果是 'auto'，传给后端逻辑处理，否则传具体的 NodeID
      node_id: deployDialog.nodeID === 'auto' ? '' : deployDialog.nodeID
    }
    
    await request.post('/api/deploy', payload)
    
    deployDialog.visible = false
    ElMessage.success('部署指令已下发')
    setTimeout(refreshData, 1500)
  } catch(e) { 
    ElMessage.error('部署失败: ' + (e.message || e)) 
  } finally { 
    deployDialog.loading = false 
  }
}

// --- 纳管服务 (Adopt) ---
const openAdoptDialog = () => { 
  adoptDialog.visible = true
  // 重置表单，注意 reset nodeID
  Object.assign(adoptForm, { name: '', nodeID: '', workDir: '', startCmd: '', stopCmd: '', pidStrategy: 'spawn', processName: '' })
}

const registerExternal = async () => {
  // 【修改】校验 nodeID
  if (!adoptForm.name || !adoptForm.nodeID || !adoptForm.startCmd) {
    return ElMessage.warning('请补全必填信息')
  }
  adoptDialog.loading = true
  try {
    // 【修改】传递 node_id
    await request.post('/api/deploy/external', { 
      system_id: currentSystem.value.id, 
      node_id: adoptForm.nodeID,
      config: {
        name: adoptForm.name,
        work_dir: adoptForm.workDir,
        start_cmd: adoptForm.startCmd,
        stop_cmd: adoptForm.stopCmd,
        pid_strategy: adoptForm.pidStrategy,
        process_name: adoptForm.processName
      }
    })
    adoptDialog.visible = false
    refreshData()
    ElMessage.success('纳管成功')
  } catch(e) { 
    ElMessage.error('纳管失败: ' + (e.message || e)) 
  } finally { 
    adoptDialog.loading = false 
  }
}

// --- 实例操作 (Start/Stop/Log) ---
const handleAction = async (id, action) => {
  try {
    await request.post('/api/instance/action', { instance_id: id, action })
    ElMessage.success('指令已发送')
    if (action === 'destroy') setTimeout(refreshData, 500)
  } catch(e) {
    ElMessage.error('操作失败: ' + e.message)
  }
}

const handleInstanceCommand = (cmd, id) => {
  if (cmd === 'destroy') {
    ElMessageBox.confirm('确定销毁实例？', '警告', { type: 'warning' })
      .then(() => handleAction(id, 'destroy'))
  }
}

// 修复 openLog: 使用 getNodeIP 显示真实 IP
const openLog = (row) => { 
  logDialog.instId = row.id
  logDialog.instName = `${row.service_name}(${getNodeIP(row.node_ip)})`
  logDialog.visible = true 
}

// ==========================================
// 6. 辅助函数 (Utils)
// ==========================================

const formatTime = (ts) => {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// 核心：将 NodeID 转换为 IP 用于显示
const getNodeIP = (id) => {
  if (!id) return '-'
  const node = wsStore.nodes.find(n => n.id === id)
  if (node) return node.ip
  // 如果找不到，返回原 ID（可能是旧数据或节点已离线）
  return id
}

// ==========================================
// 7. 生命周期 (Lifecycle)
// ==========================================

onMounted(() => {
  if (props.targetSystemId) {
    refreshData()
  }
  // 启动定时刷新 (3秒一次)
  timer = setInterval(refreshData, 3000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
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

.sys-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* Header 区域 */
.content-header {
  padding: 10px 20px;
  border-bottom: 1px solid var(--el-border-color-light);
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: var(--el-bg-color);
  height: 50px;
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.sys-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.sys-id-tag {
  font-family: monospace;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 6px;
}

/* 表格容器 */
.table-card {
  border: none;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: transparent;
}

/* 覆盖 Card Body 样式 */
.table-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0; 
}

/* 样式修复：移除表格内边框，调整内边距 */
:deep(.custom-table .el-table__inner-wrapper::before) { display: none; }
:deep(.custom-table .el-table__cell) { padding: 6px 0; }

/* 树形表格图标对齐 */
:deep(.tree-col .cell) {
  display: flex;
  align-items: center;
}

.cell-content {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
}

.module-name {
  font-weight: 700;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.instance-count {
  color: var(--el-text-color-secondary);
  margin-left: 4px;
  font-size: 12px;
}

.pkg-hint {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  font-weight: normal;
}

.tag-external {
  margin-left: 8px;
  font-size: 10px;
  background: #e6a23c;
  color: #fff;
  padding: 1px 4px;
  border-radius: 2px;
}

.inst-id {
  font-family: monospace;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-left: 24px;
}

/* 通用文本样式 */
.mono-text { font-family: Consolas, monospace; font-size: 12px; }
.text-secondary { color: var(--el-text-color-secondary); }
.text-primary { color: var(--el-color-primary); }
.text-gray { color: #999; }
.text-xs { font-size: 12px; }
.text-placeholder { color: var(--el-text-color-placeholder); }

/* 状态样式 */
.status-text {
  font-weight: 500;
  font-size: 12px;
}
.status-text.running { color: var(--el-color-success); }
.status-text.stopped { color: var(--el-color-warning); }
.status-text.error { color: var(--el-color-danger); }
.status-text.deploying { color: var(--el-color-primary); animation: pulse 1.5s infinite; }

@keyframes pulse {
  0% { opacity: 1; }
  50% { opacity: 0.5; }
  100% { opacity: 1; }
}

.action-more {
  cursor: pointer;
  color: var(--el-color-primary);
  font-size: 14px;
  margin-left: 4px;
  vertical-align: middle;
}

.col-setting { padding: 5px 12px; }
:deep(.col-no-wrap .cell) { white-space: nowrap !important; }

/* 弹窗样式 */
.deploy-confirm-info { margin-bottom: 20px; font-size: 14px; color: var(--el-text-color-regular); }
.export-body { padding: 0 10px; }
.tip-text { font-size: 12px; color: #999; }
</style>