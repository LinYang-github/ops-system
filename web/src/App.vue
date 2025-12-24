<template>
  <el-config-provider :locale="zhCn">
    <div class="common-layout" :class="{ dark: isDark }">
      <el-container class="layout-container">
        <!-- 左侧导航栏 -->
        <el-aside width="220px" class="aside-menu">
          <div class="logo-area">
            <el-icon :size="24" color="#409EFF" style="margin-right: 8px"><Platform /></el-icon>
            <h3>系统运维</h3>
          </div>
          
          <el-menu
            :default-active="activeMenu"
            class="el-menu-vertical"
            background-color="transparent"
            text-color="var(--el-text-color-regular)"
            active-text-color="var(--el-color-primary)"
            :unique-opened="true"
          >
            <!-- 1. 节点管理 -->
            <el-menu-item index="nodes" @click="handleMenuSelect('nodes')">
              <el-icon><Monitor /></el-icon>
              <span>节点管理</span>
            </el-menu-item>
            
            <!-- 2. 业务系统 (改为子菜单) -->
            <el-sub-menu index="systems">
              <template #title>
                <el-icon><Operation /></el-icon>
                <span>业务系统</span>
              </template>
              
              <!-- 创建按钮项 -->
              <el-menu-item index="create-sys" @click="openCreateDialog">
                <el-icon><Plus /></el-icon>
                <span style="font-weight: bold; color: var(--el-color-primary)">新建系统</span>
              </el-menu-item>

              <!-- 系统列表 -->
              <el-menu-item 
                v-for="sys in systemList" 
                :key="sys.id" 
                :index="`sys-${sys.id}`"
                @click="handleSystemSelect(sys.id)"
              >
                <span>{{ sys.name }}</span>
              </el-menu-item>
            </el-sub-menu>

            <!-- 3. 服务包管理 -->
            <el-menu-item index="packages" @click="handleMenuSelect('packages')">
              <el-icon><Box /></el-icon>
              <span>服务包管理</span>
            </el-menu-item>
            <!-- 4. 操作日志 -->
            <el-menu-item index="logs" @click="handleMenuSelect('logs')">
              <el-icon><Document /></el-icon>
              <span>操作日志</span>
            </el-menu-item>
            <!-- 5. 配置中心 -->
            <el-menu-item index="config" @click="handleMenuSelect('config')">
              <el-icon><Setting /></el-icon>
              <span>配置中心</span>
            </el-menu-item>
            <el-divider style="margin: 10px 0" />
    
            <!-- 系统维护组 -->
            <el-menu-item index="backups" @click="handleMenuSelect('backups')">
              <el-icon><Coin /></el-icon> <!-- 或者 use <Files /> -->
              <span>数据备份</span>
            </el-menu-item>
            <el-menu-item index="alerts" @click="handleMenuSelect('alerts')">
              <el-icon><Bell /></el-icon><span>告警中心</span>
            </el-menu-item>
          </el-menu>
        </el-aside>

        <el-container>
          <!-- 顶部 Header -->
          <el-header class="layout-header">
            <div class="header-left">
              <span class="breadcrumb">{{ headerTitle }}</span>
            </div>
            <div class="header-right">
              <!-- [新增] 告警铃铛 -->
              <div class="header-action-item" @click="handleMenuSelect('alerts')" title="查看告警">
                <el-badge :value="wsStore.activeAlertCount" :max="99" :hidden="wsStore.activeAlertCount === 0" class="alert-badge">
                  <el-icon :size="20"><Bell /></el-icon>
                </el-badge>
              </div>
              
              <el-divider direction="vertical" />
              <el-switch
                v-model="isDark"
                inline-prompt
                :active-icon="Moon"
                :inactive-icon="Sunny"
                style="margin-right: 15px; --el-switch-on-color: #2c2c2c;"
                @change="toggleDark"
              />
            </div>
          </el-header>

          <!-- 主内容区域 -->
          <el-main class="layout-main">
            <Transition name="fade-transform" mode="out-in">
              <KeepAlive include="NodeManager,PackageManager">
                <!-- 关键：这里传入 systemId prop，并监听 refresh 事件 -->
                <component 
                  :is="currentComponent" 
                  :targetSystemId="selectedSystemId"
                  @refresh-systems="fetchSystems" 
                />
              </KeepAlive>
            </Transition>
          </el-main>
        </el-container>
      </el-container>

      <!-- 全局：创建系统弹窗 -->
      <el-dialog v-model="createSysDialog" title="新建业务系统" width="400px">
        <el-form :model="newSys" label-width="80px">
          <el-form-item label="名称"><el-input v-model="newSys.name" /></el-form-item>
          <el-form-item label="描述"><el-input v-model="newSys.description" /></el-form-item>
        </el-form>
        <template #footer>
          <el-button type="primary" @click="createSystem">创建</el-button>
        </template>
      </el-dialog>

    </div>
  </el-config-provider>
</template>

<script setup>
import { ref, reactive, computed, onMounted, defineAsyncComponent } from 'vue'
import request from './utils/request'
import { ElMessage } from 'element-plus'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import { Moon, Sunny, Monitor, Box, Operation, Platform, Plus, Document, Coin  } from '@element-plus/icons-vue'
import { wsStore, connectWebSocket } from './store/wsStore' // 引入 Store

