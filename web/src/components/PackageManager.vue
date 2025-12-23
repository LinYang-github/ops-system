<template>
  <div class="view-container">
    
    <!-- 工具栏 (保持不变) -->
    <div class="toolbar">
      <div class="toolbar-left">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索服务名称..."
          prefix-icon="Search"
          clearable
          style="width: 300px"
        />
      </div>
      <div class="toolbar-right">
        <el-button type="primary" icon="Upload" @click="showUploadDialog = true">
          上传新版本
        </el-button>
        <el-button icon="Refresh" circle @click="fetchPackages" :loading="loading" />
      </div>
    </div>

    <!-- 主表格区域 (保持不变) -->
    <el-card shadow="never" class="table-card">
      <el-table 
        :data="filteredPackages" 
        style="width: 100%" 
        v-loading="loading" 
        stripe
        highlight-current-row
      >
        <el-table-column label="服务名称" min-width="200">
          <template #default="scope">
            <div class="service-identity">
              <el-avatar shape="square" :size="32" class="service-icon">
                {{ scope.row.name.substring(0, 1).toUpperCase() }}
              </el-avatar>
              <span class="service-name">{{ scope.row.name }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="版本统计" width="150">
          <template #default="scope">
            <el-tag type="info" effect="plain" round>{{ scope.row.versions.length }} 个版本</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="最新版本" width="150">
          <template #default="scope">
            <el-tag type="success">{{ getLatestVersion(scope.row.versions) }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column prop="last_upload" label="最近更新" width="180" sortable>
          <template #default="scope">
            {{ formatTime(scope.row.last_upload) }}
          </template>
        </el-table-column>

        <el-table-column label="操作" width="120" fixed="right" align="center">
          <template #default="scope">
            <el-button link type="primary" @click="openDetail(scope.row)">
              管理详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 上传弹窗 (保持不变) -->
    <el-dialog v-model="showUploadDialog" title="上传服务包" width="500px">
      <div class="upload-container">
        <el-upload
          class="upload-drag"
          drag
          action="/api/upload"
          :on-success="handleUploadSuccess"
          :on-error="handleUploadError"
          :before-upload="beforeUpload"
          :show-file-list="false"
        >
          <el-icon class="el-icon--upload"><upload-filled /></el-icon>
          <div class="el-upload__text">
            拖拽 ZIP 文件到此处，或 <em>点击选择</em>
          </div>
          <template #tip>
            <div class="el-upload__tip">
              <ul>
                <li>文件必须包含 <b>service.json</b> 描述文件</li>
              </ul>
            </div>
          </template>
        </el-upload>
      </div>
    </el-dialog>

    <!-- 服务详情抽屉 (更新) -->
    <el-drawer
      v-model="drawer.visible"
      :title="drawer.title"
      size="600px"
      direction="rtl"
    >
      <div v-if="drawer.data" class="drawer-content">
        
        <div class="drawer-header-info">
          <p>您可以查看任意版本的配置详情（service.json）或下载原始包。</p>
        </div>

        <el-timeline>
          <el-timeline-item
            v-for="ver in sortVersions(drawer.data.versions)"
            :key="ver"
            :timestamp="`版本: v${ver}`"
            placement="top"
            :type="ver === getLatestVersion(drawer.data.versions) ? 'primary' : ''"
          >
            <el-card shadow="hover" class="version-card">
              <div class="version-row">
                <div class="version-info">
                  <el-tag size="small" effect="dark" v-if="ver === getLatestVersion(drawer.data.versions)">LATEST</el-tag>
                  <span class="v-text">v{{ ver }}</span>
                </div>
                <div class="version-actions">
                  <!-- 新增：查看配置按钮 -->
                  <el-button 
                    link 
                    type="primary" 
                    icon="Document" 
                    @click="viewManifest(drawer.data.name, ver)"
                  >
                    配置
                  </el-button>
                  <el-divider direction="vertical" />
                  
                  <el-link 
                    type="primary" 
                    :href="`/download/${drawer.data.name}/${ver}.zip`" 
                    :underline="false"
                    target="_blank"
                    icon="Download"
                  >
                    下载
                  </el-link>
                  <el-divider direction="vertical" />

                  <el-popconfirm 
                    title="确定删除此版本吗？" 
                    @confirm="handleDelete(drawer.data.name, ver)"
                  >
                    <template #reference>
                      <el-button link type="danger" icon="Delete">删除</el-button>
                    </template>
                  </el-popconfirm>
                </div>
              </div>
            </el-card>
          </el-timeline-item>
        </el-timeline>
      </div>
    </el-drawer>

    <!-- 新增：配置详情查看弹窗 -->
    <el-dialog v-model="manifestDialog.visible" title="服务配置详情 (service.json)" width="600px">
      <div v-loading="manifestDialog.loading">
         <!-- 使用 pre 标签展示格式化后的 JSON -->
         <div class="json-viewer">
            <pre>{{ manifestDialog.content }}</pre>
         </div>
      </div>
      <template #footer>
        <el-button @click="manifestDialog.visible = false">关闭</el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'
import { UploadFilled, Search, Upload, Refresh, Download, Delete, Document } from '@element-plus/icons-vue'

// --- 状态定义 ---
const rawPackages = ref([])
const loading = ref(false)
const searchKeyword = ref('')
const showUploadDialog = ref(false)

const drawer = ref({ visible: false, title: '', data: null })

// 新增：配置详情弹窗状态
const manifestDialog = ref({
  visible: false,
  loading: false,
  content: ''
})

// --- 计算属性 ---
const filteredPackages = computed(() => {
  if (!searchKeyword.value) return rawPackages.value
  const kw = searchKeyword.value.toLowerCase()
  return rawPackages.value.filter(p => p.name.toLowerCase().includes(kw))
})

// --- API 交互 ---

const fetchPackages = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/packages')
    rawPackages.value = (res.data || []).sort((a, b) => b.last_upload - a.last_upload)
  } catch (err) {
    ElMessage.error('获取列表失败')
  } finally {
    loading.value = false
  }
}

