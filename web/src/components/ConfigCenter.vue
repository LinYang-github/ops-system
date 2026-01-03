<template>
  <div class="view-container">
    <div class="content-body">
      <el-card shadow="never" class="main-card">
        <template #header>
          <div class="card-header">
            <span class="title">配置中心</span>
            <div class="header-extra">
              <el-tag :type="nacosConnected ? 'success' : 'info'" size="small">
                Nacos: {{ nacosConnected ? '已连接' : '未连接' }}
              </el-tag>
            </div>
          </div>
        </template>

        <el-tabs v-model="activeTab" class="config-tabs">
          <!-- Tab 1: 原生配置模板 -->
          <el-tab-pane label="原生配置模板" name="native">
            <div class="pane-content">
              <div class="filter-bar">
                <el-button type="primary" icon="Plus" @click="handleOpenTplDialog()">新建模板</el-button>
                <el-button icon="Refresh" @click="fetchTemplates" :loading="tplLoading">刷新</el-button>
              </div>

              <el-table :data="templateList" v-loading="tplLoading" stripe border>
                <el-table-column prop="name" label="模板名称" min-width="180">
                  <template #default="{ row }">
                    <span class="tpl-name" @click="handleOpenTplDialog(row)">{{ row.name }}</span>
                  </template>
                </el-table-column>
                <el-table-column prop="format" label="格式" width="100" align="center">
                  <template #default="{ row }">
                    <el-tag size="small" effect="plain">{{ row.format.toUpperCase() }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="update_time" label="更新时间" width="180">
                  <template #default="{ row }">{{ formatTime(row.update_time) }}</template>
                </el-table-column>
                <el-table-column label="操作" width="160" fixed="right" align="center">
                  <template #default="{ row }">
                    <el-button link type="primary" icon="Edit" @click="handleOpenTplDialog(row)">编辑</el-button>
                    <el-popconfirm title="确定删除此模板?" @confirm="handleDeleteTemplate(row.id)">
                      <template #reference>
                        <el-button link type="danger" icon="Delete">删除</el-button>
                      </template>
                    </el-popconfirm>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </el-tab-pane>

          <!-- Tab 2: Nacos 代理 -->
          <el-tab-pane label="Nacos 代理" name="nacos">
            <div class="pane-content">
              <div v-if="!nacosConnected" class="empty-wrapper">
                <el-empty description="Nacos 服务未配置或连接失败">
                  <el-button type="primary" plain @click="checkNacosConnection">检测并重新连接</el-button>
                </el-empty>
              </div>

              <div v-else>
                <div class="filter-bar">
                  <el-select v-model="nacosState.currNs" placeholder="命名空间" style="width: 220px" @change="fetchNacosConfigs">
                    <el-option 
                      v-for="ns in nacosState.namespaces" 
                      :key="ns.namespace" 
                      :label="ns.namespaceShowName || 'Public'" 
                      :value="ns.namespace" 
                    />
                  </el-select>
                  <el-input 
                    v-model="nacosState.currDataId" 
                    placeholder="搜索 Data ID" 
                    style="width: 250px" 
                    clearable 
                    @keyup.enter="fetchNacosConfigs"
                  >
                    <template #append>
                      <el-button icon="Search" @click="fetchNacosConfigs" />
                    </template>
                  </el-input>
                  <div class="flex-grow"></div>
                  <el-button type="success" icon="Plus" @click="handleOpenNacosDialog()">发布配置</el-button>
                </div>

                <el-table :data="nacosState.list" v-loading="nacosState.loading" border stripe>
                  <el-table-column prop="dataId" label="Data ID" show-overflow-tooltip />
                  <el-table-column prop="group" label="Group" width="180" />
                  <el-table-column label="操作" width="160" align="center">
                    <template #default="{ row }">
                      <el-button link type="primary" @click="handleOpenNacosDialog(row)">编辑</el-button>
                      <el-popconfirm title="确定从 Nacos 删除?" @confirm="handleDeleteNacos(row)">
                        <template #reference>
                          <el-button link type="danger">删除</el-button>
                        </template>
                      </el-popconfirm>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </div>

    <!-- 弹窗：原生模板编辑 (已增强) -->
    <el-dialog 
      v-model="tplDialog.visible" 
      :title="tplDialog.id ? '编辑配置模板' : '新建配置模板'" 
      width="900px"
      top="5vh"
      destroy-on-close
    >
      <div class="editor-main-layout">
        <!-- 左侧：表单与编辑器 -->
        <div class="editor-left">
          <el-form 
            ref="tplFormRef" 
            :model="tplForm" 
            :rules="tplRules" 
            label-position="top"
          >
            <el-row :gutter="20">
              <el-col :span="16">
                <el-form-item label="模板名称" prop="name">
                  <el-input v-model="tplForm.name" placeholder="例如: nginx-base.conf" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="配置格式" prop="format">
                  <el-select v-model="tplForm.format" style="width: 100%">
                    <el-option label="YAML" value="yaml" />
                    <el-option label="JSON" value="json" />
                    <el-option label="PROPERTIES" value="properties" />
                    <el-option label="TEXT" value="text" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            
            <el-form-item label="配置内容" prop="content">
              <template #label>
                <div class="editor-header">
                  <span>模板内容 (Go Template 语法)</span>
                  <el-button link type="primary" size="small" @click="copyToClipboard(tplForm.content)">复制内容</el-button>
                </div>
              </template>
              <el-input 
                ref="tplInputRef"
                v-model="tplForm.content" 
                type="textarea" 
                :rows="20" 
                class="code-editor-input" 
                placeholder="# 在此输入配置内容..."
                @input="() => syntaxError = ''"
              />
              <!-- 语法错误提示 -->
              <el-alert 
                v-if="syntaxError" 
                :title="syntaxError" 
                type="error" 
                show-icon 
                :closable="false" 
                class="syntax-error-tip" 
              />
            </el-form-item>
          </el-form>
        </div>

        <!-- 右侧：变量辅助栏 -->
        <div class="editor-right">
          <div class="variable-guide">
            <div class="guide-title">
              <el-icon><InfoFilled /></el-icon>
              <span>可用占位符</span>
            </div>
            <div v-for="group in availableVariables" :key="group.group" class="variable-card">
              <span class="variable-group-title">{{ group.group }}</span>
              <el-tooltip
                v-for="item in group.items"
                :key="item.label"
                :content="item.desc"
                placement="left"
              >
                <span class="variable-item" @click="insertVariable(item.value)">
                  {{ item.label }}
                </span>
              </el-tooltip>
            </div>
            <div class="guide-footer">
              提示：点击上方变量名可直接插入到编辑器光标位置。部署时将由系统自动渲染。
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button @click="tplDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="submitTemplate" :loading="tplDialog.saving">提交保存</el-button>
      </template>
    </el-dialog>

    <!-- 弹窗：Nacos 编辑 -->
    <el-dialog 
      v-model="nacosDialog.visible" 
      :title="nacosDialog.isNew ? '发布新配置' : '编辑 Nacos 配置'" 
      width="60%"
      top="8vh"
    >
      <el-form :model="nacosForm" label-position="top">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="Data ID">
              <el-input v-model="nacosForm.dataId" :disabled="!nacosDialog.isNew" placeholder="必填" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Group">
              <el-input v-model="nacosForm.group" placeholder="默认 DEFAULT_GROUP" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="配置内容">
          <el-input v-model="nacosForm.content" type="textarea" :rows="18" class="code-editor-input" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nacosDialog.visible = false">关闭</el-button>
        <el-button type="primary" @click="handlePublishNacos" :loading="nacosDialog.saving">执行发布</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick } from 'vue'
