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

            <!-- 列 2: 节点 IP -->
            <el-table-column v-if="colConf.ip" label="节点 IP" width="140">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance'" class="mono-text text-primary">
                  {{ getNodeIP(scope.row.node_id) }}
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
                    @click="openEditModuleDialog(scope.row)"
                  >
                    编辑
                  </el-button>
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
      :title="addModDialog.isEdit ? '编辑服务组件' : '添加服务组件'"  
      width="650px"
      destroy-on-close
      top="5vh"
    >
      <el-form label-width="100px" :model="addModDialog" size="small">
        
        <!-- 基础信息 (保留) -->
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="组件名称" required>
              <el-input v-model="addModDialog.moduleName" placeholder="例如: 核心API" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="启动顺序">
               <el-input-number v-model="addModDialog.startOrder" :min="1" :max="99" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="服务包" required>
           <el-select 
             v-model="addModDialog.selectedPkg" 
             @change="updateModVersions" 
             style="width:100%"
             value-key="name"
             placeholder="请选择"
           >
             <el-option v-for="p in packages" :key="p.name" :label="p.name" :value="p" />
           </el-select>
        </el-form-item>
        <el-form-item label="版本" required>
           <el-select v-model="addModDialog.version" style="width:100%">
             <el-option v-for="v in addModDialog.versions" :key="v" :label="v" :value="v" />
           </el-select>
        </el-form-item>
        
        <!-- [新增] 配置文件挂载区域 -->
        <el-divider content-position="left">
          <el-icon><DocumentCopy /></el-icon> 
          <span style="margin-left: 8px; font-weight: bold;">配置文件注入</span>
        </el-divider>

        <div class="config-mounts-container">
          <!-- 列表过渡动画 -->
          <transition-group name="list">
            <div v-for="(item, index) in addModDialog.configMounts" :key="index" class="mount-card">
              <div class="mount-row">
                <div class="mount-col-source">
                  <div class="mount-label">配置模板</div>
                  <el-select v-model="item.template_id" placeholder="选择模板" style="width: 100%">
                    <el-option 
                      v-for="tpl in templateOptions" 
                      :key="tpl.id" 
                      :label="tpl.name" 
                      :value="tpl.id" 
                    />
                  </el-select>
                </div>
                
                <div class="mount-arrow">
                  <el-icon><Right /></el-icon>
                </div>
                
                <div class="mount-col-target">
                  <div class="mount-label">目标挂载路径</div>
                  <el-input v-model="item.mount_path" placeholder="e.g. conf/app.yaml" style="width: 100%" />
                </div>
                
                <div class="mount-action">
                  <el-button type="danger" icon="Delete" circle plain @click="removeMount(index)" />
                </div>
              </div>
            </div>
          </transition-group>

          <!-- 空状态显示 -->
          <div v-if="addModDialog.configMounts.length === 0" class="mount-empty">
            <el-icon><InfoFilled /></el-icon>
            <span>暂未注入任何配置文件，将使用服务包默认配置</span>
          </div>
          
          <el-button 
            type="primary" 
            plain 
            icon="Plus" 
            class="add-mount-btn"
            @click="addMount"
          >
            添加配置挂载项目
          </el-button>
        </div>

        <!-- 健康检查 (保留) -->
        <el-divider content-position="left">健康检查覆盖</el-divider>
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

    <!-- 弹窗 3: 纳管外部服务 -->
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
import { ref, reactive, computed, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  Plus, MoreFilled, More, Link, VideoPlay, VideoPause, Loading, 
  Document, Download, DocumentCopy, Right, InfoFilled
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

// [修改] 移除 fullData ref，不再从 HTTP 获取全量数据
const loading = ref(false)
const batchLoading = ref(false)
const packages = ref([])

// 弹窗状态
const addModDialog = reactive({ 
  visible: false,
  isEdit: false, // 新增：模式标记
  id: '',        // 新增：编辑时需要的 ID
  moduleName: '', 
  selectedPkg: null, 
  version: '', 
  versions: [], 
  desc: '', 
  startOrder: 1, 
  readinessType: '', 
  readinessTarget: '',
  configMounts: [] 
})

const templateOptions = ref([]) // 缓存模板列表

const deployDialog = reactive({ 
  visible: false, targetModule: null, nodeID: '', serviceName: '', version: '', loading: false 
})
const adoptDialog = reactive({ visible: false, loading: false })
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

// ==========================================
// 3. 核心计算属性 (Computed)
// ==========================================

// [关键修改] currentSystem 改为计算属性，响应 wsStore 的变化
const currentSystem = computed(() => {
  if (!props.targetSystemId) return null
  // 从 Store 中查找
  return wsStore.systems.find(s => s.id == props.targetSystemId) || null
})

// 树形数据 (Tree Data)
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
// 4. 交互操作 (Interactions)
// ==========================================

// [修改] 手动刷新逻辑
const refreshData = async () => {
  loading.value = true
  try {
    const res = await request.get('/api/systems')
    wsStore.systems = res || [] // 主动更新 Store
    ElMessage.success('数据已刷新')
  } catch (e) {
    // ...
  } finally {
    loading.value = false
  }
}

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
    // WS 会自动推送状态变更，不需要 setTimeout refreshData
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
    
    // 手动刷新一次列表，并通知父组件导航
    await refreshData()
    emit('refresh-systems') 
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
    window.URL.revokeObjectURL(url) 
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
  addModDialog.isEdit = false
  addModDialog.id = ''
  addModDialog.moduleName = ''
  addModDialog.startOrder = 1
  addModDialog.configMounts = []
  addModDialog.selectedPkg = null
  
  // 并行获取服务包和模板
  const [pkgRes, tplRes] = await Promise.all([
    request.get('/api/packages'),
    request.get('/api/templates')
  ])
  packages.value = pkgRes || []
  templateOptions.value = tplRes || []
  addModDialog.visible = true
}

