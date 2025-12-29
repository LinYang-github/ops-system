<template>
  <div class="view-container">
    
    <!-- 状态 A：未连接 -->
    <div v-if="!connected" class="empty-wrapper">
      <el-empty description="尚未连接 Nacos 配置中心">
        <template #extra>
          <div class="guide-text">
            请前往 <el-tag @click="goToSettings" style="cursor: pointer">系统设置 -> 配置中心</el-tag> 填写连接信息。
          </div>
        </template>
      </el-empty>
    </div>

    <!-- 状态 B：已连接 (正常功能) -->
    <div v-else class="content-body" v-loading="loading">
      <el-card shadow="never" class="main-card">
        <template #header>
          <div class="card-header">
            <div class="header-left">
              <span class="title">Nacos 配置管理</span>
              <el-tag type="success" effect="plain" size="small" round class="status-tag">
                <span class="dot"></span> Connected
              </el-tag>
            </div>
            <!-- 去掉了原来的“连接设置”按钮 -->
          </div>
        </template>

        <!-- 筛选栏 (保持不变) -->
        <div class="filter-bar">
          <el-select v-model="currNs" placeholder="命名空间" style="width: 220px" @change="fetchConfigs">
            <template #prefix><el-icon><Folder /></el-icon></template>
            <el-option v-for="ns in namespaces" :key="ns.namespace" :label="`${ns.namespaceShowName} (${ns.namespace})`" :value="ns.namespace" />
          </el-select>
          <el-input v-model="currGroup" placeholder="Group" style="width: 150px" clearable @clear="fetchConfigs" />
          <el-input v-model="currDataId" placeholder="搜索 Data ID..." style="width: 240px" clearable @clear="fetchConfigs" :prefix-icon="Search" />
          
          <el-button type="primary" icon="Search" @click="fetchConfigs">查询</el-button>
          <div style="flex: 1"></div>
          <el-button type="primary" plain icon="Plus" @click="openEdit(null)">新建配置</el-button>
        </div>

        <!-- 表格 (保持不变) -->
        <el-table :data="configList" style="width: 100%" stripe class="custom-table">
          <el-table-column prop="dataId" label="Data ID" min-width="200" show-overflow-tooltip>
             <template #default="scope">
                <span class="data-id-text">{{ scope.row.dataId }}</span>
             </template>
          </el-table-column>
          <el-table-column prop="group" label="Group" width="180">
             <template #default="scope">
                <el-tag type="info" size="small">{{ scope.row.group }}</el-tag>
             </template>
          </el-table-column>
          <el-table-column prop="type" label="Type" width="100">
             <template #default="scope">
                <span class="type-text">{{ scope.row.type || 'text' }}</span>
             </template>
          </el-table-column>
          <el-table-column label="操作" width="150" align="right" fixed="right">
            <template #default="scope">
              <el-button link type="primary" icon="Edit" @click="openEdit(scope.row)">编辑</el-button>
              <el-divider direction="vertical" />
              <el-popconfirm title="确定删除此配置?" @confirm="deleteConfig(scope.row)">
                <template #reference><el-button link type="danger" icon="Delete">删除</el-button></template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
        
        <div class="pagination">
           <el-pagination 
             layout="total, prev, pager, next" 
             :total="total" 
             v-model:current-page="page" 
             :page-size="10" 
             @current-change="fetchConfigs"
             background
           />
        </div>
      </el-card>
    </div>

    <!-- 编辑弹窗 (保持不变) -->
    <el-dialog 
      v-model="editDialog.visible" 
      :title="editDialog.isNew ? '新建配置' : '编辑配置'" 
      width="800px"
      top="5vh"
      append-to-body
    >
      <el-form label-width="80px">
        <div class="form-row">
           <el-form-item label="Data ID" style="flex: 2">
              <el-input v-model="editForm.dataId" :disabled="!editDialog.isNew" placeholder="e.g. app.yaml" />
           </el-form-item>
           <el-form-item label="Group" style="flex: 1">
              <el-input v-model="editForm.group" />
           </el-form-item>
           <el-form-item label="格式" style="flex: 1">
              <el-select v-model="editForm.type">
                  <el-option value="yaml" label="YAML" />
                  <el-option value="properties" label="Properties" />
                  <el-option value="json" label="JSON" />
                  <el-option value="text" label="TEXT" />
              </el-select>
           </el-form-item>
        </div>
        
        <el-form-item label="配置内容">
           <el-input v-model="editForm.content" type="textarea" :rows="20" class="code-editor" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="publishConfig">发布</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, inject } from 'vue'
import request from '../utils/request'
import { ElMessage } from 'element-plus'
import { Setting, Search, Plus, Edit, Delete, Folder } from '@element-plus/icons-vue'

