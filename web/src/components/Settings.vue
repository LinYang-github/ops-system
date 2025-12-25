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
            <div class="card-actions">
               <el-button type="primary" icon="Check" @click="saveConfig" :loading="saving">保存配置</el-button>
            </div>
          </div>
        </template>

        <div class="form-container">
          <el-form :model="form" label-width="180px" label-position="left">
            
            <!-- Master 区域 -->
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
              <div class="tip">Master 向 Worker 下发指令（如启停）时的最大等待时间。</div>
            </el-form-item>

            <el-divider border-style="dashed" />

            <!-- Worker 区域 -->
            <div class="section-title">Worker 代理侧</div>
            
            <!-- 【修改点】更新了提示文案 -->
            <div class="tip-box">
              <el-icon><InfoFilled /></el-icon> 
              <span>配置保存后，Worker 将在下一次心跳交互时自动同步并生效，无需重启。</span>
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
              <div class="tip">采集 CPU、内存、IO 数据的频率。</div>
            </el-form-item>

            <el-divider border-style="dashed" />

            <!-- 维护区域 -->
            <div class="section-title">系统维护</div>

            <el-form-item label="操作日志保留天数">
              <div class="input-wrapper">
                <el-input-number v-model="form.log.retention_days" :min="1" :max="3650" />
                <span class="unit">天</span>
              </div>
              <div class="tip">系统将自动每天清理一次超过此时限的操作审计日志。</div>
            </el-form-item>
          </el-form>
        </div>
      </el-card>

    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage } from 'element-plus'
import { Check, Tools, InfoFilled } from '@element-plus/icons-vue'

const loading = ref(false)
const saving = ref(false)

const form = reactive({
  logic: { node_offline_threshold: 30, http_client_timeout: 5 },
  worker: { heartbeat_interval: 5, monitor_interval: 3 },
  log: { retention_days: 180 }
})

const loadConfig = async () => {
  loading.value = true
  try {
    const data = await request.get('/api/settings/global')
    if (data) {
      if (data.logic) Object.assign(form.logic, data.logic)
      if (data.worker) Object.assign(form.worker, data.worker)
      if (data.log) Object.assign(form.log, data.log)
    }
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    await request.post('/api/settings/global', form)
    ElMessage.success('配置已保存并生效')
  } finally {
    saving.value = false
  }
}

onMounted(loadConfig)
</script>

<style scoped>
.view-container { 
  height: 100%; 
  display: flex; 
  flex-direction: column; 
  background: var(--el-bg-color); 
}

.content-body { 
  padding: 20px; 
  flex: 1; 
  overflow-y: auto; 
}

.settings-card { 
  max-width: 800px; 
  margin: 0 auto; 
  border: 1px solid var(--el-border-color-light);
}

.card-header { 
  display: flex; 
  justify-content: space-between; 
  align-items: center; 
}

.title-with-icon { 
  display: flex; 
  align-items: center; 
  gap: 8px; 
  font-weight: bold; 
  font-size: 16px;
  color: var(--el-text-color-primary);
}

.form-container {
  padding: 10px 20px;
}

.section-title {
  font-size: 15px;
  font-weight: 600;
  margin-bottom: 20px;
  padding-left: 10px;
  border-left: 4px solid var(--el-color-primary);
  color: var(--el-text-color-primary);
}

.input-wrapper {
  display: flex;
  align-items: center;
  gap: 10px;
}

.unit {
  color: var(--el-text-color-secondary);
}

.tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.4;
  margin-top: 6px;
}

.tip-box {
  background-color: var(--el-color-success-light-9); /* 改为绿色系提示，表示功能增强 */
  border-radius: 4px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 20px;
  font-size: 13px;
  color: var(--el-color-success);
}
</style>