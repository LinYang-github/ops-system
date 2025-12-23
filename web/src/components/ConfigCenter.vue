<template>
    <div class="view-container">
      <div class="header">
        <div class="header-left">
          <h2>配置中心 (Nacos)</h2>
          <el-tag v-if="connected" type="success" effect="dark" size="small">Connected</el-tag>
          <el-tag v-else type="danger" effect="dark" size="small">Disconnected</el-tag>
        </div>
        <div class="header-right">
          <el-button icon="Setting" @click="showSettings = true">连接设置</el-button>
        </div>
      </div>
  
      <!-- 状态 A：未连接 -->
      <div v-if="!connected" class="empty-state">
        <el-result icon="info" title="未连接配置中心" sub-title="请点击右上角设置按钮配置 Nacos 连接信息">
          <template #extra>
            <el-button type="primary" @click="showSettings = true">去配置</el-button>
          </template>
        </el-result>
      </div>
  
      <!-- 状态 B：配置列表 -->
      <div v-else class="content-body" v-loading="loading">
        <div class="filter-bar">
          <el-select v-model="currNs" placeholder="命名空间" style="width: 200px" @change="fetchConfigs">
            <el-option v-for="ns in namespaces" :key="ns.namespace" :label="`${ns.namespaceShowName} (${ns.namespace})`" :value="ns.namespace" />
          </el-select>
          <el-input v-model="currGroup" placeholder="Group" style="width: 150px" />
          <el-input v-model="currDataId" placeholder="Data ID" style="width: 200px" clearable />
          <el-button type="primary" icon="Search" @click="fetchConfigs">查询</el-button>
          <el-button type="success" icon="Plus" @click="openEdit(null)">新建配置</el-button>
        </div>
  
        <el-card shadow="never" class="table-card">
          <el-table :data="configList" style="width: 100%" stripe>
            <el-table-column prop="dataId" label="Data ID" min-width="200" />
            <el-table-column prop="group" label="Group" width="150" />
            <el-table-column prop="type" label="Type" width="100">
               <template #default="scope">
                  <el-tag size="small">{{ scope.row.type || 'text' }}</el-tag>
               </template>
            </el-table-column>
            <el-table-column label="操作" width="180" align="right">
              <template #default="scope">
                <el-button link type="primary" @click="openEdit(scope.row)">编辑</el-button>
                <el-popconfirm title="确定删除?" @confirm="deleteConfig(scope.row)">
                  <template #reference><el-button link type="danger">删除</el-button></template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
          
          <!-- 分页 -->
          <div class="pagination">
             <el-pagination 
               layout="prev, pager, next" 
               :total="total" 
               v-model:current-page="page" 
               :page-size="10" 
               @current-change="fetchConfigs"
             />
          </div>
        </el-card>
      </div>
  
      <!-- 弹窗：连接设置 -->
      <el-dialog v-model="showSettings" title="Nacos 连接配置" width="400px">
        <el-form label-width="80px">
          <el-form-item label="地址"><el-input v-model="settings.url" placeholder="http://127.0.0.1:8848" /></el-form-item>
          <el-form-item label="账号"><el-input v-model="settings.username" placeholder="nacos" /></el-form-item>
          <el-form-item label="密码"><el-input v-model="settings.password" type="password" show-password /></el-form-item>
        </el-form>
        <template #footer><el-button type="primary" @click="saveSettings">保存并连接</el-button></template>
      </el-dialog>
  
      <!-- 弹窗：编辑配置 -->
      <el-dialog v-model="editDialog.visible" :title="editDialog.isNew ? '新建配置' : '编辑配置'" width="700px">
        <el-form label-width="80px">
          <el-form-item label="Data ID">
             <el-input v-model="editForm.dataId" :disabled="!editDialog.isNew" />
          </el-form-item>
          <el-form-item label="Group">
             <el-input v-model="editForm.group" />
          </el-form-item>
          <el-form-item label="格式">
             <el-radio-group v-model="editForm.type">
                <el-radio-button label="yaml" />
                <el-radio-button label="properties" />
                <el-radio-button label="json" />
                <el-radio-button label="text" />
             </el-radio-group>
          </el-form-item>
          <el-form-item label="内容">
             <el-input v-model="editForm.content" type="textarea" :rows="15" class="code-editor" />
          </el-form-item>
        </el-form>
        <template #footer><el-button type="primary" @click="publishConfig">发布</el-button></template>
      </el-dialog>
    </div>
  </template>
  
  <script setup>
  import { ref, reactive, onMounted } from 'vue'
  import axios from 'axios'
  import { ElMessage } from 'element-plus'
  import { Setting, Search, Plus, Refresh } from '@element-plus/icons-vue'
  
  const connected = ref(false)
  const showSettings = ref(false)
  const loading = ref(false)
  const settings = reactive({ url: '', username: '', password: '' })
  
  const namespaces = ref([])
  const currNs = ref('') // Nacos 默认 public 是空字符串，或者 "public"
  const currGroup = ref('')
  const currDataId = ref('')
  const configList = ref([])
  const page = ref(1)
  const total = ref(0)
  
  const editDialog = reactive({ visible: false, isNew: false })
  const editForm = reactive({ dataId: '', group: 'DEFAULT_GROUP', type: 'yaml', content: '' })
  
  // 1. 初始化检查连接
  const checkConnection = async () => {
    try {
      const res = await axios.get('/api/nacos/settings')
      if(res.data && res.data.url) {
        settings.url = res.data.url
        settings.username = res.data.username
        // 尝试获取命名空间来验证连接是否通
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
      await axios.post('/api/nacos/settings', settings)
      ElMessage.success('保存成功')
      showSettings.value = false
      checkConnection()
    } catch(e) { ElMessage.error('保存失败') }
  }
  
  const fetchNamespaces = async () => {
    const res = await axios.get('/api/nacos/namespaces')
    // Nacos API 返回结构 { code: 200, data: [...] }
    if(res.data && res.data.data) {
      namespaces.value = res.data.data
      if(!currNs.value && namespaces.value.length > 0) {
          // 默认选中第一个
          currNs.value = namespaces.value[0].namespace
      }
    }
  }
  
  const fetchConfigs = async () => {
    loading.value = true
    try {
      const res = await axios.get('/api/nacos/configs', {
          params: {
              tenant: currNs.value,
              group: currGroup.value,
              dataId: currDataId.value,
              pageNo: page.value,
              pageSize: 10
          }
      })
      // 解析 Page 结构
      if (res.data && res.data.pageItems) {
          configList.value = res.data.pageItems
          total.value = res.data.totalCount
      } else {
          configList.value = []
      }
    } catch(e) {
      ElMessage.error('查询失败: ' + e.message)
    } finally { loading.value = false }
  }
  
  const openEdit = async (row) => {
    if (row) {
      editDialog.isNew = false
      editForm.dataId = row.dataId
      editForm.group = row.group
      editForm.type = row.type
      // 获取详情
      const res = await axios.get('/api/nacos/config/detail', {
          params: { tenant: currNs.value, dataId: row.dataId, group: row.group }
      })
      editForm.content = typeof res.data === 'object' ? JSON.stringify(res.data) : res.data
    } else {
      editDialog.isNew = true
      editForm.dataId = ''
      editForm.content = ''
    }
    editDialog.visible = true
  }
  
  const publishConfig = async () => {
    try {
      await axios.post('/api/nacos/config/publish', {
          tenant: currNs.value,
          ...editForm
      })
      ElMessage.success('发布成功')
      editDialog.visible = false
      fetchConfigs()
    } catch(e) { ElMessage.error('发布失败') }
  }
  
  const deleteConfig = async (row) => {
    try {
      await axios.post('/api/nacos/config/delete', {
          tenant: currNs.value,
          dataId: row.dataId,
          group: row.group
      })
      ElMessage.success('已删除')
      fetchConfigs()
    } catch(e) { ElMessage.error('删除失败') }
  }
  
  onMounted(checkConnection)
  </script>
  
  <style scoped>
  .view-container { height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
  .header { padding: 15px 20px; border-bottom: 1px solid var(--el-border-color-light); display: flex; justify-content: space-between; align-items: center; background: #fff; }
  .header-left { display: flex; align-items: center; gap: 10px; }
  .header h2 { margin: 0; font-size: 18px; }
  
  .empty-state { flex: 1; display: flex; align-items: center; justify-content: center; }
  .content-body { padding: 20px; display: flex; flex-direction: column; flex: 1; overflow: hidden; }
  .filter-bar { display: flex; gap: 10px; margin-bottom: 15px; }
  .table-card { border: none; flex: 1; display: flex; flex-direction: column; }
  .code-editor :deep(textarea) { font-family: Consolas, monospace; font-size: 13px; line-height: 1.5; }
  .pagination { margin-top: 15px; display: flex; justify-content: flex-end; }
  </style>