<template>
  <div class="view-container">
    
    <!-- 状态 A：未连接 (居中显示) -->
    <div v-if="!connected" class="empty-wrapper">
      <el-empty description="尚未连接 Nacos 配置中心">
        <template #extra>
          <el-button type="primary" icon="Setting" @click="showSettings = true">配置连接信息</el-button>
        </template>
      </el-empty>
    </div>

    <!-- 状态 B：已连接 (内容区域) -->
    <div v-else class="content-body" v-loading="loading">
      <el-card shadow="never" class="main-card">
        <!-- 卡片头部：替代原来的页面 Header -->
        <template #header>
          <div class="card-header">
            <div class="header-left">
              <span class="title">Nacos 配置管理</span>
              <el-tag type="success" effect="plain" size="small" round class="status-tag">
                <span class="dot"></span> Connected
              </el-tag>
            </div>
            <div class="header-right">
              <el-button icon="Setting" link @click="showSettings = true">连接设置</el-button>
            </div>
          </div>
        </template>

        <!-- 筛选栏 -->
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

        <!-- 表格 -->
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
        
        <!-- 分页 -->
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

    <!-- 弹窗：连接设置 -->
    <el-dialog v-model="showSettings" title="Nacos 连接配置" width="400px" append-to-body>
      <el-form label-width="70px" size="large">
        <el-form-item label="地址"><el-input v-model="settings.url" placeholder="http://127.0.0.1:8848" /></el-form-item>
        <el-form-item label="账号"><el-input v-model="settings.username" placeholder="nacos" /></el-form-item>
        <el-form-item label="密码"><el-input v-model="settings.password" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showSettings=false">取消</el-button>
        <el-button type="primary" @click="saveSettings">保存并连接</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗：编辑配置 -->
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
           <el-input 
             v-model="editForm.content" 
             type="textarea" 
             :rows="20" 
             class="code-editor" 
             placeholder="请输入配置内容..."
           />
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
import { ref, reactive, onMounted } from 'vue'
import request from '../utils/request'
import { ElMessage } from 'element-plus'
import { Setting, Search, Plus, Edit, Delete, Folder } from '@element-plus/icons-vue'

const connected = ref(false)
const showSettings = ref(false)
const loading = ref(false)
const settings = reactive({ url: '', username: '', password: '' })

const namespaces = ref([])
const currNs = ref('')
const currGroup = ref('')
const currDataId = ref('')
const configList = ref([])
const page = ref(1)
const total = ref(0)

const editDialog = reactive({ visible: false, isNew: false })
const editForm = reactive({ dataId: '', group: 'DEFAULT_GROUP', type: 'yaml', content: '' })

// 初始化检查连接
const checkConnection = async () => {
  try {
    const res = await request.get('/api/nacos/settings')
    if(res && res.url) {
      settings.url = res.url
      settings.username = res.username
      await fetchNamespaces()
      connected.value = true
      fetchConfigs()
    }
  } catch(e) {
    connected.value = false
  }
}

const saveSettings = async () => {
  try {
    await request.post('/api/nacos/settings', settings)
    ElMessage.success('保存成功')
    showSettings.value = false
    checkConnection()
  } catch(e) { /* request.js handles error */ }
}

const fetchNamespaces = async () => {
  const res = await request.get('/api/nacos/namespaces')
  if(res && res.data) {
    namespaces.value = res.data
    if(!currNs.value && namespaces.value.length > 0) {
        currNs.value = namespaces.value[0].namespace
    }
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

const openEdit = async (row) => {
  if (row) {
    editDialog.isNew = false
    editForm.dataId = row.dataId
    editForm.group = row.group
    editForm.type = row.type
    // 获取详情
    const res = await request.get('/api/nacos/config/detail', {
        params: { tenant: currNs.value, dataId: row.dataId, group: row.group }
    })
    editForm.content = typeof res === 'object' ? JSON.stringify(res) : res
  } else {
    editDialog.isNew = true
    editForm.dataId = ''
    editForm.group = 'DEFAULT_GROUP'
    editForm.content = ''
  }
  editDialog.visible = true
}

const publishConfig = async () => {
  try {
    await request.post('/api/nacos/config/publish', {
        tenant: currNs.value,
        ...editForm
    })
    ElMessage.success('发布成功')
    editDialog.visible = false
    fetchConfigs()
  } catch(e) { }
}

const deleteConfig = async (row) => {
  try {
    await request.post('/api/nacos/config/delete', {
        tenant: currNs.value,
        dataId: row.dataId,
        group: row.group
    })
    ElMessage.success('已删除')
    fetchConfigs()
  } catch(e) { }
}

onMounted(checkConnection)
</script>

<style scoped>
.view-container { 
  height: 100%; 
  display: flex; 
  flex-direction: column; 
  background: var(--el-bg-color-page); 
}

/* 空状态居中 */
.empty-wrapper {
  flex: 1;
  display: flex;
  justify-content: center;
  align-items: center;
  background: var(--el-bg-color);
}

.content-body { 
  padding: 20px; 
  flex: 1; 
  overflow: hidden; 
  display: flex; 
  flex-direction: column;
}

/* 主卡片样式 */
.main-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--el-border-color-light);
  background: var(--el-bg-color);
}

/* 强制卡片内容区域撑满，以便表格滚动 */
.main-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.status-tag .dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: #fff;
  margin-right: 4px;
}

/* 筛选栏 */
.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

/* 表格样式 */
.custom-table {
  flex: 1;
  overflow: hidden;
}
.data-id-text {
  font-family: monospace;
  font-weight: 500;
  color: var(--el-text-color-primary);
}
.type-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-transform: uppercase;
}

.pagination {
  margin-top: 15px;
  display: flex;
  justify-content: flex-end;
}

/* 弹窗表单 */
.form-row {
  display: flex;
  gap: 20px;
}

/* 代码编辑器样式适配黑夜模式 */
.code-editor :deep(.el-textarea__inner) {
  font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  background-color: #f9f9f9;
  color: #333;
}

/* 黑夜模式特定样式 */
html.dark .code-editor :deep(.el-textarea__inner) {
  background-color: #1e1e1e;
  color: #d4d4d4;
  border-color: #4c4d4f;
}
</style>