// 如果你想在组件间跳转，需要在 App.vue 提供切换方法，这里为了简单使用 inject 或者 location hack
// 但最好的方式是 App.vue 通过 provide('navigate') 提供方法
// 假设 App.vue 没有 provide，我们可以通过修改 activeMenu 的 props 来通知父组件（如果结构支持）
// 这里简单演示：提示用户去点菜单。

const connected = ref(false)
const loading = ref(false)
const namespaces = ref([])
const currNs = ref('')
const currGroup = ref('')
const currDataId = ref('')
const configList = ref([])
const page = ref(1)
const total = ref(0)
const editDialog = reactive({ visible: false, isNew: false })
const editForm = reactive({ dataId: '', group: 'DEFAULT_GROUP', type: 'yaml', content: '' })

// 检查连接
const checkConnection = async () => {
  try {
    const res = await request.get('/api/nacos/settings')
    if(res && res.url) {
      await fetchNamespaces()
      connected.value = true
      fetchConfigs()
    } else {
      connected.value = false
    }
  } catch(e) {
    connected.value = false
  }
}

const fetchNamespaces = async () => {
  const res = await request.get('/api/nacos/namespaces')
  if(res && res.data) {
    namespaces.value = res.data
    if(!currNs.value && namespaces.value.length > 0) currNs.value = namespaces.value[0].namespace
  }
}

const fetchConfigs = async () => {
  loading.value = true
  try {
    const res = await request.get('/api/nacos/configs', {
        params: {
            tenant: currNs.value,
            group: currGroup.value,
            dataId: currDataId.value,
            pageNo: page.value,
            pageSize: 10
        }
    })
    if (res && res.pageItems) {
        configList.value = res.pageItems
        total.value = res.totalCount
    } else {
        configList.value = []
        total.value = 0
    }
  } finally { loading.value = false }
}

// ... openEdit, publishConfig, deleteConfig 保持不变 ...
const openEdit = async (row) => { if (row) { editDialog.isNew = false; editForm.dataId = row.dataId; editForm.group = row.group; editForm.type = row.type; const res = await request.get('/api/nacos/config/detail', { params: { tenant: currNs.value, dataId: row.dataId, group: row.group } }); editForm.content = typeof res === 'object' ? JSON.stringify(res) : res } else { editDialog.isNew = true; editForm.dataId = ''; editForm.group = 'DEFAULT_GROUP'; editForm.content = '' } editDialog.visible = true }
const publishConfig = async () => { try { await request.post('/api/nacos/config/publish', { tenant: currNs.value, ...editForm }); ElMessage.success('发布成功'); editDialog.visible = false; fetchConfigs() } catch(e) { } }
const deleteConfig = async (row) => { try { await request.post('/api/nacos/config/delete', { tenant: currNs.value, dataId: row.dataId, group: row.group }); ElMessage.success('已删除'); fetchConfigs() } catch(e) { } }

// 简单的跳转逻辑提示
const goToSettings = () => {
  ElMessage.info('请点击左侧菜单栏的 [系统设置]')
}

onMounted(checkConnection)
</script>

<style scoped>
.view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color-page); }
.empty-wrapper { flex: 1; display: flex; justify-content: center; align-items: center; background: var(--el-bg-color); }
.content-body { padding: 20px; flex: 1; overflow: hidden; display: flex; flex-direction: column; }
.main-card { flex: 1; display: flex; flex-direction: column; border: 1px solid var(--el-border-color-light); background: var(--el-bg-color); }
.main-card :deep(.el-card__body) { flex: 1; display: flex; flex-direction: column; overflow: hidden; padding: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.header-left { display: flex; align-items: center; gap: 10px; }
.title { font-size: 16px; font-weight: 600; color: var(--el-text-color-primary); }
.status-tag .dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; background-color: #fff; margin-right: 4px; }
.filter-bar { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.custom-table { flex: 1; overflow: hidden; }
.data-id-text { font-family: monospace; font-weight: 500; color: var(--el-text-color-primary); }
.type-text { font-size: 12px; color: var(--el-text-color-secondary); text-transform: uppercase; }
.pagination { margin-top: 15px; display: flex; justify-content: flex-end; }
.form-row { display: flex; gap: 20px; }
.code-editor :deep(.el-textarea__inner) { font-family: 'Menlo', 'Monaco', 'Courier New', monospace; font-size: 13px; line-height: 1.6; background-color: #f9f9f9; color: #333; }
html.dark .code-editor :deep(.el-textarea__inner) { background-color: #1e1e1e; color: #d4d4d4; border-color: #4c4d4f; }
.guide-text { margin-top: 10px; color: #666; font-size: 14px; }
</style>