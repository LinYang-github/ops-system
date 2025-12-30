<template>
  <div class="view-container">
    
    <div class="sys-content" v-loading="loading">
      <template v-if="currentSystem">
        <!-- Header -->
        <div class="content-header">
          <div class="header-left">
            <h2 class="sys-title">{{ currentSystem.name }}</h2>
            <el-tag size="small" type="info" effect="plain" class="sys-id-tag">{{ currentSystem.id }}</el-tag>
          </div>
          
          <div class="header-right">
            <!-- 批量操作按钮 -->
            <el-button-group style="margin-right: 12px">
              <el-tooltip content="启动所有停止的实例" placement="bottom">
                <el-button size="small" type="success" icon="VideoPlay" @click="handleBatchAction('start')" :loading="batchLoading">全启</el-button>
              </el-tooltip>
              <el-tooltip content="停止所有运行的实例" placement="bottom">
                <el-button size="small" type="warning" icon="VideoPause" @click="handleBatchAction('stop')" :loading="batchLoading">全停</el-button>
              </el-tooltip>
            </el-button-group>

            <!-- 列配置 -->
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
            
            <el-button type="primary" size="small" icon="Plus" @click="openAddModuleDialog">标准组件</el-button>
            <el-button type="warning" size="small" icon="Link" @click="openAdoptDialog">纳管服务</el-button>
            
            <el-button icon="Refresh" size="small" circle @click="refreshData" />
            
            <!-- 更多操作下拉菜单 -->
            <el-dropdown trigger="click" @command="handleCommand" style="margin-left: 8px">
              <el-button link size="small"><el-icon><MoreFilled /></el-icon></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <!-- 导出按钮 -->
                  <el-dropdown-item command="export" icon="Download">导出单机版</el-dropdown-item>
                  <el-dropdown-item command="delete" icon="Delete" style="color: var(--el-color-danger)" divided>删除系统</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>

        <!-- 2. 核心表格 -->
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
            <!-- 1. 树结构列 -->
            <el-table-column 
              label="组件名称 / 实例 ID" 
              min-width="260" 
              show-overflow-tooltip
              class-name="tree-col"
            >
              <template #default="scope">
                <div class="cell-content">
                  <template v-if="scope.row.rowType === 'module'">
                    <span class="module-name">{{ scope.row.module_name }}</span>
                    <span class="instance-count" v-if="scope.row.children.length > 0">({{ scope.row.children.length }})</span>
                    <span v-if="scope.row.is_external" class="tag-external">EXTERNAL</span>
                    <span v-else class="pkg-hint">{{ scope.row.package_name }} v{{ scope.row.package_version }}</span>
                  </template>
                  <template v-else>
                    <span class="inst-id">{{ scope.row.id }}</span>
                  </template>
                </div>
              </template>
            </el-table-column>

            <!-- 其他列 -->
            <el-table-column v-if="colConf.ip" label="节点 IP" width="140">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance'" class="mono-text text-primary">{{ scope.row.node_ip }}</span>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.status" label="状态" width="90">
              <template #default="scope">
                <div v-if="scope.row.rowType === 'instance'" class="status-cell">
                  <el-icon v-if="scope.row.status === 'deploying'" class="is-loading" color="#409EFF" style="margin-right:4px"><Loading /></el-icon>
                  <span :class="['status-text', scope.row.status]">{{ scope.row.status }}</span>
                </div>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.pid" label="PID" width="80" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">{{ scope.row.pid }}</span>
                <span v-else-if="scope.row.rowType === 'instance'" class="text-placeholder">-</span>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.uptime" label="启动时间" width="160" class-name="col-no-wrap">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text text-gray text-xs">{{ formatTime(scope.row.uptime) }}</span>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.cpu" label="CPU" width="80" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">{{ (scope.row.cpu_usage || 0).toFixed(1) }}%</span>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.mem" label="内存" width="90" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text">{{ (scope.row.mem_usage || 0) }} MB</span>
              </template>
            </el-table-column>

            <el-table-column v-if="colConf.io" label="IO R/W" width="130" align="right">
              <template #default="scope">
                <span v-if="scope.row.rowType === 'instance' && scope.row.status === 'running'" class="mono-text text-gray text-xs">{{ scope.row.io_read }}/{{ scope.row.io_write }} KB</span>
              </template>
            </el-table-column>

            <el-table-column label="操作" width="150" fixed="right" align="right">
              <template #default="scope">
                <div v-if="scope.row.rowType === 'module'">
                  <el-button v-if="!scope.row.is_external" link type="primary" size="small" @click="openDeployDialog(scope.row)">部署</el-button>
                  <el-popconfirm v-if="!scope.row.is_external" title="删除定义?" @confirm="deleteModule(scope.row.id)">
                    <template #reference><el-button link type="info" size="small">删除</el-button></template>
                  </el-popconfirm>
                </div>
                <div v-else>
                  <el-button v-if="scope.row.status !== 'running'" link type="success" size="small" @click="handleAction(scope.row.id, 'start')">启动</el-button>
                  <el-button v-if="scope.row.status === 'running'" link type="warning" size="small" @click="handleAction(scope.row.id, 'stop')">停止</el-button>
                  <el-dropdown trigger="click" size="small" @command="(cmd) => handleInstanceCommand(cmd, scope.row.id)">
                    <span class="el-dropdown-link action-more"><el-icon><More /></el-icon></span>
                    <template #dropdown>
                      <el-dropdown-menu>
                        <el-dropdown-item command="destroy" style="color: var(--el-color-danger)">销毁实例</el-dropdown-item>
                      </el-dropdown-menu>
                    </template>
                  </el-dropdown>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </template>
      <el-empty v-else description="请选择系统" />
    </div>

    <!-- 弹窗1-3: 标准/部署/纳管 -->
    <el-dialog v-model="addModDialog.visible" title="添加标准组件" width="350px">
        <el-form label-width="70px" size="small">
            <el-form-item label="名称"><el-input v-model="addModDialog.moduleName" /></el-form-item>
            <el-form-item label="服务包">
                <el-select v-model="addModDialog.selectedPkg" @change="updateModVersions" style="width:100%">
                    <el-option v-for="p in packages" :key="p.name" :label="p.name" :value="p" />
                </el-select>
            </el-form-item>
            <el-form-item label="版本">
                <el-select v-model="addModDialog.version" style="width:100%">
                    <el-option v-for="v in addModDialog.versions" :key="v" :label="v" :value="v" />
                </el-select>
            </el-form-item>
        </el-form>
        <template #footer><el-button type="primary" size="small" @click="addModule">确定</el-button></template>
    </el-dialog>

    <el-dialog v-model="deployDialog.visible" title="部署实例" width="350px">
        <el-form label-width="70px" size="small">
            <el-form-item label="节点">
                <el-select v-model="deployDialog.nodeIP" style="width:100%" placeholder="请选择在线节点">
                    <el-option label="🤖 自动选择 (负载最低)" value="auto" style="font-weight: bold; color: var(--el-color-primary);" />
                    <el-option v-for="n in availableNodes" :key="n.ip" :label="`${n.hostname} (${n.ip})`" :value="n.ip" />
                </el-select>
            </el-form-item>
        </el-form>
        <template #footer><el-button type="primary" size="small" @click="deployInstance" :loading="deployDialog.loading">部署</el-button></template>
    </el-dialog>

    <el-dialog v-model="adoptDialog.visible" title="纳管外部服务" width="500px">
      <el-form label-width="100px" size="small" :model="adoptForm">
        <el-form-item label="服务名称"><el-input v-model="adoptForm.name" placeholder="例如: 遗留网关" /></el-form-item>
        <el-form-item label="所在节点">
           <el-select v-model="adoptForm.nodeIP" placeholder="选择目标服务器" style="width:100%">
             <el-option v-for="n in availableNodes" :key="n.ip" :label="`${n.hostname} (${n.ip})`" :value="n.ip" />
           </el-select>
        </el-form-item>
        <el-divider content-position="left">运行配置</el-divider>
        <el-form-item label="工作目录"><el-input v-model="adoptForm.workDir" placeholder="绝对路径，如 /opt/nginx" /></el-form-item>
        <el-form-item label="启动命令"><el-input v-model="adoptForm.startCmd" placeholder="例如: ./nginx 或 start.bat" /></el-form-item>
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

    <!-- 【新增】弹窗4：导出系统 -->
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
              <el-radio border label="linux">Linux (x64)</el-radio>
              <el-radio border label="windows">Windows (x64)</el-radio>
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

  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Refresh, ArrowDown, Setting, MoreFilled, More, Link, InfoFilled, VideoPlay, VideoPause, Loading, Download } from '@element-plus/icons-vue'
