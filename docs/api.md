# API 文档

## 概述

Speedtest-Go 提供简单的 REST API 用于网络测速。所有 API 端点返回 JSON 格式数据（除 `/api/ping` 返回纯文本）。

## 端点列表

### GET /

返回前端测速页面（HTML）。

**响应**：
- Content-Type: `text/html`
- 状态码: `200 OK`

---

### GET /api/config

获取服务器测速配置，前端据此调整测速策略。

**响应示例**：

```json
{
  "mode": "time",
  "durationSecs": 15,
  "downloadMB": 25,
  "uploadMB": 10,
  "streams": 4
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `mode` | string | 测速模式：`"size"` 或 `"time"` |
| `durationSecs` | int | 时间模式持续时间（秒） |
| `downloadMB` | int | 下载数据量（MB，size 模式） |
| `uploadMB` | int | 上传数据量（MB，size 模式） |
| `streams` | int | 并行连接数 |

---

### GET /api/ip

获取客户端 IP 地址。

**响应示例**：

```json
{
  "ip": "192.168.1.100"
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `ip` | string | 客户端 IP 地址 |

**注意**：如果服务器位于反向代理后面，会优先从 `X-Forwarded-For` 或 `X-Real-Ip` 头中提取真实 IP。

---

### GET /api/ping

延迟探测端点，返回最小响应以测量网络延迟。

**响应**：
- Content-Type: `text/plain`
- 状态码: `200 OK`
- 响应体: `ok`

**缓存控制**：
- `Cache-Control: no-store`（禁止缓存）

---

### GET /api/download

下载测速数据流。服务器持续发送随机字节，客户端通过测量接收速率计算下载速度。

**响应**：
- Content-Type: `application/octet-stream`
- 状态码: `200 OK`
- `Cache-Control: no-store`

**行为**：

- **时间模式**：无 `Content-Length`，使用分块传输编码，持续发送数据直到时间结束
- **数据量模式**：设置 `Content-Length` 头，发送固定大小的数据

**查询参数**：

| 参数 | 说明 |
|------|------|
| `_` | 时间戳或随机数，用于防止浏览器缓存 |

---

### POST /api/upload

上传测速数据接收端点。服务器读取并丢弃请求体，返回接收到的字节数。

**请求**：
- Method: `POST`
- Content-Type: 任意（通常为 `application/octet-stream` 或 `text/plain`）
- Body: 任意二进制数据

**响应示例**：

```json
{
  "received": 10485760
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `received` | int64 | 接收到的字节数 |

**错误响应**：

- `405 Method Not Allowed`：使用了非 POST 方法
- `500 Internal Server Error`：读取请求体时出错

## 测速流程示例

```bash
# 1. 获取配置
curl http://localhost:8080/api/config

# 2. 获取 IP
curl http://localhost:8080/api/ip

# 3. 延迟探测（重复多次）
curl http://localhost:8080/api/ping

# 4. 下载测速（使用 time 命令测量）
time curl -o /dev/null http://localhost:8080/api/download

# 5. 上传测速
curl -X POST -d @/dev/urandom http://localhost:8080/api/upload
```

## 状态码

| 状态码 | 说明 |
|--------|------|
| 200 | 请求成功 |
| 405 | 方法不允许（仅影响 `/api/upload`） |
| 500 | 服务器内部错误 |
