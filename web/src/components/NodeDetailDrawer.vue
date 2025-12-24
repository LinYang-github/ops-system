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
        <div class="chart-title">CPU 使用率趋势 (10min)</div>
        <div class="chart-wrapper">
          <v-chart class="chart" :option="cpuOption" autoresize />
        </div>
      </div>

      <div class="chart-section">
        <div class="chart-title">内存使用率趋势 (10min)</div>
        <div class="chart-wrapper">
          <v-chart class="chart" :option="memOption" autoresize />
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, watch, onUnmounted } from 'vue'
// 【关键修改 1】使用封装的 request 替代 axios
import request from '../utils/request' 
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
let timer = null

const cpuOption = ref({})
const memOption = ref({})

watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.nodeInfo) {
    node.value = props.nodeInfo
    loadData()
    timer = setInterval(loadData, 5000)
  } else {
    if (timer) clearInterval(timer)
  }
})

const handleClose = () => {
  emit('update:modelValue', false)
  if (timer) clearInterval(timer)
}

const loadData = async () => {
  if (!node.value) return
  
  const now = Math.floor(Date.now() / 1000)
  const start = now - 600

  try {
    // 【关键修改 2】request.get 返回的直接是业务数据
    // 结构为: { status: "success", data: { resultType: "matrix", result: [...] } }
    const [cpuRes, memRes] = await Promise.all([
      request.get('/api/monitor/query_range', { params: { query: 'node_cpu_usage', instance: node.value.ip, start, end: now } }),
      request.get('/api/monitor/query_range', { params: { query: 'node_mem_usage', instance: node.value.ip, start, end: now } })
    ])

    // 【关键修改 3】直接传入 res，不需要再 .data
    cpuOption.value = buildChartOption(cpuRes, 'CPU (%)', '#409EFF')
    memOption.value = buildChartOption(memRes, 'Memory (%)', '#67C23A')
  } catch (e) {
    console.error("Load chart data failed", e)
  }
}

// 构建 ECharts Option
const buildChartOption = (apiRes, title, color) => {
  let data = []
  
  // 解析 Prometheus 格式
  // apiRes 结构: { status: "success", data: { result: [...] } }
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
    yAxis: { type: 'value', min: 0 }, // 内存不设最大值，自适应
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

onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.detail-container { padding: 0 10px; }
.mb-4 { margin-bottom: 20px; }
.chart-section { margin-bottom: 20px; }
.chart-title { font-size: 14px; font-weight: bold; margin-bottom: 10px; border-left: 3px solid #409EFF; padding-left: 8px; }
.chart-wrapper { height: 200px; border: 1px solid #eee; border-radius: 4px; padding: 10px; }
.chart { height: 100%; width: 100%; }
</style>