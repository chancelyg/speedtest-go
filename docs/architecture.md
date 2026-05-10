# 架构说明

## 整体架构

Speedtest-Go 采用单体架构，所有功能集成在单个二进制文件中，通过 Go 的 `embed` 功能将前端资源内嵌到后端程序中。

```
┌─────────────────────────────────────────┐
│           客户端浏览器                   │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐ │
│  │  HTML   │  │   CSS   │  │   JS    │ │
│  └────┬────┘  └────┬────┘  └────┬────┘ │
└───────┼────────────┼────────────┼──────┘
        │            │            │
        └────────────┴────────────┘
                     │
              HTTP Request
                     │
┌────────────────────┼────────────────────┐
│              Go HTTP Server             │
│  ┌─────────────────┼─────────────────┐  │
│  │   Static Files  │   API Routes    │  │
│  │   (index.html)  │                 │  │
│  │   (style.css)   │  /api/config    │  │
│  │   (app.js)      │  /api/ip        │  │
│  │                 │  /api/ping      │  │
│  │   (embed.FS)    │  /api/download  │  │
│  │                 │  /api/upload    │  │
│  └─────────────────┴─────────────────┘  │
│                                         │
│  ┌─────────────┐    ┌─────────────────┐ │
│  │   Config    │    │    Handler      │ │
│  │  (env vars) │───▶│  (business logic)│ │
│  └─────────────┘    └─────────────────┘ │
└─────────────────────────────────────────┘
```

## 代码结构

```
speedtest-go/
├── main.go                    # 程序入口
│   ├── 加载配置
│   ├── 构建路由
│   └── 启动 HTTP 服务器
│
├── internal/
│   ├── config/                # 配置管理包
│   │   ├── config.go          # 配置结构体和加载逻辑
│   │   └── config_test.go     # 配置单元测试
│   │
│   └── handler/               # HTTP 处理器包
│       ├── handler.go         # API 端点实现
│       └── handler_test.go    # 处理器单元测试
│
└── static/                    # 前端静态资源
    ├── index.html             # 主页面结构
    ├── style.css              # 样式和主题
    └── app.js                 # 前端逻辑
```

## 后端架构

### HTTP 路由

使用 Go 标准库的 `net/http` 包，通过 `http.ServeMux` 进行路由分发：

```go
mux.Handle("/", http.FileServer(http.FS(sub)))     // 静态资源
mux.HandleFunc("/api/config", h.ConfigHandler)      // 配置接口
mux.HandleFunc("/api/ip", h.IPHandler)              // IP 接口
mux.HandleFunc("/api/ping", h.PingHandler)          // 延迟探测
mux.HandleFunc("/api/download", h.DownloadHandler)  // 下载测速
mux.HandleFunc("/api/upload", h.UploadHandler)      // 上传测速
```

### 配置管理

配置通过环境变量读取，支持默认值和范围验证：

- `envStr`: 字符串类型，空值使用默认值
- `envPort`: 端口类型，验证是否为有效数字
- `envMode`: 模式类型，验证 `size` 或 `time`
- `envDuration`: 持续时间，最小 1 秒
- `envInt`: 整数类型，支持最小/最大值限制

### 测速逻辑

#### 下载测速

1. **时间模式**：使用分块传输编码，持续发送随机数据直到时间结束
2. **数据量模式**：设置 `Content-Length`，发送固定大小的随机数据

数据通过 `crypto/rand` 生成，每次写入 256 KB 的块。

#### 上传测速

接收 POST 请求的请求体，通过 `io.Copy(io.Discard, r.Body)` 读取并丢弃数据，返回接收到的字节数。

#### 延迟测量

前端通过发送多次 `/api/ping` 请求，计算 RTT（往返时间）的平均值、标准差和丢包率。

### IP 提取

支持从以下来源提取客户端真实 IP：

1. `X-Forwarded-For` 头（取第一个 IP）
2. `X-Real-Ip` 头
3. `RemoteAddr`（直接连接）

## 前端架构

### 技术栈

- **纯原生技术**：无框架依赖，HTML5 + CSS3 + ES6
- **CSS 变量**：管理暗色/亮色主题
- **Fetch API**：网络请求
- **ReadableStream**：下载数据流处理
- **XMLHttpRequest**：上传进度监控

### 模块化设计

前端代码按功能划分为多个区域：

1. **i18n 模块**：多语言字典和切换逻辑
2. **主题模块**：暗色/亮色主题检测和切换
3. **DOM 助手**：元素选择和更新工具函数
4. **测速引擎**：Ping、下载、上传测量逻辑
5. **流程控制**：测速流程编排

### 测速算法

#### 下载测速

```javascript
// 1. 启动多个并行连接
const streams = srvCfg.streams || 1;

// 2. 每个连接 fetch 下载数据
const res = await fetch('/api/download');
const reader = res.body.getReader();

// 3. 跳过预热阶段（前 2 秒）
if (now < warmupEnd) continue;

// 4. 实时计算速度
const mbps = (totalReceived * 8) / (elapsed * 1e6);
```

#### 上传测速

```javascript
// 1. 预生成 1 MB 随机数据
const uploadChunk = new Uint8Array(1024 * 1024);

// 2. 使用 XHR 监控上传进度
xhr.upload.addEventListener('progress', onProgress);

// 3. 跳过预热阶段
if (now < warmupEnd) return;

// 4. 实时计算速度
const mbps = (totalSent * 8) / (elapsed * 1e6);
```

## 数据流

### 测速流程

```
用户点击"开始测速"
    │
    ▼
┌─────────────┐
│  重置显示   │
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│  测量延迟   │────▶│  /api/ping  │
│  (20 次)    │     │  (重复调用) │
└──────┬──────┘     └─────────────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│  测量下载   │────▶│/api/download│
│  (多连接)   │     │  (数据流)   │
└──────┬──────┘     └─────────────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│  测量上传   │────▶│ /api/upload │
│  (多连接)   │     │  (POST)     │
└──────┬──────┘     └─────────────┘
       │
       ▼
┌─────────────┐
│  显示结果   │
└─────────────┘
```

## 性能考虑

### 服务端

- **零拷贝**：使用 `io.Copy` 处理上传数据，无需缓冲
- **流式传输**：下载数据直接写入响应，无需预生成
- **连接复用**：支持 HTTP Keep-Alive，减少连接开销
- **资源限制**：配置验证防止恶意输入导致资源耗尽

### 客户端

- **并行连接**：多连接同时测速，更准确反映实际带宽
- **预热丢弃**：跳过 TCP 慢启动阶段，结果更准确
- **实时更新**：使用 `requestAnimationFrame` 友好的方式更新 UI
- **内存优化**：复用上传数据块，避免重复生成

## 安全考虑

1. **输入验证**：所有环境变量都经过类型和范围验证
2. **请求方法限制**：`/api/upload` 仅接受 POST 请求
3. **缓存控制**：测速端点设置 `no-store` 防止缓存干扰
4. **IP 伪造防护**：`X-Forwarded-For` 仅取第一个 IP，防止伪造
5. **资源限制**：通过 `envInt` 的最大值限制防止资源耗尽

## 扩展性

虽然当前是单体架构，但代码结构清晰，易于扩展：

- **添加新端点**：在 `handler.go` 中添加新的 Handler 方法
- **修改前端**：直接编辑 `static/` 目录下的文件
- **添加配置**：在 `config.go` 中添加新的环境变量
- **更换前端**：替换 `static/` 目录内容即可