import request from '../utils/request'
import { ElMessage } from 'element-plus'
import { Plus, Edit, Delete, Refresh, Search, InfoFilled } from '@element-plus/icons-vue'

const activeTab = ref('native')

// --- 可用变量清单定义 ---
const availableVariables = [
  {
    group: '节点信息 (Node)',
    items: [
      { label: '.Node.IP', value: '{{ .Node.IP }}', desc: '部署节点的通信IP地址' },
      { label: '.Node.Hostname', value: '{{ .Node.Hostname }}', desc: '节点操作系统主机名' },
      { label: '.Node.ID', value: '{{ .Node.ID }}', desc: '节点的唯一UUID' },
    ]
  },
  {
    group: '服务信息 (Service)',
    items: [
      { label: '.Service.Name', value: '{{ .Service.Name }}', desc: '正在部署的服务包名称' },
      { label: '.Service.Version', value: '{{ .Service.Version }}', desc: '服务包的版本号' },
    ]
  },
  {
    group: '环境变量 (Env)',
    items: [
      { label: '.Env.KEY', value: '{{ .Env.KEY }}', desc: '引用在“业务系统->模块设置”中定义的变量。请将 KEY 替换为实际的变量名。' },
    ]
  }
]

// --- 原生模板逻辑 ---
const tplFormRef = ref(null)
const tplInputRef = ref(null)
const templateList = ref([])
const tplLoading = ref(false)
const syntaxError = ref('')
const tplDialog = reactive({ visible: false, id: '', saving: false })
const tplForm = reactive({ name: '', format: 'yaml', content: '' })
const tplRules = {
  name: [{ required: true, message: '请输入模板名称', trigger: 'blur' }],
  format: [{ required: true, message: '请选择格式', trigger: 'change' }],
  content: [{ required: true, message: '配置内容不能为空', trigger: 'blur' }]
}

