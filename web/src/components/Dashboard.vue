<template>
  <div class="view-container dashboard">
    
    <!-- ========================= -->
    <!-- 1. 核心指标卡片 (Metrics)  -->
    <!-- ========================= -->
    <el-row :gutter="20" class="mb-4">
      <!-- 卡片 1: 节点健康度 -->
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div class="card-icon green-bg">
            <el-icon><Monitor /></el-icon>
          </div>
          <div class="card-info">
            <div class="card-label">节点健康度</div>
            <div class="card-value">{{ onlineNodes }} / {{ totalNodes }}</div>
            <el-progress 
              :percentage="nodeHealthRate" 
              :show-text="false" 
              status="success" 
              :stroke-width="6" 
              class="mt-2" 
            />
          </div>
        </el-card>
      </el-col>

      <!-- 卡片 2: 实例运行数 -->
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div class="card-icon blue-bg">
            <el-icon><Platform /></el-icon>
          </div>
          <div class="card-info">
            <div class="card-label">服务实例 (Running)</div>
            <div class="card-value">{{ runningInstances }} / {{ totalInstances }}</div>
            <el-progress 
              :percentage="instanceHealthRate" 
              :show-text="false" 
              :stroke-width="6" 
              class="mt-2" 
            />
          </div>
        </el-card>
      </el-col>

      <!-- 卡片 3: 集群负载 -->
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div class="card-icon orange-bg">
            <el-icon><Cpu /></el-icon>
          </div>
          <div class="card-info">
            <div class="card-label">集群平均负载</div>
            <div class="card-value">
              {{ avgCpu }}% <span class="sub-val">CPU</span>
            </div>
            <div class="sub-text">MEM: {{ avgMem }}%</div>
          </div>
        </el-card>
      </el-col>

      <!-- 卡片 4: 告警状态 -->
      <el-col :span="6">
        <el-card 
          shadow="hover" 
          class="data-card" 
          :class="{ 'alert-mode': activeAlerts > 0 }"
        >
          <div class="card-icon red-bg">
             <el-badge 
               :value="activeAlerts" 
               :max="99" 
               :hidden="activeAlerts === 0" 
               class="badge-offset"
             >
               <el-icon><BellFilled /></el-icon>
             </el-badge>
          </div>
          <div class="card-info">
            <div class="card-label">活跃告警</div>
            <div class="card-value" :class="{'text-danger': activeAlerts > 0}">
              {{ activeAlerts }}
            </div>
            <div class="sub-text">
              {{ activeAlerts > 0 ? '请立即处理' : '系统运行正常' }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- ========================= -->
    <!-- 2. 图表区域 (Charts)       -->
    <!-- ========================= -->
    <el-row :gutter="20" class="mb-4 row-charts">
      <!-- 左侧：集群资源趋势 -->
      <el-col :span="16">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span>集群资源实时趋势 (Session)</span>
              <el-tag size="small" type="info">Real-time</el-tag>
            </div>
          </template>
          <div class="chart-box">
             <v-chart class="chart" :option="lineOption" autoresize />
          </div>
        </el-card>
      </el-col>
      
      <!-- 右侧：实例状态分布 -->
      <el-col :span="8">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header"><span>实例状态分布</span></div>
          </template>
          <div class="chart-box">
            <v-chart class="chart" :option="pieOption" autoresize />
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- ========================= -->
    <!-- 3. 动态与审计 (Lists)      -->
    <!-- ========================= -->
    <el-row :gutter="20" class="row-lists">
      
      <!-- 实时告警列表 -->
      <el-col :span="12">
        <el-card shadow="never" class="list-card">
          <template #header>
            <div class="card-header">
              <span>🔥 实时活跃告警</span>
              <el-button link type="primary" size="small">查看全部</el-button>
            </div>
          </template>
          
          <el-table 
            :data="alertList" 
            style="width: 100%" 
            size="small" 
            :show-header="false"
          >
             <el-table-column width="140">
               <template #default="scope">
                 <span class="time-text">{{ formatTime(scope.row.start_time) }}</span>
               </template>
             </el-table-column>
             
             <el-table-column show-overflow-tooltip>
               <template #default="scope">
                 <span class="text-danger font-bold">{{ scope.row.target_name }}</span>
                 <span class="text-gray mx-2">-</span>
                 <span>{{ scope.row.message }}</span>
               </template>
             </el-table-column>
             
             <el-table-column width="80" align="right">
               <template #default>
                 <el-tag type="danger" size="small" effect="plain">Firing</el-tag>
               </template>
             </el-table-column>
          </el-table>
          <el-empty v-if="alertList.length === 0" description="暂无告警" :image-size="40" />
        </el-card>
      </el-col>

      <!-- 最近操作日志 -->
      <el-col :span="12">
        <el-card shadow="never" class="list-card">
          <template #header>
            <div class="card-header">
              <span>📝 最近操作审计</span>
              <el-button link type="primary" size="small">更多日志</el-button>
            </div>
          </template>
          
          <el-table 
            :data="logList" 
            style="width: 100%" 
            size="small" 
            :show-header="false"
          >
             <el-table-column width="140">
               <template #default="scope">
                 <span class="time-text">{{ formatTime(scope.row.create_time) }}</span>
               </template>
             </el-table-column>
             
             <el-table-column width="120" show-overflow-tooltip>
               <template #default="scope">
                 <el-tag size="small" effect="light">{{ scope.row.operator }}</el-tag>
               </template>
             </el-table-column>
             
             <el-table-column show-overflow-tooltip>
               <template #default="scope">
                 <span class="log-action">{{ scope.row.action }}</span>
                 <span class="text-gray"> {{ scope.row.target_name }}</span>
               </template>
             </el-table-column>
             
             <el-table-column width="70" align="right">
               <template #default="scope">
                 <span :class="scope.row.status === 'success' ? 'text-success' : 'text-danger'">
                    {{ scope.row.status }}
                 </span>
               </template>
             </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, reactive } from 'vue'