import { wsStore } from '../store/wsStore'

const props = defineProps(['targetSystemId'])
const emit = defineEmits(['refresh-systems'])

const currentSystem = ref(null)
const loading = ref(false)
const batchLoading = ref(false)
const fullData = ref([])
const packages = ref([])

const addModDialog = reactive({ visible: false, moduleName: '', selectedPkg: null, version: '', versions: [] })
const deployDialog = reactive({ visible: false, targetModule: null, nodeIP: '', loading: false })
const adoptDialog = reactive({ visible: false, loading: false })
const adoptForm = reactive({ name: '', nodeIP: '', workDir: '', startCmd: '', stopCmd: '', pidStrategy: 'spawn', processName: '' })
const exportDialog = reactive({ visible: false, os: 'linux', loading: false })

// 动态列配置
const tableColumns = reactive([
  { label: '节点 IP', prop: 'ip', visible: true },
  { label: '状态', prop: 'status', visible: true },
  { label: 'PID', prop: 'pid', visible: true },
  { label: '启动时间', prop: 'uptime', visible: false },
  { label: 'CPU', prop: 'cpu', visible: true },
  { label: '内存', prop: 'mem', visible: true },
  { label: 'IO', prop: 'io', visible: false },
])

const colConf = computed(() => {
  const conf = {}
  tableColumns.forEach(c => conf[c.prop] = c.visible)
  return conf
})

