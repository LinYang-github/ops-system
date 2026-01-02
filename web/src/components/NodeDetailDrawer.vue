<template>
  <el-drawer
    v-model="visible"
    :title="`节点详情: ${node?.name || node?.ip}`"
    size="700px"
    destroy-on-close
    @close="handleClose"
  >
    <div v-if="node" class="detail-container">
      <!-- 基础信息摘要 -->
      <el-descriptions border :column="2" size="small" class="mb-4">
        <el-descriptions-item label="IP">{{ node.ip }}</el-descriptions-item>
        <el-descriptions-item label="Hostname">{{ node.hostname }}</el-descriptions-item>
        <el-descriptions-item label="OS">{{ node.os }} ({{ node.arch }})</el-descriptions-item>
        <el-descriptions-item label="CPUs">{{ node.cpu_cores }} 核</el-descriptions-item>
        <el-descriptions-item label="Memory">{{ (node.mem_total / 1024).toFixed(1) }} GB</el-descriptions-item>
        <el-descriptions-item label="Disk">{{ (node.disk_total / 1024 / 1024 / 1024).toFixed(0) }} GB</el-descriptions-item>
      </el-descriptions>

      <!-- 图表区域 -->
      <div class="chart-section">
        <div class="chart-title">CPU 使用率趋势 (Real-time)</div>
        <div class="chart-wrapper">
          <v-chart class="chart" :option="cpuOption" autoresize />
        </div>
      </div>

      <div class="chart-section">
        <div class="chart-title">内存使用率趋势 (Real-time)</div>
        <div class="chart-wrapper">
          <v-chart class="chart" :option="memOption" autoresize />
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, watch, onUnmounted } from 'vue'
import request from '../utils/request' 
import { wsStore } from '../store/wsStore' // 引入 Store
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, TitleComponent, AxisPointerComponent } from 'echarts/components'

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, TitleComponent, AxisPointerComponent])

const props = defineProps(['modelValue', 'nodeInfo'])
const emit = defineEmits(['update:modelValue'])

const visible = ref(false)
const node = ref(null)

// ECharts Options
const cpuOption = ref({})
const memOption = ref({})

// [修改] 1. 监听开关，加载初始历史数据
watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.nodeInfo) {
    node.value = props.nodeInfo
    loadHistoryData()
  }
})

// [新增] 2. 监听 WebSocket Store 实现实时更新
watch(() => {
  if (!node.value) return null
  return wsStore.nodes.find(n => n.id === node.value.id)
}, (newNode) => {
  if (!newNode || !visible.value) return
  
  // 更新基础信息（如 IP 可能变化）
  node.value = newNode
  
  // 追加图表数据点
  const now = new Date()
  pushDataPoint(cpuOption, now, newNode.cpu_usage)
  pushDataPoint(memOption, now, newNode.mem_usage)
}, { deep: true }) // 深度监听对象属性变化

const handleClose = () => {
  emit('update:modelValue', false)
}

// 加载历史数据 (仅一次)
const loadHistoryData = async () => {
  if (!node.value) return
  
  const now = Math.floor(Date.now() / 1000)
  const start = now - 600 // 过去 10 分钟

  try {
    const [cpuRes, memRes] = await Promise.all([
      request.get('/api/monitor/query_range', { params: { query: 'node_cpu_usage', instance: node.value.id, start, end: now } }),
      request.get('/api/monitor/query_range', { params: { query: 'node_mem_usage', instance: node.value.id, start, end: now } })
    ])

    cpuOption.value = initChartOption(cpuRes, 'CPU (%)', '#409EFF')
    memOption.value = initChartOption(memRes, 'Memory (%)', '#67C23A')
  } catch (e) {
    console.error("Load chart data failed", e)
  }
}

// 初始化图表配置
const initChartOption = (apiRes, title, color) => {
  let data = []
  if (apiRes && apiRes.data && apiRes.data.result && apiRes.data.result.length > 0) {
    data = apiRes.data.result[0].values.map(item => ({
      value: [
        new Date(item[0] * 1000), 
        parseFloat(item[1])       
      ]
    }))
  }

  return {
    tooltip: { trigger: 'axis' },
    grid: { top: 30, right: 20, bottom: 20, left: 50 },
    xAxis: { type: 'time', splitLine: { show: false } },
    yAxis: { type: 'value', min: 0 },
    series: [{
      type: 'line',
      smooth: true,
      showSymbol: false,
      data: data,
      itemStyle: { color: color },
      areaStyle: { opacity: 0.2, color: color }
    }]
  }
}

// 追加数据点逻辑
const pushDataPoint = (optionRef, time, value) => {
  if (!optionRef.value || !optionRef.value.series || optionRef.value.series.length === 0) return
  
  const data = optionRef.value.series[0].data
  data.push({ value: [time, value] })
  
  // 保持最近 ~200 个点 (视心跳频率而定，大约 10-15 分钟)
  if (data.length > 300) {
    data.shift()
  }
}
</script>

<style scoped>
.detail-container { padding: 0 10px; }
.mb-4 { margin-bottom: 20px; }
.chart-section { margin-bottom: 20px; }
.chart-title { font-size: 14px; font-weight: bold; margin-bottom: 10px; border-left: 3px solid #409EFF; padding-left: 8px; }
.chart-wrapper { height: 200px; border: 1px solid #eee; border-radius: 4px; padding: 10px; }
.chart { height: 100%; width: 100%; }
</style>