# GDOS 工程设计文档 (Engineering Design Document)

## 1. 项目背景与设计目标

### 1.1 背景
在中小型基础设施环境中（数十至数百节点），使用 Kubernetes (K8s) 往往面临维护成本过高、资源开销过大、学习曲线陡峭的问题。而传统的 Ansible/SaltStack 缺乏实时监控和可视化交互能力。GDOS 旨在填补这一空白，提供一个**轻量级、实时交互、开箱即用**的进程级运维平台。

### 1.2 设计目标
*   **零依赖部署 (Zero Dependency)**：Master 和 Worker 均为单二进制文件，无外部数据库（如 MySQL/Redis）依赖，前端资源内嵌。
*   **跨平台兼容 (Cross-Platform)**：原生支持 Linux 和 Windows 的进程管理、文件系统及系统信号处理。
*   **实时可观测 (Real-time Observability)**：秒级监控数据推送，全双工 Web 终端支持。
*   **开发友好 (Developer Friendly)**：清晰的分层架构，便于二次开发和功能扩展。

---

## 2. 需求范围定义

### 2.1 核心功能 (In-Scope)
*   **节点纳管**：自动注册、心跳保活、静态/动态信息采集。
*   **应用编排**：基于制品包（ZIP）的分发、解压、版本管理。
*   **进程控制**：启动、停止、重启、销毁；支持原生进程及纳管外部进程。
*   **实时监控**：CPU、内存、磁盘、IO、网络流量。
*   **远程终端**：基于 Web 的 SSH 级交互体验。
*   **灾备**：元数据快照备份与恢复。

### 2.2 非功能目标 (Non-Goals)
*   **容器编排**：不替代 Docker/K8s，不涉及容器网络（CNI）和存储卷（CSI）管理。
*   **高可用集群 (HA)**：Master 为单点架构，不引入 Raft/Paxos 等分布式共识算法。
*   **复杂微服务治理**：不提供服务网格（Service Mesh）、链路追踪等功能。

---

## 3. 总体架构设计

采用 **Master-Worker** 架构，控制面与数据面分离。

### 3.1 架构风格
*   **Control Plane (Master)**：负责元数据存储、任务调度、状态聚合、Web 接入。
*   **Data Plane (Worker)**：负责具体指令执行、数据采集、日志流转。

### 3.2 通信模型
*   **Master -> Worker (Control)**：目前采用 HTTP RESTful 请求（*注：存在 NAT 穿透限制，规划迁移至 WebSocket*）。
*   **Worker -> Master (Heartbeat/Report)**：HTTP RESTful 上报。
*   **Bi-directional (Stream)**：WebSocket 用于日志流和 Web 终端。

### 3.3 模块边界
*   **API Layer**：处理 HTTP 请求，参数校验，协议转换。
*   **Manager Layer**：核心业务逻辑，状态维护，缓存管理。
*   **Executor Layer (Worker)**：屏蔽操作系统差异，执行底层系统调用。

---

## 4. 目录结构与模块说明

项目遵循 standard Go project layout 变体。

```text
ops-system/
├── cmd/                  # 入口文件 (main.go)
│   ├── master/           # Master 启动入口 (参数解析, 依赖注入)
│   └── worker/           # Worker 启动入口
├── internal/             # 私有业务代码
│   ├── master/
│   │   ├── api/          # HTTP Handlers (Router, Middleware)
│   │   ├── manager/      # 业务逻辑层 (System, Node, Package)
│   │   ├── db/           # 数据持久化 (SQLite Init)
│   │   ├── monitor/      # 内存时序数据库 (RingBuffer)
│   │   └── ws/           # WebSocket Hub (广播中心)
│   └── worker/
│       ├── executor/     # 执行器 (核心：Process Group, Job Objects)
│       ├── agent/        # 代理层 (心跳, 注册)
│       └── handler/      # Worker 自身的 HTTP Server
├── pkg/                  # 公共库 (被 Master/Worker 共享)
│   ├── protocol/         # 通信协议 (JSON Structs)
│   ├── storage/          # 存储抽象 (Local/MinIO Interface)
│   └── utils/            # 工具类 (HTTP Client, IP Resolving)
└── web/                  # Vue3 前端源码
```

---

## 5. 核心数据结构与配置设计

### 5.1 配置设计
采用 `spf13/viper` 管理配置，优先级：**数据库动态配置 > 命令行参数 > 配置文件 > 默认值**。
*   **静态配置**：端口、数据库路径、存储后端（MinIO/Local）。
*   **动态配置**：心跳间隔、超时时间、告警阈值（支持热更新）。

### 5.2 核心数据模型 (`pkg/protocol`)
*   **`ServiceManifest` (service.json)**：描述服务的静态属性（启动命令、环境变量、包版本）。
*   **`SystemModule`**：描述服务在特定系统中的编排属性（启动顺序、健康检查覆盖）。
*   **`InstanceInfo`**：描述运行时的实例状态（PID、Uptime、节点归属）。

### 5.3 数据库 Schema
使用 SQLite 存储元数据，表结构设计如下：
*   `system_infos`: 业务系统定义。
*   `node_infos`: 节点基础信息（不含高频监控数据）。
*   `instance_infos`: 实例运行状态。
*   `sys_settings`: 全局 KV 配置。

---

## 6. 核心流程设计