// 引入组件
import NodeManager from './components/NodeManager.vue'
import PackageManager from './components/PackageManager.vue'
import SystemManager from './components/SystemManager.vue'
import OpLog from './components/OpLog.vue' 
import ConfigCenter from './components/ConfigCenter.vue'
import BackupManager from './components/BackupManager.vue'
import AlertCenter from './components/AlertCenter.vue'

// 状态
const isDark = ref(false)
const activeMenu = ref('nodes')
const selectedSystemId = ref('') // 传递给子组件的 ID
// 修改获取数据的方式：直接从 wsStore 读取
// 注意：App.vue 本身用到的 systemList 用于渲染左侧菜单
const systemList = computed(() => wsStore.systems)

// 创建系统相关
const createSysDialog = ref(false)
const newSys = reactive({ name: '', description: '' })

// 计算当前组件
const currentComponent = computed(() => {
  if (activeMenu.value === 'alerts') return AlertCenter
  if (activeMenu.value === 'backups') return BackupManager
  if (activeMenu.value === 'config') return ConfigCenter
  if (activeMenu.value === 'logs') return OpLog
  if (activeMenu.value === 'packages') return PackageManager
  if (activeMenu.value.startsWith('sys-')) return SystemManager
  return NodeManager
})

// 计算标题
const headerTitle = computed(() => {
  if (activeMenu.value === 'alerts') return '告警中心'
  if (activeMenu.value === 'backups') return '数据灾备中心'
  if (activeMenu.value === 'logs') return '操作日志审计'
  if (activeMenu.value === 'packages') return '服务包发布中心'
  if (activeMenu.value === 'nodes') return '基础设施节点监控'
  if (activeMenu.value.startsWith('sys-')) {
    const sys = systemList.value.find(s => `sys-${s.id}` === activeMenu.value)
    return sys ? `业务系统 / ${sys.name}` : '业务系统管理'
  }
  return ''
})

// --- 逻辑方法 ---

const fetchSystems = async () => {
  try {
    const res = await request.get('/api/systems')
    // 手动更新 store，避免等到下一次推送
    wsStore.systems = res.data || []
    
    // 如果当前选中的系统被删了，回退到节点管理
    if (activeMenu.value.startsWith('sys-')) {
      const exists = systemList.value.some(s => `sys-${s.id}` === activeMenu.value)
      if (!exists) activeMenu.value = 'nodes'
    }
  } catch (e) {
    console.error(e)
  }
}

const handleMenuSelect = (index) => {
  activeMenu.value = index
}

const handleSystemSelect = (sysId) => {
  selectedSystemId.value = sysId
  activeMenu.value = `sys-${sysId}`
}

const openCreateDialog = () => {
  createSysDialog.value = true
}

const createSystem = async () => {
  if(!newSys.name) return ElMessage.warning('请输入名称')
  try {
    const res = await request.post('/api/systems/create', newSys)
    ElMessage.success('创建成功')
    createSysDialog.value = false
    newSys.name = ''
    newSys.description = ''
    
    // 刷新列表并自动跳转到新系统
    await fetchSystems()
    if(res.id) {
      handleSystemSelect(res.id)
    }
  } catch(e) {
    ElMessage.error('创建失败')
  }
}

const toggleDark = (val) => {
  const html = document.documentElement
  if (val) html.classList.add('dark')
  else html.classList.remove('dark')
}

onMounted(() => {
  connectWebSocket() // 启动 WS
  fetchSystems()
  if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    isDark.value = true
    toggleDark(true)
  }
})
</script>

<style scoped>
.layout-container { height: 100vh; }
.aside-menu {
  background-color: var(--el-bg-color);
  border-right: 1px solid var(--el-border-color-light);
  display: flex; flex-direction: column;
}
.logo-area {
  height: 60px; display: flex; align-items: center; justify-content: center;
  border-bottom: 1px solid var(--el-border-color-light);
  color: var(--el-text-color-primary);
  font-weight: bold; font-size: 18px;
}
.layout-header {
  background-color: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-light);
  display: flex; justify-content: space-between; align-items: center; height: 60px;
}
.breadcrumb { font-size: 16px; font-weight: 600; color: var(--el-text-color-primary); }
.layout-main { background-color: var(--el-fill-color-light); padding: 20px; overflow-x: hidden; }

/* Header Right Actions */
.header-right { display: flex; align-items: center; gap: 15px; }

.header-action-item {
  cursor: pointer;
  display: flex;
  align-items: center;
  color: var(--el-text-color-regular);
  transition: color 0.3s;
}
.header-action-item:hover {
  color: var(--el-color-primary);
}
/* 调整 Badge 位置 */
.alert-badge :deep(.el-badge__content) {
  top: 0px;
  right: 0px;
}

/* 动画 */
.fade-transform-enter-active, .fade-transform-leave-active { transition: all 0.3s; }
.fade-transform-enter-from { opacity: 0; transform: translateX(-10px); }
.fade-transform-leave-to { opacity: 0; transform: translateX(10px); }

html.dark .layout-main { background-color: #0d0d0d; }
</style>