const openEditModuleDialog = async (mod) => {
  // 1. 先获取基础数据源
  const [pkgRes, tplRes] = await Promise.all([
    request.get('/api/packages'),
    request.get('/api/templates')
  ])
  packages.value = pkgRes || []
  templateOptions.value = tplRes || []

  // 2. 填充表单
  addModDialog.isEdit = true
  addModDialog.id = mod.id
  addModDialog.moduleName = mod.module_name
  addModDialog.startOrder = mod.start_order
  addModDialog.readinessType = mod.readiness_type
  addModDialog.readinessTarget = mod.readiness_target
  addModDialog.desc = mod.description || ''
  
  // 深度拷贝挂载配置，防止修改时影响原始数据
  addModDialog.configMounts = JSON.parse(JSON.stringify(mod.config_mounts || []))

  // 匹配选中的包
  const pkg = packages.value.find(p => p.name === mod.package_name)
  if (pkg) {
    addModDialog.selectedPkg = pkg
    addModDialog.versions = pkg.versions || []
    addModDialog.version = mod.package_version
  }

  addModDialog.visible = true
}
// 3. 挂载操作逻辑
const addMount = () => {
  addModDialog.configMounts.push({ template_id: '', mount_path: '' })
}
const removeMount = (index) => {
  addModDialog.configMounts.splice(index, 1)
}
const updateModVersions = () => { 
  if (addModDialog.selectedPkg) {
    addModDialog.versions = addModDialog.selectedPkg.versions || []
    addModDialog.version = addModDialog.versions[0] || ''
    addModDialog.moduleName = addModDialog.moduleName || addModDialog.selectedPkg.name
  }
}

const addModule = async () => {
  if (!addModDialog.moduleName || !addModDialog.selectedPkg || !addModDialog.version) {
    return ElMessage.warning('请补全必填信息')
  }

  const payload = {
    id: addModDialog.id, // 编辑模式下有 ID
    system_id: currentSystem.value.id,
    module_name: addModDialog.moduleName,
    package_name: addModDialog.selectedPkg.name,
    package_version: addModDialog.version,
    description: addModDialog.desc,
    start_order: addModDialog.startOrder,
    readiness_type: addModDialog.readinessType,
    readiness_target: addModDialog.readinessTarget,
    readiness_timeout: 30,
    config_mounts: addModDialog.configMounts
  }

  try {
    const url = addModDialog.isEdit ? '/api/systems/module/update' : '/api/systems/module/add'
    await request.post(url, payload)
    addModDialog.visible = false
    refreshData() 
    ElMessage.success(addModDialog.isEdit ? '组件更新成功' : '组件添加成功')
  } catch(e) { }
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
    const payload = {
      system_id: currentSystem.value.id,
      service_name: deployDialog.targetModule.package_name,
      service_version: deployDialog.targetModule.package_version,
      node_id: deployDialog.nodeID
    }
    await request.post('/api/deploy', payload)
    
    deployDialog.visible = false
    ElMessage.success('部署指令已下发')
    // 不再需要 setTimeout，WS 会推送 'deploying' 状态
  } catch(e) { 
    ElMessage.error('部署失败: ' + (e.message || e)) 
  } finally { 
    deployDialog.loading = false 
  }
}

