<template>
  <div class="app-container">
    <div class="header">
      <h2>📦 Ops Packer</h2>
      <div class="actions">
        <el-button type="primary" @click="handleSelectDir">打开源码目录</el-button>
        <span class="path-display" :title="currentDir">{{ currentDir || '未选择目录' }}</span>
      </div>
    </div>

    <div class="content" v-if="currentDir" v-loading="loading">
      <el-form :model="form" label-width="100px" size="small">
        
        <el-tabs v-model="activeTab" type="border-card">
          <!-- 1. 基础信息 -->
          <el-tab-pane label="基础信息" name="basic">
            <el-form-item label="服务名称">
              <el-input v-model="form.name" placeholder="例如: payment-service" />
            </el-form-item>
            <el-form-item label="版本号">
              <el-input v-model="form.version" placeholder="例如: 1.0.0" />
            </el-form-item>
            <el-form-item label="操作系统">
              <el-select v-model="form.os" style="width: 100%">
                <el-option label="Windows" value="windows" />
                <el-option label="Linux" value="linux" />
                <el-option label="macOS" value="darwin" />
              </el-select>
            </el-form-item>
            <el-form-item label="描述">
              <el-input v-model="form.description" type="textarea" :rows="3" />
            </el-form-item>
          </el-tab-pane>

          <!-- 2. 启动配置 -->
          <el-tab-pane label="启动/停止" name="process">
            <el-divider content-position="left">启动配置</el-divider>
            <el-form-item label="启动入口">
              <el-input v-model="form.entrypoint" placeholder="相对路径，如 bin/app.exe" />
            </el-form-item>
            
            <el-form-item label="启动参数">
              <div v-for="(arg, index) in form.argsList" :key="index" class="dynamic-row">
                <el-input v-model="arg.value" placeholder="参数值，如 -c config.yaml" />
                <el-button type="danger" icon="Delete" circle @click="removeArg(index)" />
              </div>
              <el-button type="primary" plain icon="Plus" size="small" @click="addArg" style="width: 100%">添加参数</el-button>
            </el-form-item>

            <el-divider content-position="left">停止配置 (可选)</el-divider>
            <el-form-item label="停止脚本">
              <el-input v-model="form.stop_entrypoint" placeholder="如 bin/stop.sh" />
            </el-form-item>
            <el-form-item label="停止参数">
              <div v-for="(arg, index) in form.stopArgsList" :key="index" class="dynamic-row">
                <el-input v-model="arg.value" />
                <el-button type="danger" icon="Delete" circle @click="removeStopArg(index)" />
              </div>
              <el-button type="primary" plain icon="Plus" size="small" @click="addStopArg" style="width: 100%">添加参数</el-button>
            </el-form-item>
          </el-tab-pane>

          <!-- 3. 环境与日志 -->
          <el-tab-pane label="环境/日志" name="env">
            <el-divider content-position="left">环境变量 (ENV)</el-divider>
            <div v-for="(item, index) in form.envList" :key="index" class="dynamic-row kv-row">
              <el-input v-model="item.key" placeholder="Key (e.g. GIN_MODE)" />
              <span class="eq">=</span>
              <el-input v-model="item.val" placeholder="Value (e.g. release)" />
              <el-button type="danger" icon="Delete" circle @click="removeEnv(index)" />
            </div>
            <el-button type="primary" plain icon="Plus" size="small" @click="addEnv" style="width: 100%">添加环境变量</el-button>

            <el-divider content-position="left">日志文件映射</el-divider>
             <div v-for="(item, index) in form.logList" :key="index" class="dynamic-row kv-row">
              <el-input v-model="item.key" placeholder="显示名 (e.g. Access Log)" />
              <span class="eq">-></span>
              <el-input v-model="item.val" placeholder="路径 (e.g. logs/access.log)" />
              <el-button type="danger" icon="Delete" circle @click="removeLog(index)" />
            </div>
            <el-button type="primary" plain icon="Plus" size="small" @click="addLog" style="width: 100%">添加日志配置</el-button>
          </el-tab-pane>

          <!-- 4. 高级预览 -->
          <el-tab-pane label="JSON预览" name="preview">
            <pre class="json-preview">{{ previewJson }}</pre>
          </el-tab-pane>
        </el-tabs>

      </el-form>

      <div class="footer-bar">
        <el-button @click="handleSave" icon="Files">仅保存配置</el-button>
        <el-button type="success" @click="handleBuild" icon="Box">保存并打包 ZIP</el-button>
      </div>
    </div>

    <div v-else class="empty-state">
      <p>请先选择一个包含源代码的文件夹</p>
      <el-button type="primary" size="large" @click="handleSelectDir">选择文件夹</el-button>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, computed } from 'vue'
import { SelectDir, SelectSaveFile, LoadManifest, SaveManifest, BuildPackage, InitTemplate } from '../wailsjs/go/main/App'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Plus, Files, Box } from '@element-plus/icons-vue'

const currentDir = ref('')
const activeTab = ref('basic')
const loading = ref(false)

// 表单数据 (包含转换后的 List 结构)
const form = reactive({
  name: '',
  version: '',
  description: '',
  os: 'windows',
  entrypoint: '',
  argsList: [],       // [{value: ''}]
  stop_entrypoint: '',
  stopArgsList: [],   // [{value: ''}]
  envList: [],        // [{key: '', val: ''}]
  logList: []         // [{key: '', val: ''}]
})

