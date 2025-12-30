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
            <div class="card-actions" v-if="activeTab !== 'maintenance'">
               <el-button type="primary" icon="Check" @click="saveAllConfig" :loading="saving">保存所有配置</el-button>
            </div>
          </div>
        </template>

        <div class="form-container">
          <el-tabs type="border-card" v-model="activeTab">
            
            <!-- Tab 1: 系统运行参数 -->
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

            <!-- Tab 2: Nacos 连接配置 (新增) -->
            <el-tab-pane label="配置中心 (Nacos)" name="nacos">
              <el-form :model="nacosForm" label-width="120px" label-position="left" style="max-width: 600px">
                <div class="section-title">连接设置</div>
                <div class="tip" style="margin-bottom: 20px;">
                  配置 Nacos Server 的连接信息。Master 将作为代理访问 Nacos API。
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
                  <el-input v-model="nacosForm.password" type="password" show-password placeholder="nacos" />
                </el-form-item>
              </el-form>
            </el-tab-pane>
            <!-- Tab 3: 运维维护 (新增) -->
            <el-tab-pane label="运维维护" name="maintenance">
              <div class="maintenance-panel">
                <div class="section-title">存储空间清理</div>
                
                <el-alert
                  title="功能说明"
                  type="info"
                  :closable="false"
                  show-icon
                  class="mb-20"
                >
                  <template #default>
                    此操作将向集群内所有<b>在线节点</b>下发清理指令，删除 `instances/pkg_cache/` 目录下的服务包文件。
                    <br/>建议在磁盘空间不足或大版本更新后执行。
                  </template>
                </el-alert>

                <div class="action-row">
                  <div class="label-group">
                    <span class="main-label">清理节点下载缓存</span>
                    <span class="sub-label">删除 Worker 节点已下载的 ZIP 包，不影响运行中的服务。</span>
                  </div>
                  <el-button type="warning" plain icon="Brush" @click="openCleanDialog">立即清理</el-button>
                </div>

                <!-- 预留位置：后续可以在这里加 日志清理、孤儿进程清理 等功能 -->
              </div>
            </el-tab-pane>
          </el-tabs>
        </div>
      </el-card>

    </div>
    <!-- 弹窗：清理确认 -->
    <el-dialog v-model="cleanDialog.visible" title="清理全网节点缓存" width="450px">
      <el-form label-position="top">
        <el-form-item label="保留策略">
          <el-radio-group v-model="cleanDialog.retain">
            <el-radio :label="0" border>全部清理 (推荐)</el-radio>
            <el-radio :label="1" border>保留最近 1 个版本</el-radio>
            <el-radio :label="3" border>保留最近 3 个版本</el-radio>
          </el-radio-group>
          <div class="tip-text" style="margin-top: 10px; color: #999; font-size: 12px;">
            选择“保留版本”时，Worker 会按服务名分组，保留下载时间最新的 N 个包，删除旧包。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cleanDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="executeClean" :loading="cleanDialog.loading">开始执行</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage, ElNotification } from 'element-plus'
import { Check, Tools, InfoFilled } from '@element-plus/icons-vue'

const loading = ref(false)
const saving = ref(false)
const activeTab = ref('system')

// 系统配置
const form = reactive({
  logic: { node_offline_threshold: 30, http_client_timeout: 5 },
  worker: { heartbeat_interval: 5, monitor_interval: 3 },
  log: { retention_days: 180 }
})

// Nacos 配置
const nacosForm = reactive({
  url: '',
  username: '',
  password: ''
})

const cleanDialog = reactive({
  visible: false,
  loading: false,
  retain: 0
})
const loadAllConfig = async () => {
  loading.value = true
  try {
    // 并行请求两个接口
    const [sysRes, nacosRes] = await Promise.all([
      request.get('/api/settings/global'),
      request.get('/api/nacos/settings')
    ])

    // 填充系统配置
    if (sysRes) {
      if (sysRes.logic) Object.assign(form.logic, sysRes.logic)
      if (sysRes.worker) Object.assign(form.worker, sysRes.worker)
      if (sysRes.log) Object.assign(form.log, sysRes.log)
    }

    // 填充 Nacos 配置
    if (nacosRes) {
      nacosForm.url = nacosRes.url || ''
      nacosForm.username = nacosRes.username || ''
      // 密码如果是 ****** 则不覆盖，保持空或原有逻辑
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
    // 根据当前 Tab 决定保存什么，或者全部保存
    // 这里为了简单，全部保存
    const p1 = request.post('/api/settings/global', form)
    
    // 如果 Nacos 填了地址，才保存
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
    
    // 显示详细结果通知
    const totalSize = formatSize(res.total_freed)
    ElNotification({
      title: '清理完成',
      message: `成功节点: ${res.success_count} / ${res.total_nodes}，共释放空间: ${totalSize}`,
      type: 'success',
      duration: 5000
    })
    
  } catch (e) {
    ElMessage.error('请求失败: ' + e.message)
  } finally {
    cleanDialog.loading = false
  }
}

const formatSize = (bytes) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}


onMounted(loadAllConfig)
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
/* 新增维护面板样式 */
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
</style>