const fetchTemplates = async () => {
  tplLoading.value = true
  try {
    const res = await request.get('/api/templates')
    templateList.value = Array.isArray(res) ? res : []
  } catch (error) {
    console.error('Fetch templates failed:', error)
  } finally { tplLoading.value = false }
}

const handleOpenTplDialog = (row = null) => {
  syntaxError.value = ''
  if (row) {
    tplDialog.id = row.id
    Object.assign(tplForm, { name: row.name, format: row.format, content: row.content })
  } else {
    tplDialog.id = ''
    Object.assign(tplForm, { name: '', format: 'yaml', content: '' })
  }
  tplDialog.visible = true
  nextTick(() => tplFormRef.value?.clearValidate())
}

// 插入占位符逻辑
const insertVariable = (val) => {
  const textarea = tplInputRef.value?.$el.querySelector('textarea')
  if (!textarea) return

  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const content = tplForm.content
  
  tplForm.content = content.substring(0, start) + val + content.substring(end)
  
  nextTick(() => {
    textarea.focus()
    const newPos = start + val.length
    textarea.setSelectionRange(newPos, newPos)
  })
}

// 前端语法紧前校验
const validateGoTemplate = (code) => {
  syntaxError.value = ''
  
  // 1. 基础配对校验
  const openBraces = (code.match(/{{/g) || []).length
  const closeBraces = (code.match(/}}/g) || []).length
  
  if (openBraces !== closeBraces) {
    syntaxError.value = `语法错误: 大括号不配对 ({{: ${openBraces}, }}: ${closeBraces})`
    return false
  }

  // 2. 空指令校验
  if (/{{[ \t]*}}/.test(code)) {
    syntaxError.value = '语法错误: 包含空的指令区块 {{ }}'
    return false
  }

  return true
}

const submitTemplate = async () => {
  if (!tplFormRef.value) return

  // 运行语法校验
  if (!validateGoTemplate(tplForm.content)) {
    return
  }

  await tplFormRef.value.validate(async (valid) => {
    if (!valid) return
    tplDialog.saving = true
    try {
      const url = tplDialog.id ? '/api/templates/update' : '/api/templates/create'
      await request.post(url, { ...tplForm, id: tplDialog.id })
      ElMessage.success('保存成功')
      tplDialog.visible = false
      fetchTemplates()
    } finally { tplDialog.saving = false }
  })
}

const handleDeleteTemplate = async (id) => {
  try {
    await request.post('/api/templates/delete', { id })
    ElMessage.success('删除成功')
    fetchTemplates()
  } catch(e) {}
}

// --- Nacos 逻辑 ---
const nacosConnected = ref(false)
const nacosState = reactive({ loading: false, list: [], currNs: '', namespaces: [], currDataId: '' })
const nacosDialog = reactive({ visible: false, isNew: true, saving: false })
const nacosForm = reactive({ dataId: '', group: 'DEFAULT_GROUP', content: '' })

const checkNacosConnection = async () => {
  try {
    const res = await request.get('/api/nacos/settings')
    if (res && res.url) {
      nacosConnected.value = true
      const nsRes = await request.get('/api/nacos/namespaces')
      nacosState.namespaces = nsRes.data || []
      if (!nacosState.currNs && nacosState.namespaces.length > 0) {
        nacosState.currNs = nacosState.namespaces[0].namespace
      }
      fetchNacosConfigs()
    }
  } catch (e) { 
    nacosConnected.value = false 
  }
}

const fetchNacosConfigs = async () => {
  if (!nacosConnected.value) return
  nacosState.loading = true
  try {
    const res = await request.get('/api/nacos/configs', {
      params: { 
        tenant: nacosState.currNs, 
        dataId: nacosState.currDataId, 
        pageNo: 1, 
        pageSize: 50 
      }
    })
    nacosState.list = res.pageItems || []
  } finally { nacosState.loading = false }
}

