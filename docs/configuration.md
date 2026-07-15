# 配置说明

Speedtest-Go 支持三种配置来源，从低到高优先级如下：

1. **编译期默认值**（始终存在）
2. **JSON 配置文件**（可选；按搜索顺序查找）
3. **环境变量** `SPEEDTEST_*`
4. **命令行参数** `--xxx`（最高优先级）

同一字段在多个层级出现时，更高优先级的值覆盖低优先级。未显式提供的字段保留下层的值。

## 命令行参数

所有参数均使用长格式 `--xxx`（兼容 `-xxx` 单破折号写法）：

| 参数 | 对应字段 | 说明 |
|------|---------|------|
| `--host <addr>` | Host | 绑定地址 |
| `--port <num>` | Port | 监听端口 |
| `--mode <time\|size>` | Mode | 测速模式 |
| `--duration <sec>` | Duration | 时间模式持续秒数 |
| `--streams <n>` | Streams | 并行连接数 |
| `--download-mb <n>` | DownloadMB | 下载数据量（size 模式） |
| `--upload-mb <n>` | UploadMB | 上传数据量（size 模式） |
| `--max-concurrent <n>` | MaxConcurrent | 全局并发上限 |
| `--warmup-ms <n>` | WarmupMs | 慢启动样本丢弃毫秒数 |
| `--db-path <path>` | DBPath | SQLite 历史文件；`""` 禁用持久化 |
| `--rate-per-min <n>` | RatePerMin | 每 IP 限速（0 = 不限） |
| `--geoip-db <path>` | GeoIPDBPath | 本地 mmdb 路径,启用 History 归属地列；`""` 关闭 |
| `--geoip-license-key <key>` | GeoIPLicenseKey | MaxMind license key,带上则自动下载 + 每周刷新 |
| `--geoip-edition <name>` | GeoIPEdition | MaxMind edition，默认 `GeoLite2-City` |
| `--config <path>` | — | 显式指定 JSON 配置文件路径 |
| `--version` | — | 打印版本信息并退出 |
| `--help` | — | 打印参数说明并退出 |

示例：

```bash
# 监听 9090，使用 size 模式
./speedtest --port 9090 --mode size --download-mb 50

# 显式加载配置文件，并用 CLI 覆盖其中的 port
./speedtest --config /etc/speedtest/config.json --port 9999

# 查看版本
./speedtest --version
# speedtest-go v1.2.3 (commit abc1234, built 2026-05-21T10:00:00Z)
```

## JSON 配置文件

JSON 是除环境变量之外的可选配置方式，适合需要把多项设置签入版本控制或下发到多台机器的场景。

### 搜索顺序

按以下顺序查找，**第一个存在的文件生效**：

1. `--config <path>` 命令行显式指定
2. `SPEEDTEST_CONFIG` 环境变量指定
3. `./speedtest.json`（当前工作目录）
4. `$XDG_CONFIG_HOME/speedtest/config.json`（未设置 `XDG_CONFIG_HOME` 时回退到 `$HOME/.config/speedtest/config.json`）
5. `/etc/speedtest/config.json`

显式路径（1、2）即使文件不存在也会被使用——加载失败会以错误退出，便于及时发现部署问题。隐式搜索路径（3-5）找不到时则继续向下查找，全部找不到也不会报错。

### 文件格式

所有字段均为可选。完整 schema 示例：

```json
{
  "host": "::",
  "port": "8080",
  "mode": "time",
  "duration_sec": 15,
  "download_mb": 25,
  "upload_mb": 10,
  "streams": 4,
  "max_concurrent": 10,
  "warmup_ms": 500,
  "db_path": "./speedtest.db",
  "history_retention_days": 90,
  "rate_per_min": 0,
  "geoip_db_path": "",
  "geoip_license_key": "",
  "geoip_edition": "GeoLite2-City"
}
```

### 严格模式

JSON 解析启用了 `DisallowUnknownFields`：拼写错误的字段名（例如 `post` 而不是 `port`）会导致启动失败，避免「配置看起来生效但实际没生效」的隐蔽 bug。

## 环境变量

Speedtest-Go 的所有配置均可通过环境变量进行，无需配置文件。

### 完整配置列表

