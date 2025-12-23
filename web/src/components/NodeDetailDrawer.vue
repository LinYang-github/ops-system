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
  import axios from 'axios'
  import VChart from 'vue-echarts'
  import { use } from 'echarts/core'
  import { CanvasRenderer } from 'echarts/renderers'
  import { LineChart } from 'echarts/charts'
  import { GridComponent, TooltipComponent, TitleComponent, AxisPointerComponent } from 'echarts/components'
  
  // 注册 ECharts 组件
  use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, TitleComponent, AxisPointerComponent])
  
  const props = defineProps(['modelValue', 'nodeInfo'])
  const emit = defineEmits(['update:modelValue'])
  
  const visible = ref(false)
  const node = ref(null)
  let timer = null
  
  // 图表配置
  const cpuOption = ref({})
  const memOption = ref({})
  
  // 监听打开
  watch(() => props.modelValue, (val) => {
    visible.value = val
    if (val && props.nodeInfo) {
      node.value = props.nodeInfo
      loadData()
      // 5秒刷新一次图表
      timer = setInterval(loadData, 5000)
    } else {
      if (timer) clearInterval(timer)
    }
  })
  
  const handleClose = () => {
    emit('update:modelValue', false)
    if (timer) clearInterval(timer)
  }
  
  // 加载数据
  const loadData = async () => {
    if (!node.value) return
    
    const now = Math.floor(Date.now() / 1000)
    const start = now - 600 // 过去10分钟
  
    // 并行请求 CPU 和 Memory
    // 注意：URL 参数风格完全兼容 Prometheus
    const [cpuRes, memRes] = await Promise.all([
      axios.get('/api/monitor/query_range', { params: { query: 'node_cpu_usage', instance: node.value.ip, start, end: now } }),
      axios.get('/api/monitor/query_range', { params: { query: 'node_mem_usage', instance: node.value.ip, start, end: now } })
    ])
  
    cpuOption.value = buildChartOption(cpuRes.data, 'CPU (%)', '#409EFF')
    memOption.value = buildChartOption(memRes.data, 'Memory (%)', '#67C23A')
  }
  
  // 构建 ECharts Option (适配 Prometheus 结构)
  const buildChartOption = (apiRes, title, color) => {
    let data = []
    // 解析 Prometheus 格式: data.result[0].values = [[ts, "val"], ...]
    if (apiRes.data && apiRes.data.result && apiRes.data.result.length > 0) {
      data = apiRes.data.result[0].values.map(item => ({
        value: [
          new Date(item[0] * 1000), // X轴时间
          parseFloat(item[1])       // Y轴数值
        ]
      }))
    }
  
    return {
      tooltip: { trigger: 'axis' },
      grid: { top: 30, right: 20, bottom: 20, left: 50 },
      xAxis: { type: 'time', splitLine: { show: false } },
      yAxis: { type: 'value', max: 100, min: 0 },
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
  .mb-4 { margin-bottom: 20px; }
  .chart-section { margin-bottom: 20px; }
  .chart-title { font-size: 14px; font-weight: bold; margin-bottom: 10px; border-left: 3px solid #409EFF; padding-left: 8px; }
  .chart-wrapper { height: 200px; border: 1px solid #eee; border-radius: 4px; padding: 10px; }
  .chart { height: 100%; width: 100%; }
  </style>