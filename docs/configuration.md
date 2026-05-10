# 配置说明

## 环境变量

Speedtest-Go 的所有配置均通过环境变量进行，无需配置文件。

### 完整配置列表

| 环境变量 | 默认值 | 有效范围 | 说明 |
|---------|--------|---------|------|
| `SPEEDTEST_HOST` | `0.0.0.0` | 任意有效 IP | HTTP 服务器绑定地址 |
| `SPEEDTEST_PORT` | `8080` | 1-65535 | HTTP 服务器监听端口 |
| `SPEEDTEST_MODE` | `time` | `size`, `time` | 测速模式 |
| `SPEEDTEST_DURATION` | `15` | >= 1 秒 | 时间模式持续时间 |
| `SPEEDTEST_DOWNLOAD_SIZE` | `25` | 1-10240 MB | 下载数据量（size 模式） |
| `SPEEDTEST_UPLOAD_SIZE` | `10` | 1-10240 MB | 上传数据量（size 模式） |
| `SPEEDTEST_STREAMS` | `4` | 1-32 | 并行连接数 |

### 配置详解

#### SPEEDTEST_HOST

绑定地址。使用 `0.0.0.0` 监听所有接口，`127.0.0.1` 仅监听本地回环。

```bash
# 仅本地访问
SPEEDTEST_HOST=127.0.0.1 ./speedtest

# 监听特定网卡
SPEEDTEST_HOST=192.168.1.10 ./speedtest
```

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