const openDetail = (row) => {
  drawer.value.title = `服务详情: ${row.name}`
  drawer.value.data = row
  drawer.value.visible = true
}

// 新增：查看配置详情
const viewManifest = async (name, version) => {
  manifestDialog.value.visible = true
  manifestDialog.value.loading = true
  manifestDialog.value.content = ''
  
  try {
    const res = await axios.get(`/api/packages/manifest`, {
      params: { name, version }
    })
    // 格式化 JSON
    manifestDialog.value.content = JSON.stringify(res.data, null, 2)
  } catch (err) {
    manifestDialog.value.content = '获取配置失败: ' + (err.response?.data || err.message)
  } finally {
    manifestDialog.value.loading = false
  }
}

const beforeUpload = (file) => {
  if (!file.name.endsWith('.zip')) {
    ElMessage.error('仅支持 ZIP 格式')
    return false
  }
  return true
}

const handleUploadSuccess = (res) => {
  ElMessage.success(`上传成功: ${res.service} v${res.version}`)
  showUploadDialog.value = false
  fetchPackages()
}

const handleUploadError = (err) => {
  ElMessage.error('上传失败: ' + err.message)
}

const handleDelete = async (name, version) => {
  try {
    await axios.post('/api/packages/delete', { name, version })
    ElMessage.success('已删除')
    await fetchPackages()
    const currentPkg = rawPackages.value.find(p => p.name === name)
    if (currentPkg && currentPkg.versions.length > 0) {
      drawer.value.data = currentPkg
    } else {
      drawer.value.visible = false
    }
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

const formatTime = (ts) => new Date(ts * 1000).toLocaleString()

const getLatestVersion = (versions) => {
  if (!versions || versions.length === 0) return '-'
  return [...versions].sort().pop()
}

const sortVersions = (versions) => {
  return [...versions].sort().reverse()
}

onMounted(fetchPackages)
</script>

<style scoped>
.view-container {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  background-color: var(--el-bg-color);
  padding: 12px 16px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-light);
}

.toolbar-right { display: flex; gap: 10px; }
.table-card { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
.service-identity { display: flex; align-items: center; gap: 10px; }
.service-icon { background-color: var(--el-color-primary-light-8); color: var(--el-color-primary); font-weight: bold; }
.service-name { font-weight: 600; font-size: 14px; }
.drawer-content { padding: 0 10px; }
.drawer-header-info { margin-bottom: 20px; color: var(--el-text-color-secondary); font-size: 13px; }
.version-card { border-radius: 4px; }
.version-row { display: flex; justify-content: space-between; align-items: center; }
.version-info { display: flex; align-items: center; gap: 8px; }
.v-text { font-weight: bold; font-size: 15px; }
.upload-container { padding: 20px 0; text-align: center; }

/* JSON Viewer 样式 */
.json-viewer {
  background-color: #f4f4f5;
  padding: 15px;
  border-radius: 6px;
  max-height: 400px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
}
.json-viewer pre {
  margin: 0;
  font-family: Consolas, Monaco, monospace;
  font-size: 13px;
  color: #303133;
}
/* 暗黑模式适配 */
:global(.dark) .json-viewer {
  background-color: #1e1e1e;
  border-color: #4c4d4f;
}
:global(.dark) .json-viewer pre {
  color: #cfd3dc;
}
</style>