const handleOpenNacosDialog = async (row = null) => {
  if (row) {
    nacosDialog.isNew = false
    nacosForm.dataId = row.dataId
    nacosForm.group = row.group
    nacosDialog.saving = true
    try {
      const content = await request.get('/api/nacos/config/detail', { 
        params: { tenant: nacosState.currNs, dataId: row.dataId, group: row.group } 
      })
      nacosForm.content = typeof content === 'string' ? content : JSON.stringify(content, null, 2)
    } finally { nacosDialog.saving = false }
  } else {
    nacosDialog.isNew = true
    Object.assign(nacosForm, { dataId: '', group: 'DEFAULT_GROUP', content: '' })
  }
  nacosDialog.visible = true
}

const handlePublishNacos = async () => {
  if (!nacosForm.dataId || !nacosForm.content) {
    return ElMessage.warning('Data ID 和内容不能为空')
  }
  nacosDialog.saving = true
  try {
    await request.post('/api/nacos/config/publish', { 
      tenant: nacosState.currNs, ...nacosForm, type: 'yaml' 
    })
    ElMessage.success('发布成功')
    nacosDialog.visible = false
    fetchNacosConfigs()
  } finally { nacosDialog.saving = false }
}

const handleDeleteNacos = async (row) => {
  try {
    await request.post('/api/nacos/config/delete', { 
      tenant: nacosState.currNs, dataId: row.dataId, group: row.group 
    })
    ElMessage.success('已从 Nacos 删除')
    fetchNacosConfigs()
  } catch(e) {}
}

// --- 通用工具 ---
const formatTime = (ts) => {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false })
}

const copyToClipboard = (text) => {
  navigator.clipboard.writeText(text).then(() => ElMessage.success('已复制到剪贴板'))
}

onMounted(() => {
  fetchTemplates()
  checkNacosConnection()
})
</script>

<style scoped>
.view-container { 
  height: 100%; 
  padding: 20px;
  background-color: var(--el-bg-color-page);
  box-sizing: border-box;
}

.main-card { 
  height: 100%;
  display: flex;
  flex-direction: column;
}

.card-header { 
  display: flex; 
  justify-content: space-between; 
  align-items: center; 
}

.title { font-size: 16px; font-weight: 600; }

.config-tabs { height: 100%; }
:deep(.el-tabs__content) { padding: 0; }

.pane-content { padding-top: 15px; }

.filter-bar { 
  display: flex; 
  align-items: center; 
  gap: 12px; 
  margin-bottom: 20px; 
}

.flex-grow { flex-grow: 1; }

.tpl-name { 
  font-weight: 600; 
  color: var(--el-color-primary); 
  cursor: pointer;
}

/* 编辑器布局 */
.editor-main-layout {
  display: flex;
  gap: 20px;
}
.editor-left { flex: 1; min-width: 0; }
.editor-right { width: 240px; border-left: 1px solid var(--el-border-color-lighter); padding-left: 20px; }

/* 变量引导样式 */
.guide-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: bold;
  color: var(--el-text-color-primary);
  margin-bottom: 15px;
}
.variable-card {
  background: var(--el-fill-color-light);
  border-radius: 6px;
  padding: 12px;
  margin-bottom: 15px;
}
.variable-group-title {
  font-size: 12px;
  font-weight: bold;
  color: var(--el-text-color-secondary);
  margin-bottom: 10px;
  display: block;
  text-transform: uppercase;
}
.variable-item {
  display: block;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  padding: 6px 10px;
  margin-bottom: 6px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
  color: var(--el-color-primary);
}
.variable-item:hover {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  transform: translateX(4px);
}
.guide-footer {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
  font-style: italic;
}

/* 编辑器外观 */
.code-editor-input :deep(.el-textarea__inner) {
  font-family: 'Fira Code', 'Consolas', monospace;
  background-color: #282c34;
  color: #abb2bf;
  padding: 15px;
  line-height: 1.6;
  font-size: 13px;
}

.syntax-error-tip {
  margin-top: 10px;
}

.editor-header {
  display: flex;
  justify-content: space-between;
  width: 100%;
  align-items: center;
}

.empty-wrapper {
  margin-top: 100px;
}

:deep(.el-table) {
  border-radius: 8px;
  overflow: hidden;
}
</style>