| 环境变量 | 默认值 | 有效范围 | 说明 |
|---------|--------|---------|------|
| `SPEEDTEST_HOST` | `::` | 任意有效 IP | HTTP 服务器绑定地址（IPv6 通配，同时监听 v4 + v6） |
| `SPEEDTEST_PORT` | `8080` | 1-65535 | HTTP 服务器监听端口 |
| `SPEEDTEST_MODE` | `time` | `size`, `time` | 测速模式 |
| `SPEEDTEST_DURATION` | `15` | >= 1 秒 | 时间模式持续时间 |
| `SPEEDTEST_DOWNLOAD_SIZE` | `25` | 1-10240 MB | 下载数据量（size 模式） |
| `SPEEDTEST_UPLOAD_SIZE` | `10` | 1-10240 MB | 上传数据量（size 模式） |
| `SPEEDTEST_STREAMS` | `4` | 1-32 | 并行连接数（前端会额外用 `maxConcurrent` 卡一次） |
| `SPEEDTEST_MAX_CONCURRENT` | `10` | 1-100 | 全局并发测速上限（含 download + upload）；超出返回 503 |
| `SPEEDTEST_WARMUP_MS` | `500` | 0-10000 | 吞吐样本前 N ms 丢弃以规避 TCP 慢启动 |
| `SPEEDTEST_DB_PATH` | `./speedtest.db` | 任意路径或 `""` | SQLite 历史文件；空串禁用持久化 |
| `SPEEDTEST_HISTORY_RETENTION_DAYS` | `90` | 0-3650 | 历史保留天数；`0` 永不清理 |
| `SPEEDTEST_RATE_PER_MIN` | `0` | 0-100000 | 每 IP 每分钟请求上限（仅作用于 `/api/download`、`/api/upload`、`/api/ping`）；`0` 表示关闭 |
| `SPEEDTEST_CONFIG` | `""` | 任意路径 | 显式指定 JSON 配置文件路径（覆盖默认搜索顺序） |
| `SPEEDTEST_GEOIP_DB` | `""` | 任意路径 | mmdb 文件路径,启用 History 归属地列；空串关闭 |
| `SPEEDTEST_GEOIP_LICENSE_KEY` | `""` | MaxMind key | 有值则自动下载 + 每周刷新 mmdb；空串关闭 |
| `SPEEDTEST_GEOIP_EDITION` | `GeoLite2-City` | MaxMind edition | 自动下载时选用的 edition |

### 配置详解

#### SPEEDTEST_HOST

绑定地址。默认 `::` 是 IPv6 通配地址；在双栈内核（Linux 默认、macOS 默认）下，
监听 `[::]:8080` 等价于同时监听 IPv4 和 IPv6（`0.0.0.0` + `::`），不需要额外
配置即可被 v4 客户端访问。如果部署环境内核禁用了 IPv6 双栈
(`net.ipv6.bindv6only=1`)，请显式将 `SPEEDTEST_HOST` 改回 `0.0.0.0`。

```bash
# 默认值：IPv6 通配，同时接收 v4 + v6（推荐生产部署）
./speedtest

# 仅本地访问（IPv4 loopback）
SPEEDTEST_HOST=127.0.0.1 ./speedtest

# 仅本地访问（IPv6 loopback）
SPEEDTEST_HOST=::1 ./speedtest

# 监听特定 v4 网卡
SPEEDTEST_HOST=192.168.1.10 ./speedtest

# 强制仅 v4 通配（关闭 IPv6 监听）
SPEEDTEST_HOST=0.0.0.0 ./speedtest
```

##### IPv6 默认部署说明

`Addr()` 在拼接监听字符串时会自动给包含冒号的 IPv6 字面量加方括号，例如
`SPEEDTEST_HOST=::` + `SPEEDTEST_PORT=8080` → `[::]:8080`，符合
`net.Listen("tcp", addr)` 对 IPv6 字面量的语法要求。

- 单二进制部署：直接 `./speedtest`，无需任何环境变量即可在 v4/v6 双栈接收流量。
- 反向代理后端：nginx / Caddy 通常通过 `127.0.0.1` 连接上游；若上游配置成 `::`
  仍然可用（v4-mapped-v6），不必为此显式改回 `0.0.0.0`。
- systemd `ExecStart` 不需要任何变化 — 默认值已就绪。

#### SPEEDTEST_PORT

监听端口。如果指定的端口无效（非数字），将回退到默认值 `8080`。

```bash
# 使用 80 端口（需要 root 权限）
sudo SPEEDTEST_PORT=80 ./speedtest

# 使用高端口
SPEEDTEST_PORT=3000 ./speedtest
```

