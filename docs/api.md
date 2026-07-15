# API 文档

## 概述

Speedtest-Go 暴露一组 REST 端点用于:测速本身(`/api/download`、`/api/upload`、
`/api/ping`)、前端启动引导(`/api/config`、`/api/ip`)、历史 & 导出
(`/api/results*`)、以及运维观测(`/healthz`、`/metrics`)。除 `/api/ping`
返回 `text/plain`、`/api/download` 返回 `application/octet-stream`、
`/metrics` 返回 Prometheus exposition 格式外,其它端点响应均为 JSON。

## 端点列表

### GET /

返回前端测速页面(HTML)。资源由 `//go:embed` 打进二进制,零外部依赖。

- Content-Type: `text/html`
- 状态码: `200 OK`

---

### GET /api/config

前端启动时拉一次,用于:调整测速策略、判断 UI 组件(History / 归属地列)是否
渲染、footer 显示版本号、streams 下拉框卡上限。

**响应示例**:

```json
{
  "mode": "time",
  "durationSecs": 15,
  "downloadMB": 25,
  "uploadMB": 10,
  "streams": 4,
  "warmupMs": 500,
  "historyEnabled": true,
  "maxConcurrent": 10,
  "geoipEnabled": false,
  "version": "0.5.0",
  "commit": "abcdef1",
  "date": "2026-07-15T01:00:00Z"
}
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| `mode` | string | 测速模式:`"size"` 或 `"time"` |
| `durationSecs` | int | 时间模式持续时间(秒) |
| `downloadMB` | int | 下载数据量(MB,size 模式) |
| `uploadMB` | int | 上传数据量(MB,size 模式) |
| `streams` | int | 服务端建议的并行连接数 |
| `warmupMs` | int | 吞吐样本前 N ms 丢弃(慢启动 trim) |
| `historyEnabled` | bool | 服务端是否开了 SQLite 持久化;false 时前端隐藏 History |
| `maxConcurrent` | int | 全局并发上限;前端把 streams 下拉框超过此值的选项禁用 |
| `geoipEnabled` | bool | 服务端是否加载了 mmdb 归属地库;false 时前端隐藏 Location 列 |
| `version` | string | 构建版本号(ldflag 注入);未注入时报 `"dev"` |
| `commit` | string | 构建 commit hash(短)或空串 |
| `date` | string | 构建时间戳或空串 |

---

### GET /api/ip

获取客户端 IP 地址。

**响应示例**:

```json
{"ip": "203.0.113.5"}
```

**注意**:反向代理场景下,只有当直接连接的 peer 属于 loopback / RFC-1918 /
RFC-4193 私有范围时,才会信任 `X-Forwarded-For` / `X-Real-Ip` 头。公网直连的
客户端伪造这两个头无效。

---

### GET /api/ping

延迟探测端点。最小响应,`Cache-Control: no-store`。

- Content-Type: `text/plain`
- Body: `ok`

---

### GET /api/download

下载测速数据流。服务器发送不可压缩的随机字节;客户端根据接收速率算带宽。

- Content-Type: `application/octet-stream`
- `Cache-Control: no-store`
- 受**并发信号量**约束,超过 `maxConcurrent` 立即返回 `503`

**查询参数**:

| 参数 | 说明 |
|------|------|
| `duration` | 强制走 time 模式,持续 N 秒(hard cap 300) |
| `bytes` | 强制走 size 模式,传输 N 字节(hard cap 1 GB) |
| `size` | `bytes` 的别名 |
| `_` | 缓存破坏用随机数,服务端忽略 |

`duration` / `bytes` 二选一;都不带就沿用启动配置的模式。

**行为**:
- time 模式:分块传输,无 `Content-Length`,持续到 deadline
- size 模式:设 `Content-Length`,写完关闭

**状态码**:`200` / `503 Service Unavailable`(并发满) / `429 Too Many Requests`(限流)

---

### POST /api/upload

上传测速数据接收端点。服务端读并丢弃 body,返回接收字节数 + 服务端观察到的
耗时(便于客户端 cross-check)。

- 请求体上限:**10 GB**(超出返回 `413`)
- 受并发信号量约束
- 受 `Cache-Control: no-store`

**响应示例**:

```json
{
  "received": 10485760,
  "serverElapsedMs": 812,
  "truncated": false
}
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| `received` | int64 | 服务端读到的字节数 |
| `serverElapsedMs` | int64 | 服务端观察到的传输耗时 |
| `truncated` | bool | 是否触碰到 10GB 上限 |

**状态码**:`200` / `405`(非 POST) / `413`(超上限) / `503` / `429`

---

### GET /api/results

分页拉历史测速记录(需要 `historyEnabled=true`)。

**查询参数**:

| 参数 | 默认 | 上限 | 说明 |
|------|-----|------|------|
| `limit` | 20 | 100 | 单页条数 |
| `offset` | 0 | 1_000_000 | 分页偏移 |

**响应示例**:

```json
{
  "results": [
    {
      "id": 42,
      "created_at": 1789000000000,
      "download_mbps": 823.5,
      "upload_mbps": 92.1,
      "latency_idle_ms": 5.2,
      "latency_loaded_ms": 15.8,
      "download_jitter_ms": 0.9,
      "upload_jitter_ms": 1.1,
      "packet_loss": 0.0,
      "bufferbloat_grade": "A",
      "client_ip": "203.0.113.5",
      "client_ip_location": "上海市, 中国",
      "user_agent": "Mozilla/5.0 ...",
      "settings_json": "{\"mode\":\"time\",\"duration\":15,\"streams\":4}"
    }
  ],
  "total": 1234,
  "limit": 20,
  "offset": 0
}
```

