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
├── main.go                    # 程序入口：解析配置 → 打开 store/geoip → 挂 mux → 启动 → 优雅退出
├── go.mod / go.sum            # 依赖(唯一非 stdlib 依赖:modernc.org/sqlite、prometheus/client_golang、
│                              #  oschwald/maxminddb-golang、golang.org/x/time)
├── .goreleaser.yaml           # 多平台交叉编译 + Docker + SBOM + GPG 签名
│
├── internal/
│   ├── config/                # 4 层配置合并(默认 < JSON 文件 < env < CLI)
│   │   ├── config.go          # Config 结构体 + LoadWithSources + envStr/envInt/clamp
│   │   └── config_test.go
│   │
│   ├── handler/               # HTTP 处理器
│   │   ├── handler.go         # 共享 Handler 结构体、并发信号量、ClientIP 提取
│   │   ├── health_handler.go  # GET /healthz
│   │   ├── metrics_handler.go # GET /metrics(Prometheus)
│   │   ├── ratelimit_handler.go # 每 IP 令牌桶中间件
│   │   ├── results_handler.go # /api/results (list / POST / export)
│   │   └── *_test.go
│   │
│   ├── store/                 # SQLite 历史持久化(纯 Go, modernc.org/sqlite)
│   │   ├── store.go           # Store 接口 + Result 结构体
│   │   ├── store_sqlite.go    # SQLite 实现 + ensureColumn 幂等迁移
│   │   ├── migrations/*.sql   # 0001_init + 0002_geoip_column
│   │   └── store_sqlite_test.go
│   │
│   └── geoip/                 # 可选 IP 归属地(github.com/oschwald/maxminddb-golang)
│       ├── geoip.go           # Lookup 接口 + Open() 读 .mmdb + "City, Country" 格式化
│       ├── updater.go         # 后台 goroutine:下载 tar.gz → 抽 mmdb → 原子替换 → 热插新 reader
│       └── *_test.go          # 全部通过 httptest 伪 MaxMind + stubOpener,无需真 mmdb 固件
│
└── static/                    # 前端静态资源(全部 //go:embed 进二进制)
    ├── index.html
    ├── style.css              # 暗色/亮色主题、Bufferbloat 分级、History 表格
    ├── app.js                 # 测速引擎主脚本
    ├── history.mjs            # History 表格模块(分页、导出、条件列)
    ├── toast.mjs / jitter.mjs / metrics.mjs # 拆分出来的独立模块
    ├── sw.js                  # PWA Service Worker(CACHE_NAME 由 main.go 在请求时注入 version)
    └── manifest.json / icons/
```

## 后端架构

### HTTP 路由

使用 Go 标准库 `net/http` + `http.ServeMux`(具体路径 > 前缀 > `/`,longest match 优先):

```go
mux.Handle("/", http.FileServer(http.FS(sub)))              // 静态资源(内嵌)
mux.HandleFunc("/favicon.ico", faviconHandler(sub))         // 支持 cwd override
mux.HandleFunc("/sw.js", swjsHandler(sub, version))         // 动态 CACHE_NAME 注入
mux.HandleFunc("/api/config", h.ConfigHandler)              // 前端引导配置
mux.HandleFunc("/api/ip", h.IPHandler)                      // 客户端 IP
mux.HandleFunc("/api/ping", h.PingHandler)                  // 延迟探测
mux.HandleFunc("/api/download", h.DownloadHandler)          // 下载测速(受并发信号量)
mux.HandleFunc("/api/upload", h.UploadHandler)              // 上传测速(受并发信号量)
mux.HandleFunc("/api/results", h.ResultsListOrCreate)       // History list + POST
mux.HandleFunc("/api/results/export", h.ResultsExport)      // 一次性导出
mux.HandleFunc("/healthz", h.HealthHandler)                 // 运维健康 + build 元数据
mux.HandleFunc("/metrics", h.MetricsHandler)                // Prometheus exposition
// 全链路由 h.RateLimit(mux) 中间件包裹(限流仅对测速端点生效)
// + loggingMiddleware(h, ...) 提供 access log + X-Request-Id
```

### 配置管理

`internal/config` 提供 4 层合并(优先级从低到高):**编译期默认 → JSON 文件 →
环境变量 → CLI 参数**。同一字段在多层出现时,高优先级层覆盖低优先级层。

- 每个字段的进入点都有 6 个触点:struct 字段、`defaults()`、`jsonConfig`
  pointer 字段、`overlayEnv`、CLI flag 注册、`applyCLI` 的 setter 分支
- 数值 clamp 用统一的 `clamp(n, min, max, fallback)` 函数,越界返回 fallback
  值(**不**降级到默认值),避免"看起来生效但实际没生效"的隐蔽 bug
- JSON 解析开 `DisallowUnknownFields`,拼写错误的字段名(比如 `post` vs `port`)
  会启动失败

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

`handler.ClientIP` 提取客户端真实 IP,顺序:

1. `X-Forwarded-For` 头(取第一个 IP)—— **仅当直接 peer 属于 loopback /
   RFC1918 / RFC4193 私有网段时才信任**;公网直连伪造无效
2. `X-Real-Ip` 头 —— 同上信任规则
3. `RemoteAddr`(直接连接)

同一套信任规则被限流器 (`ratelimit_handler.go`) 复用来识别令牌桶 key,避免
NAT 出口所有客户端共用一个桶。

### 历史持久化(可选)

`internal/store` 包一个纯 Go SQLite 后端(`modernc.org/sqlite`,无 CGO):

- `Store` 接口小而稳:`Save / List / Count / PruneOlderThan / Close`
- 单连接 (SetMaxOpenConns=1) + WAL journaling —— 单写多读,反正 speedtest
  不是高写入场景
- 迁移策略:**canonical `0001_init.sql` + additive `NNNN_*.sql`**。SQLite
  没有 `ADD COLUMN IF NOT EXISTS`,所以 `ensureColumn` helper 会先
  `PRAGMA table_info` 检查,column 缺失才跑 ALTER —— fresh install 走 0001
  一次到位,老 DB 升级走 ensureColumn 补差
- `SPEEDTEST_DB_PATH=""` 完全关闭这个子系统,`historyEnabled` 报 false,
  前端隐藏 History 卡片

结果由 handler 的 `createResult` 写入,**服务端强制**填写 `id` /
`created_at` / `client_ip` / `client_ip_location` / `user_agent`,客户端
伪造无效(见 `sanitiseResult`)。

### IP 归属地(可选,opt-in)

`internal/geoip` 包对 `oschwald/maxminddb-golang` 的薄封装。两种触发路径:

**路径 A**:`SPEEDTEST_GEOIP_DB=/path/to/*.mmdb` —— 启动时 `Open()` 一次,
失败就 warn + 关闭特性

**路径 B**:`SPEEDTEST_GEOIP_LICENSE_KEY=xxx` —— 触发 `Updater`
goroutine,`Run(ctx)` 循环:
```
tick() → MkdirAll → download(带 If-Modified-Since 304 短路) → tar.gz extract
→ Opener 验证 → os.Rename 原子替换 → OnSwap 回调 h.SetGeoIP(newReader)
```
下载/抽取/验证的每一步失败都是 fail-open —— warn log + 保留旧文件 + 主服务
不受影响。tick 外包一层 `tickSafe` 加 `recover` 防止 panic 静默杀死后台
goroutine。

**并发 & 热插安全**是这个子系统的载重设计。maxminddb 底层用 mmap,
`Close()` 会 `munmap`;如果一个读者在 `Locate()` 中还在扫 mmap 页面,另一个
goroutine 同时 munmap 那块内存,进程直接 SIGBUS。因此:

- `Handler.geoip` 字段私有,由 `sync.RWMutex` 守护
- `Handler.LocateIP(ip)` 在 **RLock 期间**执行 mmdb 查询 —— 读者互相不
  阻塞,但 Close 必须等所有读者结束
- `Handler.SetGeoIP(new)` 在**写锁内** Close 旧的 reader,几微秒 munmap
  时读者阻塞可接受
- `Handler.CloseGeoIP()` 供 shutdown 用:关掉当前 reader 并把 handler flag
  为"已关闭",防止 updater 在 shutdown 之后完成最后一次 tick 时 SetGeoIP 把
  新 reader 塞进死掉的 handler(会泄漏)
- `race` 测试专门加了 5 reader + 1 writer 循环 stress 200 次,确认无
  `-race` 报告

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