#### SPEEDTEST_MODE

测速模式，决定如何测量下载和上传速度。

**time 模式**（默认）：
- 在固定时间内持续传输数据
- 适合测量稳定的带宽
- 结果更准确，但耗时固定

**size 模式**：
- 传输固定大小的数据
- 适合快速测试
- 在慢速网络下可能需要较长时间

```bash
# 时间模式，持续 30 秒
SPEEDTEST_MODE=time SPEEDTEST_DURATION=30 ./speedtest

# 数据量模式，下载 100MB，上传 50MB
SPEEDTEST_MODE=size SPEEDTEST_DOWNLOAD_SIZE=100 SPEEDTEST_UPLOAD_SIZE=50 ./speedtest
```

#### SPEEDTEST_DURATION

时间模式的持续时间（秒）。仅当 `SPEEDTEST_MODE=time` 时生效。

```bash
# 快速测试（5 秒）
SPEEDTEST_DURATION=5 ./speedtest

# 详细测试（60 秒）
SPEEDTEST_DURATION=60 ./speedtest
```

#### SPEEDTEST_DOWNLOAD_SIZE

数据量模式下的下载数据量（MB）。仅当 `SPEEDTEST_MODE=size` 时生效。

```bash
# 下载 100 MB
SPEEDTEST_DOWNLOAD_SIZE=100 ./speedtest
```

#### SPEEDTEST_UPLOAD_SIZE

数据量模式下的上传数据量（MB）。仅当 `SPEEDTEST_MODE=size` 时生效。

```bash
# 上传 50 MB
SPEEDTEST_UPLOAD_SIZE=50 ./speedtest
```

#### SPEEDTEST_STREAMS

并行连接数。更多的连接可以更好地饱和高带宽链路，但会消耗更多服务器资源。

```bash
# 单连接（适合低带宽环境）
SPEEDTEST_STREAMS=1 ./speedtest

# 8 连接（适合千兆以上网络）
SPEEDTEST_STREAMS=8 ./speedtest
```

#### SPEEDTEST_RATE_PER_MIN

每 IP 每分钟允许的请求数上限。默认 `0` = 关闭限流，适合单机内网 / 内部部署
场景；将其设置为正整数会启用一个令牌桶算法的限流器，仅对以下端点生效：

- `/api/download`
- `/api/upload`
- `/api/ping`

`/metrics`、`/healthz`、`/api/config`、`/api/ip`、`/api/results*` 等状态 / 元数据
端点不计数，这样监控系统的高频抓取不会被限流，DoS 流量也不会被这些计数掩盖。

每个客户端 IP 维护独立的令牌桶（突发容量 = 每分钟上限，匀速恢复）。客户端
IP 通过 `handler.ClientIP` 提取，会沿用既有的可信反向代理白名单（loopback /
RFC-1918 / RFC-4193）来识别 `X-Forwarded-For` / `X-Real-Ip`。

超过上限的请求会立刻收到 `429 Too Many Requests` + `Retry-After: <秒>` + JSON
错误 `{"error":"rate limit exceeded"}`。

```bash
# 公网部署：每个 IP 每分钟最多 60 次速度测试相关请求
SPEEDTEST_RATE_PER_MIN=60 ./speedtest

# 共享 NAT 后高校园区：放松到每分钟 600
SPEEDTEST_RATE_PER_MIN=600 ./speedtest

# 默认 / 单机内网：完全关闭限流
SPEEDTEST_RATE_PER_MIN=0 ./speedtest
```

后台会启动一个轻量级 GC 协程，每 60 秒清理一次 15 分钟未活动的 IP
桶，避免移动客户端 / CGNAT 切换地址导致内存无界增长。

#### SPEEDTEST_MAX_CONCURRENT

全局并发测速上限（download + upload 请求共享一个 semaphore）。超出会立刻返回
`503 Service Unavailable`,不排队。前端 `/api/config` 也会读到这个值,并把并发
流下拉框超上限的选项禁用,防止用户选了必然 503 的组合。

单机内网默认 `10` 完全够用。公网部署可以按机器 CPU / 带宽上调。

```bash
# 双核弱鸡:压到 4
SPEEDTEST_MAX_CONCURRENT=4 ./speedtest

# 千兆物理机:放到 32
SPEEDTEST_MAX_CONCURRENT=32 ./speedtest
```

#### SPEEDTEST_DB_PATH / SPEEDTEST_HISTORY_RETENTION_DAYS

