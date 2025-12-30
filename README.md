# GDOS (Go Distributed Ops System)

![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)
![Vue Version](https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat&logo=vue.js)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-gray)
![License](https://img.shields.io/badge/License-MIT-blue)

**GDOS** 是一个轻量级、跨平台、去中心化的分布式运维管理平台。采用 Master-Worker 架构，后端基于 Golang，前端基于 Vue3 + Element Plus。

本项目旨在为中小规模集群提供开箱即用的应用部署、进程托管、实时监控与审计能力。系统采用单文件交付模式，无需依赖 Docker、K8s 或外部数据库，即可快速构建私有运维控制台。

## 1. 核心特性 (Features)

*   **轻量架构**：无 CGO 依赖，纯 Go 实现 SQLite 驱动；前端资源嵌入二进制文件，零依赖部署。
*   **节点管理**：Worker 自动注册与心跳保活；自动采集 OS、架构、MAC、磁盘等硬件指纹。
*   **服务编排**：
    *   **定义与运行分离**：支持服务组件（Module）规划与实例（Instance）部署解耦。
    *   **全生命周期**：支持应用的分发、部署、启动、停止及销毁。
    *   **外部纳管**：支持接管非平台部署的遗留进程（如 Nginx、MySQL），支持 PID 文件及进程名匹配策略。
*   **实时可观测**：
    *   **秒级监控**：基于 WebSocket 推送 CPU、内存、IO 速率实时数据。
    *   **Web 终端**：内置 xterm.js + PTY，提供网页版 SSH 交互能力。
    *   **告警中心**：支持自定义监控阈值与防抖动机制。
*   **混合存储**：元数据持久化至 SQLite，高频监控数据驻留内存，支持 MinIO/本地文件系统切换。
*   **健壮性**：Windows Job Objects / Unix Process Group 进程树管理，防止僵尸进程；支持 Worker 开机自启。

## 2. 技术栈 (Tech Stack)

*   **Backend**: Go 1.21+
*   **Frontend**: Vue 3, Element Plus, Vite
*   **Database**: SQLite (modernc.org/sqlite, Pure Go)
*   **Communication**: HTTP/REST (Control Plane), WebSocket (Data Plane)
*   **Terminal**: xterm.js, creack/pty
*   **Storage**: Local Filesystem / MinIO S3

## 3. 快速开始 (Quick Start)

### 前置要求
*   Go 1.21+
*   Node.js 16+ (仅构建前端需要)

### 构建步骤

```bash
# 1. 构建前端资源
cd web
npm install && npm run build
cd ..

# 2. 整理后端依赖
go mod tidy

# 3. 编译二进制文件
# Linux/macOS
go build -o master ./cmd/master/main.go
go build -o worker ./cmd/worker/main.go
go build -o pack-tool ./cmd/pack-tool/main.go

# Windows
# go build -o master.exe ./cmd/master/main.go
# go build -o worker.exe ./cmd/worker/main.go
```

### 启动运行

**启动 Master (控制节点)**
```bash
# 默认监听 :8080，数据存放在当前目录
./master
```
访问浏览器：`http://localhost:8080`

**启动 Worker (工作节点)**
```bash
# 默认连接 127.0.0.1:8080
./worker

# 指定连接远程 Master
./worker -master http://192.168.1.100:8080 -port 8081
```

## 4. 使用示例 (Usage)

### 命令行参数

**Master Server**
```bash
./master \
  -port :9090 \                  # 监听端口
  -db_path /data/ops.db \        # SQLite 数据库路径
  -store_type minio \            # 存储后端：local 或 minio
  -minio_endpoint 10.0.0.5:9000  # MinIO 地址
```

**Worker Agent**
```bash
./worker \
  -port 8082 \                   # Worker 自身监听端口
  -master http://10.0.0.1:8080 \ # Master 地址
  -work_dir /opt/instances \     # 实例运行目录
  -autostart 1                   # 设置开机自启 (需 root/admin 权限)
```

### 打包工具 (Pack Tool)
生成符合平台规范的 ZIP 服务包：

```bash
# 1. 初始化项目模板
./pack-tool init ./my-project

# 2. 编辑生成的 service.json (配置启动命令、健康检查等)

# 3. 打包
./pack-tool build ./my-project -o my-service-v1.zip
```

## 5. 项目结构 (Project Structure)

```text
ops-system/
├── cmd/                     # 程序入口
│   ├── master/              # Master 主服务
│   ├── worker/              # Worker 代理服务
│   └── pack-tool/           # CLI 打包工具
├── internal/                # 内部私有代码
│   ├── master/
│   │   ├── api/             # HTTP Handlers & Router
│   │   ├── manager/         # 核心业务逻辑 (System, Node, Package)
│   │   └── ws/              # WebSocket Hub
│   └── worker/
│       ├── executor/        # 进程执行器 (PTY, Process Group)
│       └── agent/           # 心跳与注册逻辑
├── pkg/                     # 公共库
│   ├── protocol/            # 通信协议定义
│   └── storage/             # 存储接口实现 (Local/MinIO)
└── web/                     # Vue3 前端源代码
```

## 6. 文档与扩展阅读

*   **架构设计细节**：请参阅 [DESIGN.md](./DESIGN.md) 了解 Master-Worker 通信模型及数据一致性设计。
*   **服务包规范**：关于 `service.json` 的详细配置说明，请参考 `docs/spec_service_json.md`。
*   **API 文档**：请参考 Postman 集合或 `docs/api.md`。