### 6.1 Worker 注册与心跳
1.  Worker 启动，读取本地配置。
2.  周期性向 Master 发送 `HeartbeatRequest`。
3.  Master 更新内存缓存 (`metricsCache`) 和数据库 (`last_heartbeat`)。
4.  Master 在响应中携带 `GlobalConfig`，Worker 接收后动态调整 Ticker。

### 6.2 应用部署 (Async Deploy)
1.  **Request**: 用户发起部署请求 -> Master。
2.  **Pre-write**: Master 将实例状态置为 `deploying` 并入库。
3.  **Dispatch**: Master 异步发送指令给 Worker。
4.  **Execute**: Worker 立即返回 200 OK，后台启动协程下载、解压。
5.  **Callback**: Worker 完成后，调用 Master `status_report` 接口更新状态为 `stopped`/`error`。

### 6.3 进程启动 (Start Process)
1.  Worker 读取 `service.json`。
2.  **平台适配**：
    *   **Windows**: 创建 Job Object，将新进程加入 Job。
    *   **Linux**: 调用 `Setsid` 创建新的 Session/Process Group。
3.  **PID 记录**：将 PID 写入 `instances/xxx/pid` 文件。
4.  **就绪检测**：执行 TCP/HTTP Probe，通过后上报 `running`。

---

## 7. 错误处理与异常场景

### 7.1 错误传播
*   **后端**：统一使用 `pkg/response` 封装，返回 `{code, msg, data}`。
*   **错误码**：定义 `pkg/code`，如 `20001` (NodeOffline), `40005` (PackageInvalid)。
*   **前端**：Axios 拦截器统一捕获非 0 错误码，弹出 `ElMessage`，业务逻辑无需重复 `try-catch`。

### 7.2 异常恢复
*   **Master 重启**：从 SQLite 恢复元数据；监控数据归零，等待 Worker 下次心跳自动填充（自愈）。
*   **Worker 重启**：扫描 `instances` 目录，读取 PID 文件，重新接管已有进程的监控，不影响业务运行。

---

## 8. 并发与性能管理

### 8.1 混合存储策略
*   **写压力**：高频监控数据（CPU/Mem）仅写入 **Master 内存**，不落盘。只有元数据变更（注册、状态改变）才写 SQLite。
*   **读压力**：列表查询优先读内存 Map，避免 SQL 查询瓶颈。

### 8.2 连接复用
*   全局单例 `http.Client`，开启 Keep-Alive，复用 TCP 连接。
*   WebSocket 推送采用 **节流 (Throttling)** 机制，每秒最多广播一次快照，防止广播风暴。

### 8.3 大文件分发
*   **MinIO 模式**：Master 仅生成 Presigned URL，流量直传（Browser -> MinIO -> Worker），Master 无带宽压力。
*   **Local 模式**：使用 `multipart/form-data` 流式读写，内存占用恒定（不随文件大小增加）。

---

## 9. 安全性与稳定性考虑

### 9.1 安全性
*   **Token 鉴权**：Master/Worker 通信强制校验 `Authorization: Bearer <Secret>`。
*   **路径安全**：文件操作严格校验路径，防止 Zip Slip 漏洞或路径遍历攻击。

### 9.2 稳定性
*   **进程守护**：利用 Job Objects/PGID 确保停止服务时能够清理所有子进程，防止僵尸进程。
*   **无锁设计**：高频读写使用 `sync.Map` 或细粒度锁，避免全局锁竞争。

---

## 10. 可测试性与可维护性

*   **单元测试**：Manager 层依赖 Interface 而非具体实现，使用 `:memory:` 模式的 SQLite 进行快速测试。
*   **E2E 测试**：提供 Mock Worker 工具 (`cmd/test-tool/mock_cluster.go`)，可模拟 1000+ 节点进行压力测试。
*   **代码解耦**：通过依赖注入（Dependency Injection）组装 ServerHandler，模块间无全局变量耦合。

---

## 11. 设计取舍与替代方案

| 决策点 | 当前方案 | 替代方案 | 取舍原因 |
| :--- | :--- | :--- | :--- |
| **数据库** | **SQLite** | MySQL/PG | 为了实现“单文件部署”的极简体验，牺牲了部分扩展性。 |
| **通信模型** | **Master Push (HTTP)** | Worker Pull | 当前实现简单，但存在 NAT 穿透问题。**这是目前最大的架构债务。** |
| **监控存储** | **内存 RingBuffer** | Prometheus | 降低部署复杂度。设计上已兼容 Prometheus 数据格式，方便未来迁移。 |
| **前端集成** | **go:embed** | 独立部署 Nginx | 简化部署流程，运维人员只需管理一个二进制文件。 |

---

## 12. 已知问题与演进方向

### 12.1 技术债
1.  **NAT 穿透问题**：Master 主动连接 Worker 的 HTTP 接口，导致 Master 无法管理 NAT 后的 Worker。
    *   *演进*：控制流全量切换至 WebSocket 双向隧道。
2.  **日志检索性能**：实时 Tail 无法满足复杂的历史日志检索需求。
    *   *演进*：引入轻量级日志索引或通过 Worker 端 grep 实现。

### 12.2 未来扩展
1.  **依赖编排**：基于 DAG 的服务启动顺序控制。
2.  **任务队列**：引入持久化任务队列（Task Queue），保证 Master 宕机后的任务断点续传。
3.  **插件机制**：允许用户编写 Lua/Go Plugin 扩展监控指标或控制逻辑。