const availableNodes = computed(() => wsStore.nodes.filter(n => n.status === 'online'))

let timer = null

const treeData = computed(() => {
  if (!currentSystem.value) return []
  
  const standardModules = currentSystem.value.modules.map(mod => {
    const instances = currentSystem.value.instances.filter(inst => 
      inst.service_name === mod.package_name && 
      inst.service_version === mod.package_version
    ).map(inst => ({ ...inst, rowType: 'instance', id: inst.id }))

    return { ...mod, rowType: 'module', is_external: false, children: instances }
  })

  const externalInstances = currentSystem.value.instances.filter(inst => inst.service_version === 'external')
  const extGroups = {}
  externalInstances.forEach(inst => {
    if (!extGroups[inst.service_name]) extGroups[inst.service_name] = []
    extGroups[inst.service_name].push({ ...inst, rowType: 'instance', id: inst.id })
  })

  const extModules = Object.keys(extGroups).map(name => ({
    id: `ext_group_${name}`,
    module_name: name,
    package_name: 'External',
    package_version: '-',
    rowType: 'module',
    is_external: true,
    children: extGroups[name]
  }))

  return [...standardModules, ...extModules]
})

watch(() => props.targetSystemId, (newId) => {
  if (newId) refreshData()
  else currentSystem.value = null
})

// --- API 方法 ---

const refreshData = async () => {
  if (!props.targetSystemId) return
  try {
    const data = await request.get('/api/systems')
    fullData.value = data || []
    const found = fullData.value.find(s => s.id === props.targetSystemId)
    currentSystem.value = found || null
  } catch (e) {} finally { loading.value = false }
}

// 批量操作
const handleBatchAction = async (action) => {
  if (!currentSystem.value || !currentSystem.value.instances.length) {
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
    await ElMessageBox.confirm(`确定要${action==='start'?'启动':'停止'} ${count} 个实例吗？`, '批量操作', { type: 'warning' })
  } catch { return }

  batchLoading.value = true
  try {
    await request.post('/api/systems/action', {
      system_id: currentSystem.value.id,
      action: action
    })
    ElMessage.success('批量指令已下发')
    setTimeout(refreshData, 1000)
  } catch(e) {}
  finally { batchLoading.value = false }
}