`SPEEDTEST_DB_PATH` 指定 SQLite 历史文件位置。默认 `./speedtest.db` (跟二进制
同目录)。**显式设置为空串会禁用整个 History 特性** —— 服务只做即时测速,不写盘、
不暴露 `/api/results`,`/api/config.historyEnabled` 也报 false。

`SPEEDTEST_HISTORY_RETENTION_DAYS` 控制历史保留期,启动时会 prune 一次超期数据。
`0` 表示永久保留。

```bash
# 完全无状态
SPEEDTEST_DB_PATH="" ./speedtest

# 只留一个月
SPEEDTEST_DB_PATH=/var/lib/speedtest.db SPEEDTEST_HISTORY_RETENTION_DAYS=30 ./speedtest
```

#### SPEEDTEST_GEOIP_DB / SPEEDTEST_GEOIP_LICENSE_KEY / SPEEDTEST_GEOIP_EDITION

三个 opt-in 参数,任意组合都可以关闭 History 里 IP 归属地那一列(不设 = 关闭 =
零第三方依赖)。启用有两种路径:

**方式 A —— 只设 `_DB` 指向本地已有 mmdb**:适合已经跑了 MaxMind 官方
`geoipupdate` 工具、或者用 ansible 分发的场景。文件从哪里来我们不管,启动时
`Open()` 一次,失败就 warn + 关闭特性,不影响主服务。

```bash
SPEEDTEST_GEOIP_DB=/etc/speedtest/GeoLite2-City.mmdb ./speedtest
```

**方式 B —— 加上 `_LICENSE_KEY` 让二进制自己下**:后台 goroutine 从
`download.maxmind.com` 拉 tar.gz、抽 mmdb、原子替换、热插新 reader。默认每 7 天
带 `If-Modified-Since` 复查一次,304 就跳过。首次下载**异步**进行,不阻塞启动
—— 前 5-10 秒 `/api/config.geoipEnabled` 是 false,下完变 true。

```bash
SPEEDTEST_GEOIP_LICENSE_KEY=xxxx ./speedtest
# 文件默认落在 ./GeoLite2-City.mmdb
```

**两个都设**也可以 —— key 管更新,`_DB` 管文件路径,便于把 mmdb 放到 `/var/lib`
之类的持久化目录。

**`_EDITION`** 默认 `GeoLite2-City`。想要更小/仅国家级的话用
`SPEEDTEST_GEOIP_EDITION=GeoLite2-Country`。其它 MaxMind edition 也可以透传,只要
名字对得上 URL 就行。

所有失败路径(key 错、404、tar 损坏、验证不过)都是 fail-open —— warn log 一条,
现有 mmdb 继续用,主服务不受影响。

## 配置示例

### 开发环境

```bash
# 快速测试，本地访问
SPEEDTEST_HOST=127.0.0.1 \
SPEEDTEST_PORT=8080 \
SPEEDTEST_MODE=size \
SPEEDTEST_DOWNLOAD_SIZE=10 \
SPEEDTEST_UPLOAD_SIZE=5 \
./speedtest
```

### 生产环境（高带宽）

```bash
# 长时间测试，多连接
SPEEDTEST_HOST=0.0.0.0 \
SPEEDTEST_PORT=8080 \
SPEEDTEST_MODE=time \
SPEEDTEST_DURATION=30 \
SPEEDTEST_STREAMS=8 \
./speedtest
```

### 生产环境（快速测试）

```bash
# 固定数据量，适合大量用户同时测试
SPEEDTEST_HOST=0.0.0.0 \
SPEEDTEST_PORT=8080 \
SPEEDTEST_MODE=size \
SPEEDTEST_DOWNLOAD_SIZE=25 \
SPEEDTEST_UPLOAD_SIZE=10 \
SPEEDTEST_STREAMS=4 \
./speedtest
```

## 配置验证

启动时，服务器会打印当前配置：

```
mode=time  download=25MB  upload=10MB  duration=15s  listen=0.0.0.0:8080
```

可以通过 `/api/config` 端点查看前端获取到的配置：

```bash
curl http://localhost:8080/api/config | jq
```

## 无效值处理

如果环境变量设置为无效值，系统将使用默认值：

- 空值 → 使用默认值
- 无效数字（如 `abc`）→ 使用默认值
- 超出范围的值 → 限制在最小/最大值内
- 无效的模式值 → 使用 `time`
