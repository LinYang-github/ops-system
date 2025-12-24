<template>
    <div class="view-container">
      <div class="header">
        <h2>📜 操作日志审计</h2>
        <div class="filter-bar">
          <el-input 
            v-model="query.keyword" 
            placeholder="搜索操作、对象或IP..." 
            style="width: 300px" 
            clearable 
            @clear="fetchLogs"
            @keyup.enter="fetchLogs"
          >
            <template #append>
              <el-button :icon="Search" @click="fetchLogs" />
            </template>
          </el-input>
          <el-button :icon="Refresh" circle @click="fetchLogs" style="margin-left: 10px" />
        </div>
      </div>
  
      <el-card shadow="never" class="table-card">
        <el-table :data="logs" style="width: 100%; height: 100%" v-loading="loading" stripe border>
          
          <el-table-column label="时间" width="180">
            <template #default="scope">
              <span class="time-text">{{ formatTime(scope.row.create_time) }}</span>
            </template>
          </el-table-column>
  
          <el-table-column prop="operator" label="操作者 (IP)" width="150" show-overflow-tooltip />
  
          <el-table-column prop="action" label="动作类型" width="160">
            <template #default="scope">
              <el-tag :type="getActionTag(scope.row.action)" effect="plain">
                {{ scope.row.action }}
              </el-tag>
            </template>
          </el-table-column>
  
          <el-table-column label="操作对象">
            <template #default="scope">
              <span style="font-weight: bold">{{ scope.row.target_name }}</span>
              <el-tag size="small" type="info" style="margin-left: 5px">{{ scope.row.target_type }}</el-tag>
            </template>
          </el-table-column>
  
          <el-table-column prop="detail" label="详情" min-width="200" show-overflow-tooltip />
  
          <el-table-column prop="status" label="结果" width="100" align="center">
            <template #default="scope">
              <el-tag :type="scope.row.status === 'success' ? 'success' : 'danger'" size="small">
                {{ scope.row.status }}
              </el-tag>
            </template>
          </el-table-column>
  
        </el-table>
  
        <div class="pagination">
          <el-pagination
            v-model:current-page="query.page"
            v-model:page-size="query.page_size"
            :total="total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            @size-change="fetchLogs"
            @current-change="fetchLogs"
          />
        </div>
      </el-card>
    </div>
  </template>
  
  <script setup>
  import { ref, reactive, onMounted } from 'vue'
  import request from '../utils/request'
  import { Search, Refresh } from '@element-plus/icons-vue'
  
  const loading = ref(false)
  const logs = ref([])
  const total = ref(0)
  
  const query = reactive({
    page: 1,
    page_size: 20,
    keyword: ''
  })
  
  const fetchLogs = async () => {
    loading.value = true
    try {
      const res = await request.post('/api/logs', query)
      logs.value = res.list || []
      total.value = res.total || 0
    } catch (e) {
      console.error(e)
    } finally {
      loading.value = false
    }
  }
  
  const formatTime = (ts) => new Date(ts * 1000).toLocaleString()
  
  const getActionTag = (action) => {
    if (action.includes('delete') || action.includes('destroy')) return 'danger'
    if (action.includes('create') || action.includes('add')) return 'primary'
    if (action.includes('start')) return 'success'
    if (action.includes('stop')) return 'warning'
    return 'info'
  }
  
  onMounted(() => {
    fetchLogs()
  })
  </script>
  
  <style scoped>
  .view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); padding: 20px; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
  .header h2 { margin: 0; font-size: 20px; color: var(--el-text-color-primary); }
  .filter-bar { display: flex; align-items: center; }
  .table-card { flex: 1; display: flex; flex-direction: column; border: none; overflow: hidden; }
  .time-text { font-family: monospace; font-size: 13px; color: var(--el-text-color-regular); }
  .pagination { margin-top: 15px; display: flex; justify-content: flex-end; }
  </style>