// 下拉菜单
const handleCommand = (cmd) => {
  if (cmd === 'delete') {
    ElMessageBox.confirm('确定删除系统?', '警告', { type: 'warning' }).then(async () => {
        await request.post('/api/systems/delete', { id: currentSystem.value.id })
        ElMessage.success('已删除')
        emit('refresh-systems')
    })
  } else if (cmd === 'export') {
    // 【修复】调用导出弹窗
    openExportDialog()
  }
}

const handleInstanceCommand = (cmd, id) => {
  if (cmd === 'destroy') {
    ElMessageBox.confirm('确定销毁? 文件将删除', '警告', { type: 'warning' })
      .then(() => handleAction(id, 'destroy'))
  }
}

// 模组 & 部署 & 纳管
const openAddModuleDialog = async () => { addModDialog.visible = true; const data = await request.get('/api/packages'); packages.value = data || [] }
const updateModVersions = () => { if(addModDialog.selectedPkg) addModDialog.versions = addModDialog.selectedPkg.versions; addModDialog.version = addModDialog.versions[0]; if(!addModDialog.moduleName) addModDialog.moduleName = addModDialog.selectedPkg.name }
const addModule = async () => { await request.post('/api/systems/module/add', { system_id: currentSystem.value.id, module_name: addModDialog.moduleName, package_name: addModDialog.selectedPkg.name, package_version: addModDialog.version, description: addModDialog.desc }); addModDialog.visible = false; refreshData() }
const deleteModule = async (id) => { await request.post('/api/systems/module/delete', { id }); refreshData() }

const openDeployDialog = async (mod) => { 
  deployDialog.visible = true; 
  deployDialog.targetModule = mod 
  deployDialog.nodeIP = 'auto' 
}

const deployInstance = async () => { 
  if(!deployDialog.nodeIP) return ElMessage.warning('请选择节点')
  deployDialog.loading = true; 
  try { 
    await request.post('/api/deploy', { 
      system_id: currentSystem.value.id, 
      node_ip: deployDialog.nodeIP, 
      service_name: deployDialog.targetModule.package_name, 
      service_version: deployDialog.targetModule.package_version 
    }); 
    ElMessage.success('指令已发送')
    deployDialog.visible = false; 
    setTimeout(refreshData, 500) 
  } catch(e) {} 
  finally { deployDialog.loading = false } 
}

const openAdoptDialog = () => {
  adoptDialog.visible = true
  adoptForm.name = ''
  adoptForm.nodeIP = ''
  adoptForm.workDir = ''
  adoptForm.startCmd = ''
  adoptForm.stopCmd = ''
  adoptForm.pidStrategy = 'spawn'
  adoptForm.processName = ''
}
const registerExternal = async () => {
  if(!adoptForm.name || !adoptForm.nodeIP || !adoptForm.startCmd) return ElMessage.warning('请补全信息')
  adoptDialog.loading = true
  try {
    await request.post('/api/deploy/external', {
      system_id: currentSystem.value.id,
      node_ip: adoptForm.nodeIP,
      config: {
        name: adoptForm.name,
        work_dir: adoptForm.workDir,
        start_cmd: adoptForm.startCmd,
        stop_cmd: adoptForm.stopCmd,
        pid_strategy: adoptForm.pidStrategy,
        process_name: adoptForm.processName
      }
    })
    ElMessage.success('纳管成功')
    adoptDialog.visible = false
    refreshData()
  } catch(e) {}
  finally { adoptDialog.loading = false }
}