// --- 纳管服务 (Adopt) ---
const openAdoptDialog = () => { 
  adoptDialog.visible = true
  Object.assign(adoptForm, { name: '', nodeID: '', workDir: '', startCmd: '', stopCmd: '', pidStrategy: 'spawn', processName: '' })
}

const registerExternal = async () => {
  if (!adoptForm.name || !adoptForm.nodeID || !adoptForm.startCmd) {
    return ElMessage.warning('请补全必填信息')
  }
  adoptDialog.loading = true
  try {
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

const openLog = (row) => { 
  logDialog.instId = row.id
  logDialog.instName = `${row.service_name}(${getNodeIP(row.node_id)})`
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

const getNodeIP = (id) => {
  if (!id) return '-'
  const node = wsStore.nodes.find(n => n.id === id)
  if (node) return node.ip
  return id
}

// ==========================================
// 7. 生命周期 (Lifecycle)
// ==========================================

onMounted(() => {
  // 初次加载时，尝试获取一次数据，以防 Store 为空
  if (wsStore.systems.length === 0) {
    refreshData()
  }
})
// [修改] 移除 onUnmounted 和 timer 清理
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

.table-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0; 
}

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

/* [新增] 挂载区域样式 */
/* 配置文件注入区域容器 */
.config-mounts-container {
  background-color: var(--el-fill-color-lighter); /* 自动适配黑夜模式的浅色底 */
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 24px;
}

/* 每一行作为一个卡片 */
.mount-card {
  background-color: var(--el-bg-color); /* 黑夜模式下自动变深 */
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px 16px;
  margin-bottom: 12px;
  box-shadow: var(--el-box-shadow-light);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.mount-card:hover {
  border-color: var(--el-color-primary-light-5);
  transform: translateY(-2px);
  box-shadow: var(--el-box-shadow);
}

.mount-row {
  display: flex;
  align-items: flex-end; /* 标签在输入框上方，所以底部对齐 */
  gap: 12px;
}

.mount-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
  font-weight: bold;
  text-transform: uppercase;
}

.mount-col-source { flex: 2; }
.mount-col-target { flex: 3; }

.mount-arrow {
  padding-bottom: 8px; /* 对齐输入框中心 */
  color: var(--el-text-color-placeholder);
  font-size: 18px;
}

.mount-action {
  padding-bottom: 2px;
}

/* 空状态样式 */
.mount-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 20px;
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  border: 1px dashed var(--el-border-color-darker);
  border-radius: 6px;
  margin-bottom: 12px;
  gap: 8px;
}

/* 新增按钮美化 */
.add-mount-btn {
  width: 100%;
  border-style: dashed !important;
  background: transparent !important;
}

.add-mount-btn:hover {
  background: var(--el-color-primary-light-9) !important;
}

/* 列表增删动画 */
.list-enter-active,
.list-leave-active {
  transition: all 0.4s ease;
}
.list-enter-from,
.list-leave-to {
  opacity: 0;
  transform: translateX(30px);
}

/* 针对黑夜模式的微调 (如果 Element Plus 的变量不够完美) */
.dark .config-mounts-container {
  background-color: rgba(255, 255, 255, 0.02);
  border-color: #333;
}

.dark .mount-card {
  background-color: #1d1e1f; /* 更深的深灰色 */
}

/* 弹窗样式 */
.deploy-confirm-info { margin-bottom: 20px; font-size: 14px; color: var(--el-text-color-regular); }
.export-body { padding: 0 10px; }
.tip-text { font-size: 12px; color: #999; }
</style>