<template>
    <div class="view-container">
      <div class="header">
        <div class="header-left">
          <h2>数据备份与恢复</h2>
          <el-tag type="info" round effect="plain" class="count-tag">{{ backups.length }} Snapshots</el-tag>
        </div>
        <div class="header-right">
          <el-button type="primary" icon="Plus" @click="openCreateDialog">新建备份</el-button>
          <el-button icon="Refresh" circle @click="fetchBackups" :loading="loading" />
        </div>
      </div>
  
      <el-card shadow="never" class="table-card">
        <el-table :data="backups" style="width: 100%; height: 100%" stripe v-loading="loading">
          <!-- 1. 文件名 -->
          <el-table-column prop="name" label="备份文件名" min-width="250">
            <template #default="scope">
              <div class="backup-name">
                <el-icon class="icon-zip"><Files /></el-icon>
                <span>{{ scope.row.name }}</span>
              </div>
            </template>
          </el-table-column>
  
          <!-- 2. 类型 (是否包含文件) -->
          <el-table-column label="备份类型" width="150">
            <template #default="scope">
              <el-tag v-if="scope.row.with_files" type="warning" effect="dark" size="small">全量 (含文件)</el-tag>
              <el-tag v-else type="success" effect="dark" size="small">仅元数据</el-tag>
            </template>
          </el-table-column>
  
          <!-- 3. 大小 -->
          <el-table-column prop="size" label="文件大小" width="120" align="right">
            <template #default="scope">
              <span class="mono-text">{{ formatSize(scope.row.size) }}</span>
            </template>
          </el-table-column>
  
          <!-- 4. 创建时间 -->
          <el-table-column prop="create_time" label="创建时间" width="180" align="right">
            <template #default="scope">
              <span class="time-text">{{ formatTime(scope.row.create_time) }}</span>
            </template>
          </el-table-column>
  
          <!-- 5. 操作 -->
          <el-table-column label="操作" width="180" fixed="right" align="center">
            <template #default="scope">
              <el-button 
                link type="danger" 
                icon="RefreshLeft" 
                @click="handleRestore(scope.row)"
              >恢复</el-button>
              <el-divider direction="vertical" />
              <el-popconfirm title="确定删除此备份文件?" @confirm="handleDelete(scope.row)">
                <template #reference>
                  <el-button link type="info" icon="Delete">删除</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
      </el-card>
  
      <!-- 弹窗：创建备份 -->
      <el-dialog v-model="createDialog.visible" title="创建新备份" width="400px">
        <div class="dialog-body">
          <el-alert
            title="提示"
            type="info"
            :closable="false"
            show-icon
            style="margin-bottom: 20px"
          >
            <template #default>
              备份期间数据库可能会短暂锁定。
            </template>
          </el-alert>
          
          <el-form label-position="top">
            <el-form-item label="备份策略">
              <el-radio-group v-model="createDialog.withFiles">
                <el-radio :label="false" border>仅元数据 (推荐)</el-radio>
                <el-radio :label="true" border>全量 (含服务包)</el-radio>
              </el-radio-group>
              <div class="form-tip" v-if="createDialog.withFiles">
                注意：全量备份会包含 uploads 目录下的所有文件，耗时较长且占用磁盘空间。
              </div>
              <div class="form-tip" v-else>
                快速备份，仅包含数据库 (系统结构、节点信息、纳管配置等)。
              </div>
            </el-form-item>
          </el-form>
        </div>
        <template #footer>
          <el-button @click="createDialog.visible = false">取消</el-button>
          <el-button type="primary" @click="createBackup" :loading="createDialog.loading">开始备份</el-button>
        </template>
      </el-dialog>
  
    </div>
  </template>
  
  <script setup>
  import { ref, reactive, onMounted } from 'vue'
  import axios from 'axios'
  import { ElMessage, ElMessageBox, ElLoading } from 'element-plus'
  import { Plus, Refresh, Files, RefreshLeft, Delete } from '@element-plus/icons-vue'
  
  const backups = ref([])
  const loading = ref(false)
  
  const createDialog = reactive({
    visible: false,
    withFiles: false,
    loading: false
  })
  
  // --- API ---
  
  const fetchBackups = async () => {
    loading.value = true
    try {
      const res = await axios.get('/api/backups')
      backups.value = res.data || []
    } catch (e) {
      ElMessage.error('获取列表失败')
    } finally {
      loading.value = false
    }
  }
  
  const openCreateDialog = () => {
    createDialog.withFiles = false
    createDialog.visible = true
  }
  
  const createBackup = async () => {
    createDialog.loading = true
    try {
      await axios.post('/api/backups/create', { with_files: createDialog.withFiles })
      ElMessage.success('备份创建成功')
      createDialog.visible = false
      fetchBackups()
    } catch (e) {
      ElMessage.error('创建失败: ' + (e.response?.data || e.message))
    } finally {
      createDialog.loading = false
    }
  }
  
  const handleDelete = async (row) => {
    try {
      await axios.post('/api/backups/delete', { filename: row.name })
      ElMessage.success('已删除')
      fetchBackups()
    } catch (e) {
      ElMessage.error('删除失败')
    }
  }
  
  const handleRestore = (row) => {
    ElMessageBox.confirm(
      `确定要恢复到备份点 [${row.name}] 吗？\n\n警告：当前所有数据将被覆盖，且服务器将自动重启！`,
      '高风险操作',
      {
        confirmButtonText: '确定恢复',
        cancelButtonText: '取消',
        type: 'error',
        dangerouslyUseHTMLString: false
      }
    ).then(async () => {
      // 全屏 Loading
      const loadingInstance = ElLoading.service({
        lock: true,
        text: '正在恢复数据，服务将重启，请稍候...',
        background: 'rgba(0, 0, 0, 0.7)',
      })
  
      try {
        await axios.post('/api/backups/restore', { filename: row.name })
        // 注意：后端恢复成功后会直接 Exit，前端可能收不到完整的 200 响应或者收到 Network Error
        // 无论如何，这里都提示成功
      } catch (e) {
        console.warn("Restore request ended (likely server exited):", e)
      }
  
      // 延迟刷新页面
      setTimeout(() => {
        loadingInstance.close()
        ElMessageBox.alert('恢复指令已发送，服务正在重启。请尝试刷新页面。', '操作完成', {
          confirmButtonText: '刷新页面',
          callback: () => {
            window.location.reload()
          }
        })
      }, 3000)
    })
  }
  
  // --- Utils ---
  
  const formatTime = (ts) => {
    if (!ts) return '-'
    return new Date(ts * 1000).toLocaleString()
  }
  
  const formatSize = (bytes) => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }
  
  onMounted(() => {
    fetchBackups()
  })
  </script>
  
  <style scoped>
  .view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
  
  .header { 
    padding: 15px 20px; 
    border-bottom: 1px solid var(--el-border-color-light); 
    display: flex; 
    justify-content: space-between; 
    align-items: center; 
    background: var(--el-bg-color);
    flex-shrink: 0;
  }
  .header-left { display: flex; align-items: center; gap: 12px; }
  .header h2 { margin: 0; font-size: 18px; color: var(--el-text-color-primary); }
  .count-tag { font-family: monospace; }
  
  .table-card { border: none; flex: 1; display: flex; flex-direction: column; overflow: hidden; background: transparent; }
  
  .backup-name { display: flex; align-items: center; font-weight: bold; color: var(--el-text-color-primary); }
  .icon-zip { font-size: 18px; margin-right: 8px; color: #E6A23C; }
  
  .mono-text { font-family: Consolas, monospace; font-size: 13px; }
  .time-text { font-size: 13px; color: var(--el-text-color-secondary); }
  
  .form-tip { margin-top: 8px; font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.4; }
  </style>