// 导出
const openExportDialog = () => {
  exportDialog.visible = true
  exportDialog.os = 'linux'
}
// 【修复】confirmExport 使用 request.get 并处理 blob
const confirmExport = async () => {
  exportDialog.loading = true
  try {
    const res = await request.get('/api/systems/export', {
      params: {
        id: currentSystem.value.id,
        os: exportDialog.os
      },
      responseType: 'blob'
    })

    const blob = new Blob([res.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url

    const contentDisposition = res.headers['content-disposition']
    let fileName = `export_${currentSystem.value.name}.zip`
    if (contentDisposition) {
      const fileNameMatch = contentDisposition.match(/filename="?([^"]+)"?/)
      if (fileNameMatch && fileNameMatch.length === 2) {
        fileName = fileNameMatch[1]
      }
    }

    link.setAttribute('download', fileName)
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    exportDialog.visible = false
    ElMessage.success("导出成功")
  } catch (e) {
    if (e.response && e.response.data instanceof Blob) {
        const reader = new FileReader()
        reader.onload = () => {
            try {
                const errorJson = JSON.parse(reader.result)
                ElMessage.error(errorJson.msg || "导出失败")
            } catch (err) {
                ElMessage.error("导出失败")
            }
        }
        reader.readAsText(e.response.data)
    } else {
        ElMessage.error("导出失败")
    }
  } finally {
    exportDialog.loading = false
  }
}

const handleAction = async (id, action) => { 
  try {
    await request.post('/api/instance/action', { instance_id: id, action }); 
    ElMessage.success('指令已发送')
    if(action==='destroy') setTimeout(refreshData, 500) 
  } catch(e) {}
}

const getStatusType = (s) => s==='running'?'success':(s==='stopped'?'info':(s==='deploying'?'primary':'danger'))
const formatTime = (ts) => { if(!ts) return '-'; return new Date(ts * 1000).toLocaleString() }

onMounted(() => {
  if(props.targetSystemId) refreshData()
  timer = setInterval(refreshData, 3000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
.sys-content { flex: 1; display: flex; flex-direction: column; overflow: hidden; }

/* Header */
.content-header { padding: 10px 20px; border-bottom: 1px solid var(--el-border-color-light); display: flex; justify-content: space-between; align-items: center; background: var(--el-bg-color); height: 50px; flex-shrink: 0;}
.header-left { display: flex; align-items: baseline; gap: 12px; }
.sys-title { margin: 0; font-size: 16px; font-weight: 600; color: var(--el-text-color-primary); }
.sys-id-tag { font-family: monospace; }
.header-right { display: flex; align-items: center; gap: 6px; }

/* 表格容器 */
.table-card { border: none; flex: 1; display: flex; flex-direction: column; overflow: hidden; background: transparent; }

:deep(.custom-table .el-table__inner-wrapper::before) { display: none; }
:deep(.custom-table .el-table__cell) { padding: 6px 0; }
:deep(.tree-col .cell) { display: flex; align-items: center; }

.cell-content { display: flex; align-items: center; flex: 1; min-width: 0; }
.module-name { font-weight: 700; font-size: 13px; color: var(--el-text-color-primary); }
.instance-count { color: var(--el-text-color-secondary); margin-left: 4px; font-size: 12px; }
.pkg-hint { margin-left: 8px; font-size: 12px; color: var(--el-text-color-placeholder); font-weight: normal; }
.tag-external { margin-left: 8px; font-size: 10px; background: #e6a23c; color: #fff; padding: 1px 4px; border-radius: 2px; }

.inst-id { font-family: monospace; color: var(--el-text-color-secondary); font-size: 12px; margin-left: 24px; }

.mono-text { font-family: Consolas, monospace; font-size: 12px; }
.text-secondary { color: var(--el-text-color-secondary); }
.text-primary { color: var(--el-color-primary); }
.text-xs { font-size: 12px; }
.text-placeholder { color: var(--el-text-color-placeholder); }

.status-text { font-weight: 500; font-size: 12px; }
.status-text.running { color: var(--el-color-success); }
.status-text.stopped { color: var(--el-color-warning); }
.status-text.error { color: var(--el-color-danger); }
.status-text.deploying { color: var(--el-color-primary); animation: pulse 1.5s infinite; }

@keyframes pulse { 0% { opacity: 1; } 50% { opacity: 0.5; } 100% { opacity: 1; } }

.action-more { cursor: pointer; color: var(--el-color-primary); font-size: 14px; margin-left: 4px; vertical-align: middle; }
.col-setting { padding: 5px 12px; }
:deep(.col-no-wrap .cell) { white-space: nowrap !important; }

.export-body { padding: 10px; }
</style>