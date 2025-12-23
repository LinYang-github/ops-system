ops-system/
├── cmd/
│   ├── master/          # Master 入口
│   │   └── main.go
│   └── worker/          # Worker 入口
│       └── main.go
├── configs/             # 配置文件模板
├── pkg/
│   ├── protocol/        # 核心通讯协议（Request/Response Structs）
│   ├── utils/           # 通用工具（ZIP解压、文件操作、IP获取）
│   └── sysinfo/         # 系统信息采集封装
├── internal/
│   ├── master/
│   │   ├── api/         # Master HTTP Handlers
│   │   ├── store/       # 内存数据存储（节点列表、任务状态）
│   │   └── manager/     # 业务逻辑（包管理）
│   └── worker/
│       ├── agent/       # 注册与心跳逻辑
│       ├── handler/     # Worker HTTP Handlers (接收指令)
│       └── executor/    # 执行器（CMD、部署、启停）
└── uploads/             # Master 存放上传的服务包