import request from '../utils/request'
import { wsStore } from '../store/wsStore'
import { Monitor, Platform, Cpu, BellFilled } from '@element-plus/icons-vue'

// ECharts
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, PieChart } from 'echarts/charts'
import { 
  GridComponent, TooltipComponent, LegendComponent, TitleComponent 
} from 'echarts/components'

use([
  CanvasRenderer, LineChart, PieChart, 
  GridComponent, TooltipComponent, LegendComponent, TitleComponent
])

// ==========================================
// 1. 核心指标计算 (Computed Metrics)
// ==========================================

const totalNodes = computed(() => wsStore.nodes.length)
const onlineNodes = computed(() => 
  wsStore.nodes.filter(n => n.status === 'online').length
)
const nodeHealthRate = computed(() => 
  totalNodes.value ? (onlineNodes.value / totalNodes.value) * 100 : 0
)

const allInstances = computed(() => {
  let list = []
  wsStore.systems.forEach(sys => {
    if (sys.instances) list = list.concat(sys.instances)
  })
  return list
})

const totalInstances = computed(() => allInstances.value.length)
const runningInstances = computed(() => 
  allInstances.value.filter(i => i.status === 'running').length
)
const instanceHealthRate = computed(() => 
  totalInstances.value ? (runningInstances.value / totalInstances.value) * 100 : 0
)

const avgCpu = computed(() => {
  if (onlineNodes.value === 0) return 0
  const sum = wsStore.nodes.reduce((acc, cur) => 
    acc + (cur.status === 'online' ? (cur.cpu_usage || 0) : 0), 0
  )
  return (sum / onlineNodes.value).toFixed(1)
})

const avgMem = computed(() => {
  if (onlineNodes.value === 0) return 0
  const sum = wsStore.nodes.reduce((acc, cur) => 
    acc + (cur.status === 'online' ? (cur.mem_usage || 0) : 0), 0
  )
  return (sum / onlineNodes.value).toFixed(1)
})

const activeAlerts = computed(() => wsStore.activeAlertCount)

// ==========================================
// 2. 列表数据获取 (Lists)
// ==========================================

const alertList = ref([])
const logList = ref([])

const loadLists = async () => {
  // 加载告警
  const resAlert = await request.get('/api/alerts/events')
  if (resAlert && resAlert.active) {
    alertList.value = resAlert.active.slice(0, 5) // Top 5
  }
  // 加载日志
  const resLog = await request.post('/api/logs', { page: 1, page_size: 5 })
  if (resLog && resLog.list) {
    logList.value = resLog.list
  }
}

// ==========================================
// 3. 图表配置 (Charts Option)
// ==========================================

// Line Chart (CPU/Mem Trend)
const lineDataCPU = ref([])
const lineDataMem = ref([])
const lineOption = ref({})