`client_ip_location` 只在 `geoipEnabled=true` 时才有内容;老记录或私有 IP 均为
空串。

**状态码**:`200` / `503 history disabled`

---

### POST /api/results

保存一条测速结果。前端在测完之后自动 POST(需 `historyEnabled=true`)。

**请求体**:同上表 `results[i]` 结构,但服务端会**强制覆盖**以下字段以防伪造:
- `id`、`created_at`(server-assigned)
- `client_ip`(取自 peer,受可信代理白名单约束)
- `client_ip_location`(geoip 启用时由服务端本地 mmdb 查出;禁用时强制空串)
- `user_agent`(取自 `User-Agent` header,截断至 512 字节)

其它数值字段服务端会 clamp 到安全范围(NaN/Inf → 0,mbps ≤ 1e6,latency ≤
60000ms,packet loss ≤ 100)。

**响应**:

```json
{"id": 43, "created_at": 1789000123456}
```

**状态码**:`201` / `400`(payload 无效) / `503 history disabled`

---

### GET /api/results/export

一次性导出全部历史(cap 100_000 行)。

**查询参数**:

| 参数 | 默认 | 说明 |
|------|-----|------|
| `format` | `json` | `json` 或 `csv` |

`Content-Disposition: attachment; filename="speedtest-results-YYYYMMDD-HHMMSS.<ext>"`

CSV 中所有字符串字段先经过 formula-injection 过滤(`=`/`+`/`-`/`@`/`\t`/`\r`
开头的值前面加 `'`),防止 Excel 拿到时被误当公式执行。

---

### GET /healthz

运维健康检查。也附带最小的构建元数据便于告警排查跑的到底是哪个版本。

**响应示例**:

```json
{
  "status": "ok",
  "uptime_sec": 3600,
  "active_tests": 2,
  "accepted_total": 1024,
  "rejected_total": 8,
  "history_enabled": true,
  "version": "0.5.0",
  "commit": "abcdef1",
  "date": "2026-07-15T01:00:00Z"
}
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 恒为 `"ok"`(服务器能响应就意味着 ok) |
| `uptime_sec` | int64 | 进程运行秒数 |
| `active_tests` | int | 当前占用并发 slot 数 |
| `accepted_total` | int64 | 累计接受的测速请求数 |
| `rejected_total` | int64 | 累计因并发满被 503 拒绝数 |
| `history_enabled` | bool | 同 `/api/config` |
| `version` / `commit` / `date` | string | 同 `/api/config` |

`Cache-Control: no-store`。仅接受 `GET`;其它方法 405。

---

### GET /metrics

Prometheus exposition。包含标准 Go runtime metrics + 服务端自定义:

- `speedtest_tests_admitted_total` / `_rejected_total`(counter,并发信号量的双面)
- `speedtest_active_tests`(gauge,`active_tests` 的 Prom 版本)
- `speedtest_upload_bytes_total` / `_download_bytes_total`(counter)
- `speedtest_request_duration_seconds`(histogram,按 endpoint 分标签)

具体指标以实际抓取结果为准 —— schema 随 metrics_handler.go 演进,不保证向后
兼容。

---

### GET /sw.js

Service Worker 脚本。**由 Go handler 动态渲染** —— 每次请求把
`__SPEEDTEST_CACHE_NAME__` 占位符替换成 `speedtest-<version>`,让每次
release 自动 invalidate PWA cache,不需要手动 bump 常量。

`Cache-Control: no-cache`。

---

## 测速流程示例

```bash
# 1. 前端引导
curl http://localhost:8080/api/config
curl http://localhost:8080/api/ip

# 2. 延迟基线(重复 N 次)
for i in $(seq 1 20); do time curl -sf http://localhost:8080/api/ping; done

# 3. 下载 5 秒(time 模式)
curl -o /dev/null -w '%{speed_download}\n' \
  'http://localhost:8080/api/download?duration=5'

# 4. 下载固定 50MB(size 模式,4 条流并发)
for i in 1 2 3 4; do
  curl -o /dev/null 'http://localhost:8080/api/download?bytes=13107200' &
done; wait

# 5. 上传 50MB
head -c 50m /dev/urandom | curl -X POST --data-binary @- \
  http://localhost:8080/api/upload

# 6. 拉最近一页历史
curl -sf http://localhost:8080/api/results?limit=5 | jq

# 7. 导出全部历史为 CSV
curl -sfLO 'http://localhost:8080/api/results/export?format=csv'
```

## 全部状态码

| 状态码 | 触发 |
|--------|------|
| 200 | 成功 |
| 201 | POST /api/results 创建成功 |
| 400 | POST body 无效 / query param 越界 |
| 405 | 端点不接受该 HTTP 方法 |
| 413 | POST /api/upload 超过 10 GB 上限 |
| 429 | 触发 per-IP rate limit(仅测速端点开启限流时) |
| 500 | 服务端内部错误(细节仅记录在日志) |
| 503 | 服务不可用:并发满 / history 禁用时访问 `/api/results*` |
