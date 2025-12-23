<template>
    <div class="view-container">
      
      <div class="content-body" v-loading="loading">
        
        <!-- 上半部分：规则配置 -->
        <el-card shadow="never" class="box-card">
          <template #header>
            <div class="card-header">
              <div class="title-with-icon">
                 <el-icon><Setting /></el-icon> <span>告警规则配置</span>
              </div>
              <div class="card-actions">
                 <el-button icon="Refresh" circle size="small" @click="loadData" title="刷新数据" />
                 <el-button type="primary" size="small" icon="Plus" @click="showAddRule=true">添加规则</el-button>
              </div>
            </div>
          </template>
          
          <el-table :data="rules" size="small" stripe>
            <el-table-column prop="name" label="规则名称" />
            <el-table-column label="监控对象" width="120">
               <template #default="scope">
                  <el-tag size="small" effect="plain">{{ scope.row.target_type }}</el-tag>
               </template>
            </el-table-column>
            <el-table-column label="触发条件">
               <template #default="scope">
                  <span class="mono-text">{{ scope.row.metric }} {{ scope.row.condition }} {{ scope.row.threshold }}</span>
                  <span class="duration-text"> (持续 {{ scope.row.duration }}s)</span>
               </template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="center">
              <template #default="scope">
                <el-popconfirm title="删除此规则?" @confirm="deleteRule(scope.row.id)">
                  <template #reference>
                    <el-button type="danger" link icon="Delete" />
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
  
        <!-- 下半部分：告警历史 -->
        <el-card shadow="never" class="box-card history-card">
          <template #header>
            <div class="card-header">
              <div class="title-with-icon">
                 <el-icon><Bell /></el-icon> <span>告警事件记录</span>
                 <!-- 活跃告警提示 -->
                 <el-tag v-if="activeAlerts.length > 0" type="danger" effect="dark" size="small" style="margin-left: 10px;">
                   {{activeAlerts.length}} 个活跃中
                 </el-tag>
              </div>
              
              <!-- [新增] 清空按钮 -->
              <div class="card-actions">
                 <el-popconfirm 
                    title="确定清空所有历史告警记录吗？此操作不可恢复。" 
                    confirm-button-type="danger"
                    @confirm="clearAllEvents"
                    width="220"
                 >
                   <template #reference>
                     <el-button type="danger" plain size="small" icon="Delete" :disabled="allAlerts.length === 0">清空记录</el-button>
                   </template>
                 </el-popconfirm>
              </div>
            </div>
          </template>
          
          <el-table :data="allAlerts" size="small" stripe style="width: 100%; height: 100%;">
             <el-table-column label="触发时间" width="160">
               <template #default="scope">
                 <span class="time-text">{{ formatTime(scope.row.start_time) }}</span>
               </template>
             </el-table-column>
             
             <el-table-column prop="status" label="状态" width="100">
               <template #default="scope">
                 <el-tag :type="scope.row.status==='firing'?'danger':'success'" effect="dark" size="small" class="status-tag">
                   {{ scope.row.status === 'firing' ? 'FIRING' : 'RESOLVED' }}
                 </el-tag>
               </template>
             </el-table-column>
  
             <el-table-column prop="message" label="告警详情" show-overflow-tooltip>
                <template #default="scope">
                   <span :class="{ 'text-danger': scope.row.status === 'firing' }">{{ scope.row.message }}</span>
                </template>
             </el-table-column>
  
             <el-table-column prop="target_name" label="来源对象" width="180" show-overflow-tooltip />
             
             <el-table-column prop="rule_name" label="命中规则" width="150" show-overflow-tooltip />
  
             <!-- [新增] 单条删除列 -->
             <el-table-column label="操作" width="80" align="center">
               <template #default="scope">
                 <el-popconfirm title="删除此条记录?" @confirm="deleteEvent(scope.row.id)">
                    <template #reference>
                      <el-button type="info" link icon="Close" title="删除记录" />
                    </template>
                 </el-popconfirm>
               </template>
             </el-table-column>
          </el-table>
        </el-card>
      </div>
  
      <!-- 添加规则弹窗 -->
      <el-dialog v-model="showAddRule" title="添加规则" width="400px">
        <el-form :model="newRule" label-width="80px">
          <el-form-item label="名称"><el-input v-model="newRule.name" /></el-form-item>
          <el-form-item label="对象类型">
             <el-select v-model="newRule.target_type" style="width:100%">
               <el-option label="节点 (Node)" value="node" />
               <el-option label="实例 (Instance)" value="instance" />
             </el-select>
          </el-form-item>
          <el-form-item label="监控指标">
             <el-select v-model="newRule.metric" style="width:100%">
               <el-option label="CPU (%)" value="cpu" />
               <el-option label="内存 (MB)" value="mem" />
               <el-option label="状态异常" value="status" />
             </el-select>
          </el-form-item>
          <el-form-item label="阈值">
              <el-input-number v-model="newRule.threshold" />
              <span style="font-size: 12px; color: #999; margin-left: 10px;">(状态异常填 0)</span>
          </el-form-item>
          <el-form-item label="持续时间">
              <el-input-number v-model="newRule.duration" :min="0" /> 秒
          </el-form-item>
        </el-form>
        <template #footer><el-button type="primary" @click="addRule">保存</el-button></template>
      </el-dialog>
    </div>
  </template>
  
  <script setup>
  import { ref, reactive, computed, onMounted } from 'vue'
  import axios from 'axios'
  import { ElMessage } from 'element-plus'
  import { Bell, Refresh, Delete, Setting, Plus, Close } from '@element-plus/icons-vue'
  
  const loading = ref(false)
  const rules = ref([])
  const activeAlerts = ref([])
  const historyAlerts = ref([])
  const showAddRule = ref(false)
  const newRule = reactive({ name: '', target_type: 'node', metric: 'cpu', condition: '>', threshold: 80, duration: 10 })
  
  // 合并显示
  const allAlerts = computed(() => {
    return [...activeAlerts.value, ...historyAlerts.value]
  })
  
  const loadData = async () => {
    loading.value = true
    try {
      const r1 = await axios.get('/api/alerts/rules')
      rules.value = r1.data || []
      const r2 = await axios.get('/api/alerts/events')
      activeAlerts.value = r2.data.active || []
      historyAlerts.value = r2.data.history || []
    } finally {
      loading.value = false
    }
  }
  
  const addRule = async () => {
    await axios.post('/api/alerts/rules/add', newRule)
    showAddRule.value = false
    loadData()
    ElMessage.success('规则已添加')
  }
  
  const deleteRule = async (id) => {
    await axios.get(`/api/alerts/rules/delete?id=${id}`)
    loadData()
  }
  
  // [新增] 删除单条事件
  const deleteEvent = async (id) => {
    try {
      await axios.post('/api/alerts/events/delete', { id })
      ElMessage.success('记录已删除')
      loadData()
    } catch (e) {
      ElMessage.error('删除失败')
    }
  }
  
  // [新增] 清空所有事件
  const clearAllEvents = async () => {
    try {
      await axios.post('/api/alerts/events/clear')
      ElMessage.success('所有告警记录已清空')
      loadData()
    } catch (e) {
      ElMessage.error('清空失败')
    }
  }
  
  const formatTime = (ts) => new Date(ts*1000).toLocaleString()
  
  onMounted(loadData)
  </script>
  
  <style scoped>
  .view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
  .content-body { padding: 20px; flex: 1; display: flex; flex-direction: column; overflow: hidden; gap: 20px; }
  
  .box-card { border: none; display: flex; flex-direction: column; }
  .history-card { flex: 1; overflow: hidden; }
  
  .card-header { display: flex; justify-content: space-between; align-items: center; }
  .title-with-icon { display: flex; align-items: center; gap: 6px; font-weight: bold; color: var(--el-text-color-primary); }
  
  .mono-text { font-family: Consolas, monospace; font-weight: bold; }
  .duration-text { color: var(--el-text-color-secondary); font-size: 12px; }
  .time-text { font-size: 12px; color: var(--el-text-color-regular); }
  .status-tag { font-weight: bold; }
  
  .text-danger { color: var(--el-color-danger); font-weight: bold; }
  </style>