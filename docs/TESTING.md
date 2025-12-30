# 🧪 GDOS 测试指南 (Testing Guide)

本文档详细介绍了 GDOS 项目中的测试工具集、测试应用及使用方法，涵盖功能验证、稳定性测试与性能压测。

## 1. 测试资源目录结构

所有测试相关的代码均整理在 `cmd/tests/` 目录下：

```text
cmd/tests/
├── apps/               # [被管应用] 用于验证 Master/Worker 的管理能力
│   ├── demo-app/       # 综合测试应用 (HTTP, Env注入, 信号处理)
│   ├── hello/          # 短作业测试 (执行完即退出)
│   ├── logger/         # 日志测试 (无限循环打印日志)
│   ├── web/            # 简单 Web Server
│   └── script/         # 脚本执行测试 (.bat/.sh)
└── tools/              # [测试工具] 用于向系统施压
    ├── mock_cluster.go # 全链路模拟器 (Go) - 测试逻辑稳定性与前端渲染
    └── load_test.js    # 极限压测脚本 (k6) - 测试 API 吞吐量
```

---

## 2. 单元测试 (Unit Test)

在提交代码前，请确保核心逻辑通过单元测试。

```bash
# 运行所有单元测试
go test ./...

# 运行特定模块测试 (例如 Manager 层)
go test -v ./internal/master/manager/...
```

---

## 3. 功能验证测试 (Functional Testing)

使用 `cmd/tests/apps/` 下的靶子应用，验证 GDOS 的核心编排能力。

### 3.1 准备工作
编译测试应用（Windows 示例）：
```bash
# 编译 demo-app
go build -o demo-app.exe ./cmd/tests/apps/demo-app/main.go

# 编译 logger
go build -o logger.exe ./cmd/tests/apps/logger/main.go
```

### 3.2 验证场景

| 测试应用 | 验证功能点 | 操作步骤 | 预期结果 |
| :--- | :--- | :--- | :--- |
| **demo-app** | 环境变量注入<br>优雅停机<br>端口监听 | 1. 使用 `pack-tool` 打包<br>2. 上传并部署<br>3. 配置 Env 变量 | 1. 启动成功，状态变为 Running<br>2. 访问 `http://IP:Port` 返回 Hello<br>3. 点击停止，日志显示 "Bye Bye" |
| **logger** | 实时日志流<br>日志轮转 | 1. 部署并启动<br>2. 打开 Web 终端日志 | 1. Web 端能看到实时滚动的日志<br>2. `instances/` 目录下日志文件按配置轮转 |
| **hello** | 短作业管理<br>自动退出状态 | 1. 部署并启动 | 1. 进程启动后迅速退出<br>2. Master 状态自动变为 `stopped` (或 `error` 取决于退出码) |

---

## 4. 全链路稳定性测试 (Mock Cluster)

使用 `mock_cluster.go` 模拟大量 Worker 节点，用于测试 Master 的内存稳定性、并发处理逻辑及前端的大屏渲染性能。

### 4.1 特性
*   **真实模拟**：模拟真实的心跳协议，响应 Master 的动态配置下发。
*   **负载仿真**：CPU/内存数据呈正弦波波动，而非随机数。
*   **网络模拟**：支持模拟网络丢包 (Loss) 和抖动 (Jitter)。

### 4.2 使用方法

**基本用法 (启动 200 个节点)：**
```bash
go run cmd/tests/tools/mock_cluster.go -count 200
```

**高阶用法 (模拟弱网环境)：**
```bash
# 模拟 500 个节点，20% 丢包，最大 1秒 网络延迟，持续 10 分钟
go run cmd/tests/tools/mock_cluster.go \
  -count 500 \
  -loss 20 \
  -jitter 1000 \
  -duration 10m
```

### 4.3 观测重点
1.  **Master 内存**：观察 `MemoryTSDB` 是否因高基数导致 OOM。
2.  **前端页面**：打开“节点管理”页面，检查是否卡顿；打开“Dashboard”，检查图表是否断裂。
3.  **日志**：Master 日志中不应出现大量的 `SQLITE_BUSY` 错误。

---

## 5. 极限性能压测 (Load Testing)

使用 **k6** 运行 `load_test.js`，主要测试 Master HTTP 接口的极限 QPS。

### 5.1 前置要求
请先安装 k6 工具 (参考 [k6 官方文档](https://k6.io/docs/get-started/installation/))。

### 5.2 运行压测

```bash
# 确保 Master 已在 localhost:8080 启动
k6 run cmd/tests/tools/load_test.js
```

### 5.3 结果解读示例

```text
  ✓ status is 200
  ✓ duration < 100ms

  http_reqs......................: 4500.000000/s  <-- 重点关注：每秒处理请求数
  http_req_duration..............: p(95)=12.45ms  <-- 重点关注：95% 请求的延迟
```

*   **基准线**：在普通开发机上，QPS 应大于 2000，P95 延迟应小于 50ms。

---

## 6. 测试工具对比

| 工具 | `mock_cluster.go` | `load_test.js` |
| :--- | :--- | :--- |
| **测试对象** | 全链路逻辑 (Manager/DB/Cache) | HTTP 接口吞吐量 |
| **真实度** | 高 (模拟业务逻辑) | 低 (固定包体) |
| **主要用途** | 验证稳定性、内存泄露、UI渲染 | 验证 API 性能极限、网关瓶颈 |
| **建议频率** | 每次大版本发布前运行 | 架构调整后运行 |

---

## 7. 常见问题 (FAQ)

**Q: Mock Cluster 启动后，前端页面没有任何节点？**
*   A: 请检查 mock 工具的 `-master` 参数是否正确指向了 Master 地址。如果 Master 开启了鉴权，请确保 mock 工具代码中的 Token 与 Master 配置一致。

**Q: k6 压测出现大量 `connection refused`？**
*   A: Master 可能已经崩溃或端口耗尽。请检查 Master 日志，或尝试降低 k6 脚本中的 `vus` (并发用户数)。

**Q: Demo App 启动失败，提示找不到文件？**
*   A: 请检查 `service.json` 中的 `entrypoint` 路径是否与压缩包内的实际路径一致。Windows 下注意 `.exe` 后缀。