// --- 逻辑处理 ---

const handleSelectDir = async () => {
  const dir = await SelectDir()
  if (dir) {
    currentDir.value = dir
    loadConfig(dir)
  }
}

const loadConfig = async (dir) => {
  loading.value = true
  try {
    const jsonStr = await LoadManifest(dir)
    if (!jsonStr) {
      // 文件不存在
      ElMessageBox.confirm('该目录没有 service.json，是否初始化默认模板？', '初始化', {
        confirmButtonText: '初始化',
        cancelButtonText: '取消',
        type: 'info'
      }).then(async () => {
        await InitTemplate(dir)
        loadConfig(dir) // 重新加载
      }).catch(() => {
        currentDir.value = '' // 取消则重置
      })
      return
    }
    
    // 解析 JSON 并映射到 UI 结构
    const data = JSON.parse(jsonStr)
    form.name = data.name || ''
    form.version = data.version || ''
    form.description = data.description || ''
    form.os = data.os || 'windows'
    form.entrypoint = data.entrypoint || ''
    form.stop_entrypoint = data.stop_entrypoint || ''
    
    // 数组转换
    form.argsList = (data.args || []).map(s => ({ value: s }))
    form.stopArgsList = (data.stop_args || []).map(s => ({ value: s }))
    
    // Map 转换
    form.envList = Object.entries(data.env || {}).map(([k, v]) => ({ key: k, val: v }))
    form.logList = Object.entries(data.log_paths || {}).map(([k, v]) => ({ key: k, val: v }))
    
  } catch (e) {
    ElMessage.error("加载配置失败: " + e)
  } finally {
    loading.value = false
  }
}

// 生成符合协议的 JSON 对象
const generateJsonObj = () => {
  return {
    name: form.name,
    version: form.version,
    description: form.description,
    os: form.os,
    entrypoint: form.entrypoint,
    args: form.argsList.map(i => i.value),
    stop_entrypoint: form.stop_entrypoint,
    stop_args: form.stopArgsList.map(i => i.value),
    // 数组转对象
    env: form.envList.reduce((acc, cur) => {
      if(cur.key) acc[cur.key] = cur.val
      return acc
    }, {}),
    log_paths: form.logList.reduce((acc, cur) => {
      if(cur.key) acc[cur.key] = cur.val
      return acc
    }, {})
  }
}

const previewJson = computed(() => {
  return JSON.stringify(generateJsonObj(), null, 2)
})

const handleSave = async () => {
  if (!currentDir.value) return
  const jsonStr = JSON.stringify(generateJsonObj(), null, 2)
  try {
    await SaveManifest(currentDir.value, jsonStr)
    ElMessage.success('配置已保存')
    return true
  } catch (e) {
    ElMessage.error('保存失败: ' + e)
    return false
  }
}

const handleBuild = async () => {
  // 先保存
  if (!await handleSave()) return

  // 选择输出路径
  const defaultName = `${form.name}_${form.version}.zip`
  const destPath = await SelectSaveFile(defaultName)
  
  if (destPath) {
    loading.value = true
    try {
      const res = await BuildPackage(currentDir.value, destPath)
      // Go 方法如果返回 error 会抛出异常，否则返回 Success 字符串
      if (res && res.startsWith("Error")) {
          throw new Error(res)
      }
      ElMessage.success(`打包成功: ${destPath}`)
    } catch (e) {
      ElMessage.error('打包失败: ' + e)
    } finally {
      loading.value = false
    }
  }
}

// --- 动态增删 ---
const addArg = () => form.argsList.push({ value: '' })
const removeArg = (i) => form.argsList.splice(i, 1)
const addStopArg = () => form.stopArgsList.push({ value: '' })
const removeStopArg = (i) => form.stopArgsList.splice(i, 1)
const addEnv = () => form.envList.push({ key: '', val: '' })
const removeEnv = (i) => form.envList.splice(i, 1)
const addLog = () => form.logList.push({ key: '', val: '' })
const removeLog = (i) => form.logList.splice(i, 1)

</script>

<style>
/* 全局样式重置 */
body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; background-color: #f5f7fa; }
</style>

<style scoped>
.app-container { display: flex; flex-direction: column; height: 100vh; }

.header {
  background: #fff;
  padding: 15px 20px;
  border-bottom: 1px solid #e4e7ed;
  display: flex; justify-content: space-between; align-items: center;
}
.header h2 { margin: 0; color: #303133; }
.actions { display: flex; align-items: center; gap: 10px; }
.path-display { font-size: 12px; color: #909399; max-width: 300px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; background: #f2f6fc; padding: 4px 8px; border-radius: 4px; }

.content { flex: 1; padding: 20px; overflow-y: auto; max-width: 800px; margin: 0 auto; width: 100%; box-sizing: border-box;}
.empty-state { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; color: #909399; gap: 20px; }

.footer-bar {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid #e4e7ed;
  display: flex; justify-content: flex-end; gap: 12px;
}

.dynamic-row { display: flex; gap: 10px; margin-bottom: 10px; align-items: center; }
.kv-row .eq { color: #909399; font-weight: bold; }

.json-preview {
  background: #282c34; color: #abb2bf; padding: 15px; border-radius: 4px; font-family: monospace; font-size: 12px; overflow: auto; max-height: 400px;
}
</style>