<template>
  <div class="view-container">
    <div class="content-body" v-loading="loading">
      
      <el-card shadow="never" class="settings-card">
        <template #header>
          <div class="card-header">
            <div class="title-with-icon">
               <el-icon><Tools /></el-icon> 
               <span>系统参数配置</span>
            </div>
            <!-- 仅在非维护 Tab 显示保存按钮 -->
            <div class="card-actions" v-if="activeTab !== 'maintenance'">
               <el-button type="primary" icon="Check" @click="saveAllConfig" :loading="saving">保存所有配置</el-button>
            </div>
          </div>
        </template>

        <div class="form-container">
          <el-tabs type="border-card" v-model="activeTab" @tab-change="handleTabChange">
            
            <!-- ========================= -->
            <!-- Tab 1: 系统运行参数        -->
            <!-- ========================= -->
            <el-tab-pane label="运行参数" name="system">
              <el-form :model="form" label-width="180px" label-position="left">
                
                <div class="section-title">Master 控制侧</div>
                <el-form-item label="节点离线判定阈值">
                  <div class="input-wrapper">
                    <el-input-number v-model="form.logic.node_offline_threshold" :min="5" :max="3600" />
                    <span class="unit">秒</span>
                  </div>
                  <div class="tip">超过此时间未收到 Worker 心跳，Master 将其标记为 Offline。</div>
                </el-form-item>

                <el-form-item label="HTTP 请求超时">
                  <div class="input-wrapper">
                    <el-input-number v-model="form.logic.http_client_timeout" :min="1" :max="60" />
                    <span class="unit">秒</span>
                  </div>
                  <div class="tip">Master 主动请求 Worker 接口时的超时限制。</div>
                </el-form-item>

                <el-divider border-style="dashed" />

                <div class="section-title">Worker 代理侧</div>
                <div class="tip-box">
                  <el-icon><InfoFilled /></el-icon> 
                  <span>配置保存后，Worker 将在下一次心跳交互时自动同步并生效。</span>
                </div>

                <el-form-item label="心跳上报间隔">
                  <div class="input-wrapper">
                    <el-input-number v-model="form.worker.heartbeat_interval" :min="1" :max="60" />
                    <span class="unit">秒</span>
                  </div>
                </el-form-item>
                <el-form-item label="监控采集间隔">
                  <div class="input-wrapper">
                    <el-input-number v-model="form.worker.monitor_interval" :min="1" :max="60" />
                    <span class="unit">秒</span>
                  </div>
                </el-form-item>

                <el-divider border-style="dashed" />
                
                <div class="section-title">系统维护</div>
                <el-form-item label="操作日志保留天数">
                  <div class="input-wrapper">
                    <el-input-number v-model="form.log.retention_days" :min="1" :max="3650" />
                    <span class="unit">天</span>
                  </div>
                </el-form-item>
              </el-form>
            </el-tab-pane>

            <!-- ========================= -->
            <!-- Tab 2: Nacos 连接配置      -->
            <!-- ========================= -->
            <el-tab-pane label="配置中心 (Nacos)" name="nacos">
              <el-form :model="nacosForm" label-width="120px" label-position="left" style="max-width: 600px">
                <div class="section-title">连接设置</div>
                <div class="tip" style="margin-bottom: 20px;">
                  配置 Nacos Server 的连接信息。Master 将作为代理访问 Nacos API，无需 Worker 直接连接。
                </div>

                <el-form-item label="服务地址">
                  <el-input v-model="nacosForm.url" placeholder="http://127.0.0.1:8848">
                    <template #prepend>URL</template>
                  </el-input>
                </el-form-item>

                <el-form-item label="账号">
                  <el-input v-model="nacosForm.username" placeholder="nacos" />
                </el-form-item>

                <el-form-item label="密码">
                  <el-input v-model="nacosForm.password" type="password" show-password placeholder="如有更改请填写" />
                </el-form-item>
              </el-form>
            </el-tab-pane>

            <!-- ========================= -->
            <!-- Tab 3: 运维维护            -->
            <!-- ========================= -->
            <el-tab-pane label="运维维护" name="maintenance">
              <div class="maintenance-panel">
                
                <!-- 1. 存储清理区域 -->
                <div class="section-title">存储空间清理</div>
                <div class="action-row">
                  <div class="label-group">
                    <span class="main-label">清理节点下载缓存</span>
                    <span class="sub-label">删除 Worker 节点已下载的 ZIP 包，释放磁盘空间。</span>
                  </div>
                  <el-button type="warning" plain icon="Brush" @click="openCleanDialog">立即清理</el-button>
                </div>

                <div class="action-row" style="margin-top: 15px;">
                  <div class="label-group">
                    <span class="main-label">孤儿资源回收 (GC)</span>
                    <span class="sub-label">扫描并清理 Worker 节点上不再受 Master 管理的残留目录。</span>
                  </div>
                  <el-button type="danger" plain icon="Search" @click="openGcDialog">扫描孤儿</el-button>
                </div>

                <el-divider />

                <!-- 2. Agent 版本管理区域 -->
                <div class="section-title">Worker Agent 版本管理</div>
                
                <el-alert
                  title="升级说明"
                  type="info"
                  :closable="false"
                  show-icon
                  class="mb-20"
                >
                  <template #default>
                    在此上传最新编译的 Worker 二进制文件。上传后，点击“执行批量升级”即可向所有在线节点下发更新指令。
                    <br/>文件名将自动保存为规范名称 (例如: <b>worker_linux_arm64</b>, <b>worker_darwin_arm64</b>)。
                  </template>
                </el-alert>

                <!-- 制品列表 -->
                <el-table :data="artifactList" border style="width: 100%; margin-bottom: 20px;" v-loading="artifactLoading">
                  <el-table-column label="平台架构" width="180">
                    <template #default="scope">
                      <div style="display: flex; align-items: center; gap: 8px;">
                        <!-- Linux 图标 -->
                        <el-icon v-if="scope.row.key.includes('linux')"><Platform /></el-icon>
                        <!-- Windows 图标 -->
                        <el-icon v-else-if="scope.row.key.includes('windows')"><Monitor /></el-icon>
                        <!-- macOS 图标 [新增] -->
                        <el-icon v-else-if="scope.row.key.includes('darwin')"><Apple /></el-icon>
                        
                        <span style="font-weight: bold;">{{ scope.row.key }}</span>
                      </div>
                    </template>
                  </el-table-column>
                  
                  <el-table-column label="状态" width="100" align="center">
                    <template #default="scope">
                      <el-tag v-if="scope.row.exists" type="success" effect="dark">已就绪</el-tag>
                      <el-tag v-else type="info" effect="plain">未上传</el-tag>
                    </template>
                  </el-table-column>

                  <el-table-column label="文件信息" min-width="250">
                    <template #default="scope">
                      <div v-if="scope.row.exists" class="file-info">
                        <div><span class="label">Name:</span> {{ scope.row.filename }}</div>
                        <div><span class="label">Size:</span> {{ formatSize(scope.row.size) }}</div>
                        <div><span class="label">Time:</span> {{ formatTime(scope.row.mod_time) }}</div>
                      </div>
                      <span v-else class="text-gray">-</span>
                    </template>
                  </el-table-column>

                  <el-table-column label="操作" width="260" align="center">
                    <template #default="scope">
                      <el-upload
                        action="#"
                        :show-file-list="false"
                        :http-request="(opt) => handleUploadArtifact(opt, scope.row.key)"
                        style="display: inline-block; margin-right: 8px;"
                      >
                        <el-button type="primary" size="small" icon="Upload">
                          {{ scope.row.exists ? '覆盖' : '上传' }}
                        </el-button>
                      </el-upload>

                      <template v-if="scope.row.exists">
                        <el-button size="small" icon="Download" @click="downloadFile(scope.row.download_url)">下载</el-button>
                        <el-button size="small" icon="CopyDocument" @click="copyLink(scope.row.download_url)" title="复制链接" />
                      </template>
                    </template>
                  </el-table-column>
                </el-table>

                <!-- 批量升级入口 -->
                <div class="upgrade-action-bar">
                  <div class="info">
                    <el-icon class="icon-pulse"><Top /></el-icon>
                    <span>准备好文件后，点击右侧按钮开始全网升级</span>
                  </div>
                  <el-button 
                    type="danger" 
                    icon="Top" 
                    @click="openUpgradeDialog" 
                    :disabled="!hasAnyArtifact"
                  >
                    执行批量升级
                  </el-button>
                </div>

              </div>
            </el-tab-pane>
          </el-tabs>
        </div>
      </el-card>

    </div>

    <!-- ========================================= -->
    <!-- 弹窗组件区域                               -->
    <!-- ========================================= -->

    <!-- 弹窗: 清理缓存 -->
    <el-dialog v-model="cleanDialog.visible" title="清理全网节点缓存" width="450px">
      <el-form label-position="top">
        <el-form-item label="保留策略">
          <el-radio-group v-model="cleanDialog.retain">
            <el-radio :label="0" border>全部清理 (推荐)</el-radio>
            <el-radio :label="1" border>保留最近 1 个版本</el-radio>
            <el-radio :label="3" border>保留最近 3 个版本</el-radio>
          </el-radio-group>
          <div class="tip-text" style="margin-top: 10px; color: #999; font-size: 12px;">
            Worker 会按服务名分组，保留下载时间最新的 N 个包，删除旧包。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cleanDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="executeClean" :loading="cleanDialog.loading">开始执行</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗: 孤儿扫描结果 -->
    <el-dialog v-model="gcDialog.visible" title="孤儿资源扫描结果" width="800px">
      <div class="gc-container" v-loading="gcDialog.loading">
        <el-alert 
          v-if="gcDialog.scanned && gcDialog.list.length > 0"
          title="警告：请仔细核对！"
          type="warning" 
          show-icon
          :closable="false"
          description="以下目录在 Master 数据库中不存在。标红项表示进程仍在运行（僵尸进程），建议手动排查。"
          style="margin-bottom: 15px;"
        />
        <el-alert 
          v-if="gcDialog.scanned && gcDialog.list.length === 0"
          title="系统很干净"
          type="success" 
          show-icon
          :closable="false"
          description="未发现任何孤儿资源。"
        />

        <el-table 
          v-if="gcDialog.list.length > 0"
          :data="gcDialog.list" 
          style="width: 100%" 
          height="400px" 
          border 
          @selection-change="handleGcSelectionChange"
        >
          <el-table-column type="selection" width="55" :selectable="isGcSelectable" />
          <el-table-column prop="node_ip" label="节点 IP" width="130" />
          <el-table-column label="类型" width="100">
            <template #default="scope">
              <el-tag :type="scope.row.type === 'system_dir' ? 'warning' : 'info'" size="small">
                {{ scope.row.type === 'system_dir' ? '系统目录' : '实例目录' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="path" label="相对路径" show-overflow-tooltip />
          <el-table-column label="占用空间" width="100" align="right">
            <template #default="scope">{{ formatSize(scope.row.size) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100" align="center">
            <template #default="scope">
              <el-tag v-if="scope.row.is_running" type="danger" effect="dark">Running</el-tag>
              <el-tag v-else type="info" effect="plain">Idle</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <template #footer>
        <el-button @click="gcDialog.visible = false">关闭</el-button>
        <el-button 
          v-if="gcDialog.list.length > 0" 
          type="danger" 
          @click="executeGcDelete" 
          :disabled="selectedOrphans.length === 0"
          :loading="gcDialog.deleting"
        >
          删除选中 ({{ selectedOrphans.length }})
        </el-button>
      </template>
    </el-dialog>

    <!-- 弹窗: 批量升级确认 -->
    <el-dialog v-model="upgradeDialog.visible" title="Agent 批量升级" width="500px">
      <div style="margin-bottom: 20px;">
        <el-alert
          title="高风险操作"
          type="warning"
          description="升级过程中，所有 Worker 节点将短暂离线并自动重启。请确保已上传了正确版本的二进制文件，否则节点可能无法启动。"
          show-icon
          :closable="false"
        />
      </div>
      <el-form label-position="top">
        <el-form-item label="升级范围">
          <el-radio-group v-model="upgradeDialog.scope" disabled>
            <el-radio label="all" border>所有在线节点</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="确认操作">
          <el-checkbox v-model="upgradeDialog.confirmed">我已确认上传了正确的 Worker 文件</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="upgradeDialog.visible = false">取消</el-button>
        <el-button 
          type="danger" 
          @click="executeUpgrade" 
          :loading="upgradeDialog.loading"
          :disabled="!upgradeDialog.confirmed"
        >
          立即升级
        </el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElNotification, ElMessageBox } from 'element-plus'
import { 
  Check, Tools, InfoFilled, Brush, Search, Platform, Monitor, 
  Upload, Download, CopyDocument, Top, Apple
} from '@element-plus/icons-vue'

// =====================
// State Management
// =====================

const loading = ref(false)
const saving = ref(false)
const activeTab = ref('system')

// Config Models
const form = reactive({
  logic: { node_offline_threshold: 30, http_client_timeout: 5 },
  worker: { heartbeat_interval: 5, monitor_interval: 3 },
  log: { retention_days: 180 }
})

const nacosForm = reactive({
  url: '',
  username: '',
  password: ''
})

// Dialog States
const cleanDialog = reactive({ visible: false, loading: false, retain: 0 })
const gcDialog = reactive({ visible: false, loading: false, scanned: false, deleting: false, list: [] })
const selectedOrphans = ref([])
const upgradeDialog = reactive({ visible: false, loading: false, scope: 'all', confirmed: false })

// Artifact State
const artifactList = ref([])
const artifactLoading = ref(false)
const hasAnyArtifact = computed(() => artifactList.value.some(item => item.exists))

// =====================
// General Logic
// =====================

const handleTabChange = (tabName) => {
  if (tabName === 'maintenance') {
    loadArtifacts() // 切换到维护 Tab 时刷新文件列表
  }
}

const loadAllConfig = async () => {
  loading.value = true
  try {
    const [sysRes, nacosRes] = await Promise.all([
      request.get('/api/settings/global'),
      request.get('/api/nacos/settings')
    ])

    if (sysRes) {
      if (sysRes.logic) Object.assign(form.logic, sysRes.logic)
      if (sysRes.worker) Object.assign(form.worker, sysRes.worker)
      if (sysRes.log) Object.assign(form.log, sysRes.log)
    }

    if (nacosRes) {
      nacosForm.url = nacosRes.url || ''
      nacosForm.username = nacosRes.username || ''
      if (nacosRes.password && nacosRes.password !== '******') {
          nacosForm.password = nacosRes.password
      }
    }
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const saveAllConfig = async () => {
  saving.value = true
  try {
    const p1 = request.post('/api/settings/global', form)
    let p2 = Promise.resolve()
    if (nacosForm.url) {
        p2 = request.post('/api/nacos/settings', nacosForm)
    }
    await Promise.all([p1, p2])
    ElMessage.success('所有配置已保存并生效')
  } catch(e) {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

// =====================
// Maintenance: Cache Clean
// =====================

const openCleanDialog = () => {
  cleanDialog.retain = 0
  cleanDialog.visible = true
}

const executeClean = async () => {
  cleanDialog.loading = true
  try {
    const res = await request.post('/api/maintenance/cleanup_all_cache', {
      retain: cleanDialog.retain
    })
    cleanDialog.visible = false
    
    const totalSize = formatSize(res.total_freed)
    const totalBase = res.target_nodes !== undefined ? res.target_nodes : res.total_nodes
    
    ElNotification({
      title: '清理完成',
      message: `在线节点清理成功: ${res.success_count} / ${totalBase}，共释放空间: ${totalSize}`,
      type: 'success',
      duration: 5000
    })
  } catch (e) {
    ElMessage.error('请求失败: ' + e.message)
  } finally {
    cleanDialog.loading = false
  }
}

// =====================
// Maintenance: Orphan GC
// =====================

const openGcDialog = async () => {
  gcDialog.visible = true
  gcDialog.scanned = false
  gcDialog.list = []
  gcDialog.loading = true
  
  try {
    const res = await request.post('/api/maintenance/scan_orphans')
    const flatList = []
    res.forEach(node => {
      if (node.items) {
        node.items.forEach(item => {
          flatList.push({ node_ip: node.node_ip, ...item })
        })
      }
    })
    gcDialog.list = flatList
    gcDialog.scanned = true
  } catch(e) {
    ElMessage.error('扫描失败: ' + e.message)
  } finally {
    gcDialog.loading = false
  }
}

const isGcSelectable = (row) => !row.is_running
const handleGcSelectionChange = (val) => selectedOrphans.value = val

const executeGcDelete = async () => {
  try {
    await ElMessageBox.confirm(
      `确定要永久删除这 ${selectedOrphans.value.length} 个目录吗？此操作不可撤销！`, 
      '高风险操作', 
      { type: 'error', confirmButtonText: '确定删除', cancelButtonText: '再想想' }
    )
    
    gcDialog.deleting = true
    const targets = []
    const map = {}
    selectedOrphans.value.forEach(item => {
      if (!map[item.node_ip]) map[item.node_ip] = []
      map[item.node_ip].push(item.path)
    })
    for (const ip in map) {
      targets.push({ node_ip: ip, paths: map[ip] })
    }

    const res = await request.post('/api/maintenance/delete_orphans', { targets })
    ElMessage.success(`清理成功，共删除 ${res.success_count} 个项目`)
    gcDialog.visible = false
  } catch(e) {
    if (e !== 'cancel') ElMessage.error('清理失败: ' + (e.message || e))
  } finally {
    gcDialog.deleting = false
  }
}

// =====================
// Maintenance: Artifacts & Upgrade
// =====================

const loadArtifacts = async () => {
  artifactLoading.value = true
  try {
    const res = await request.get('/api/maintenance/artifacts')
    // Sort: Linux first
    artifactList.value = (res || []).sort((a, b) => a.key.localeCompare(b.key))
  } catch (e) {
    console.error(e)
  } finally {
    artifactLoading.value = false
  }
}

const handleUploadArtifact = async (options, key) => {
  const formData = new FormData()
  formData.append('file', options.file)
  formData.append('key', key)

  const loadingMsg = ElMessage.info({ message: '正在上传...', duration: 0 })
  try {
    await request.post('/api/maintenance/artifacts/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
    loadingMsg.close()
    ElMessage.success('上传成功')
    loadArtifacts()
  } catch (e) {
    loadingMsg.close()
    ElMessage.error('上传失败: ' + (e.message || '网络错误'))
  }
}

const downloadFile = (url) => window.open(url, '_blank')

const copyLink = (url) => {
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('链接已复制')
  })
}

const openUpgradeDialog = () => {
  upgradeDialog.confirmed = false
  upgradeDialog.visible = true
}

const executeUpgrade = async () => {
  upgradeDialog.loading = true
  try {
    const res = await request.post('/api/maintenance/upgrade_workers')
    upgradeDialog.visible = false
    ElNotification({
      title: '升级指令已下发',
      message: `成功触发 ${res.success} 个节点 (共尝试 ${res.triggered} 个)`,
      type: 'success',
      duration: 8000
    })
  } catch(e) {
    ElMessage.error('升级失败: ' + e.message)
  } finally {
    upgradeDialog.loading = false
  }
}

// =====================
// Utils
// =====================

const formatSize = (bytes) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const formatTime = (ts) => {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

onMounted(() => {
  loadAllConfig()
  loadArtifacts()
})
</script>

<style scoped>
.view-container { 
  height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); 
}
.content-body { padding: 20px; flex: 1; overflow-y: auto; }
.settings-card { max-width: 900px; margin: 0 auto; border: 1px solid var(--el-border-color-light); }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.title-with-icon { display: flex; align-items: center; gap: 8px; font-weight: bold; font-size: 16px; color: var(--el-text-color-primary); }
.form-container { padding: 10px; }
.section-title { font-size: 15px; font-weight: 600; margin-bottom: 20px; padding-left: 10px; border-left: 4px solid var(--el-color-primary); color: var(--el-text-color-primary); }
.input-wrapper { display: flex; align-items: center; gap: 10px; }
.unit { color: var(--el-text-color-secondary); }
.tip { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.4; margin-top: 6px; }
.tip-box { background-color: var(--el-color-success-light-9); border-radius: 4px; padding: 8px 12px; display: flex; align-items: center; gap: 8px; margin-bottom: 20px; font-size: 13px; color: var(--el-color-success); }

/* Maintenance Panel */
.maintenance-panel { padding: 0 10px; }
.mb-20 { margin-bottom: 20px; }
.action-row {
  display: flex; justify-content: space-between; align-items: center;
  padding: 20px;
  background-color: var(--el-fill-color-light);
  border-radius: 6px;
  border: 1px solid var(--el-border-color-lighter);
}
.label-group { display: flex; flex-direction: column; gap: 6px; }
.main-label { font-weight: 600; color: var(--el-text-color-primary); font-size: 14px; }
.sub-label { color: var(--el-text-color-secondary); font-size: 12px; }

/* File Info Styles */
.file-info { font-size: 12px; color: var(--el-text-color-regular); line-height: 1.5; }
.file-info .label { color: var(--el-text-color-secondary); display: inline-block; width: 40px; }
.text-gray { color: #ccc; font-style: italic; }

/* Upgrade Bar */
.upgrade-action-bar {
  display: flex; justify-content: space-between; align-items: center;
  background-color: var(--el-color-success-light-9);
  border: 1px solid var(--el-color-success-light-5);
  padding: 15px 20px;
  border-radius: 4px;
}
.upgrade-action-bar .info {
  font-size: 14px; color: var(--el-color-success); font-weight: bold;
  display: flex; align-items: center; gap: 8px;
}
.icon-pulse { animation: pulse 2s infinite; }
@keyframes pulse { 0% { opacity: 1; } 50% { opacity: 0.5; } 100% { opacity: 1; } }
</style>