// Pie Chart (Instance Status)
const pieOption = computed(() => {
  const counts = { Running: 0, Stopped: 0, Error: 0, Deploying: 0 }
  allInstances.value.forEach(i => {
    if (counts[i.status] !== undefined) counts[i.status]++
    else counts.Error++ // Unknown as Error
  })
  
  return {
    tooltip: { trigger: 'item' },
    legend: { bottom: '0%', left: 'center' },
    series: [
      {
        name: '实例状态',
        type: 'pie',
        radius: ['40%', '70%'],
        avoidLabelOverlap: false,
        itemStyle: { 
          borderRadius: 5, 
          borderColor: '#fff', 
          borderWidth: 2 
        },
        label: { show: false, position: 'center' },
        emphasis: { 
          label: { show: true, fontSize: 16, fontWeight: 'bold' } 
        },
        data: [
          { value: counts.Running, name: 'Running', itemStyle: { color: '#67C23A' } },
          { value: counts.Stopped, name: 'Stopped', itemStyle: { color: '#909399' } },
          { value: counts.Error, name: 'Error', itemStyle: { color: '#F56C6C' } },
          { value: counts.Deploying, name: 'Deploying', itemStyle: { color: '#409EFF' } },
        ]
      }
    ]
  }
})

// 定时更新趋势图逻辑
let timer = null

const updateChart = () => {
  const now = new Date().toLocaleTimeString()
  
  // 维护最近 60 个点 (3分钟)
  if (lineDataCPU.value.length > 60) {
    lineDataCPU.value.shift()
    lineDataMem.value.shift()
  }
  
  lineDataCPU.value.push({ name: now, value: [now, avgCpu.value] })
  lineDataMem.value.push({ name: now, value: [now, avgMem.value] })

  lineOption.value = {
    tooltip: { trigger: 'axis' },
    legend: { data: ['CPU', 'MEM'] },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', boundaryGap: false, show: false }, 
    yAxis: { type: 'value', max: 100 },
    series: [
      {
        name: 'CPU',
        type: 'line',
        smooth: true,
        showSymbol: false,
        areaStyle: { opacity: 0.1 },
        data: lineDataCPU.value,
        itemStyle: { color: '#409EFF' }
      },
      {
        name: 'MEM',
        type: 'line',
        smooth: true,
        showSymbol: false,
        areaStyle: { opacity: 0.1 },
        data: lineDataMem.value,
        itemStyle: { color: '#67C23A' }
      }
    ]
  }
}

// ==========================================
// 4. 辅助函数 (Utils)
// ==========================================

const formatTime = (ts) => {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${d.getMonth()+1}-${d.getDate()} ${d.getHours()}:${d.getMinutes()}`
}

// ==========================================
// 5. 生命周期 (Lifecycle)
// ==========================================

onMounted(() => {
  loadLists()
  timer = setInterval(() => {
    updateChart()
  }, 3000)
  // 初次执行
  updateChart()
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
/* 容器布局 */
.dashboard {
  padding: 20px;
  overflow-y: auto;
  background-color: var(--el-bg-color-page);
}

/* 卡片通用样式 */
.data-card {
  display: flex;
  align-items: center;
  border: none;
  cursor: pointer;
  transition: transform 0.2s;
}

.data-card:hover {
  transform: translateY(-3px);
}

.data-card :deep(.el-card__body) {
  display: flex;
  align-items: center;
  padding: 20px;
  width: 100%;
}

/* 图标色块 */
.card-icon {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
  color: #fff;
  margin-right: 16px;
}

.green-bg { background: linear-gradient(135deg, #85eec3 0%, #4ace8e 100%); }
.blue-bg { background: linear-gradient(135deg, #a0cfff 0%, #409eff 100%); }
.orange-bg { background: linear-gradient(135deg, #ffd666 0%, #ff9c6e 100%); }
.red-bg { background: linear-gradient(135deg, #ff9a9e 0%, #f56c6c 100%); }

.card-info {
  flex: 1;
}

.card-label {
  font-size: 14px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}

.card-value {
  font-size: 24px;
  font-weight: bold;
  color: var(--el-text-color-primary);
}

.sub-val {
  font-size: 12px;
  color: #999;
  margin-left: 4px;
}

.sub-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

/* 通用 Helper 类 */
.mt-2 { margin-top: 8px; }
.mb-4 { margin-bottom: 20px; }
.mx-2 { margin: 0 8px; }

/* 告警角标微调 */
.badge-offset :deep(.el-badge__content) {
  transform: translate(10px, -10px);
}

/* 图表卡片 */
.chart-card {
  min-height: 350px;
  display: flex;
  flex-direction: column;
}

.chart-box {
  height: 300px;
  width: 100%;
}

.chart {
  width: 100%;
  height: 100%;
}

/* 列表卡片 */
.list-card {
  min-height: 300px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: bold;
}

/* 文本颜色 */
.time-text {
  font-family: monospace;
  color: #999;
}

.font-bold {
  font-weight: bold;
}

.text-danger { color: var(--el-color-danger); }
.text-success { color: var(--el-color-success); }
.text-gray { color: #ccc; }

.log-action {
  font-weight: 500;
  color: var(--el-color-primary);
}
</style>