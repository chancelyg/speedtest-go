# Speedtest-Go

一个单点部署的 Golang 网络测速 Web 站点，单个二进制文件即可运行，不依赖外部 API 或 CDN。前端设计简洁优雅，支持中英文切换和自动暗色/亮色主题。

## 功能特性

- **网络测速**：测量下载速度、上传速度、延迟、抖动和丢包率
- **双模式支持**：支持按数据量（size）或按时间（time）两种测速模式
- **多语言**：支持中文和英文界面切换
- **自适应主题**：自动跟随系统偏好，支持手动切换暗色/亮色主题
- **单二进制部署**：前端资源通过 Go embed 内嵌，零外部依赖
- **跨平台**：通过 GoReleaser 构建 Linux/macOS/Windows 多平台二进制

## 快速开始

### 使用预编译二进制

```bash
# 下载对应平台的二进制文件
./speedtest

# 访问 http://localhost:8080
```

### 从源码运行

```bash
# 克隆仓库
git clone <repository-url>
cd speedtest-go

# 开发运行
go run main.go

# 构建当前平台
go build -o speedtest .

# 使用 GoReleaser 构建多平台二进制
goreleaser build --snapshot --clean
```

## 环境变量配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `SPEEDTEST_HOST` | `0.0.0.0` | 绑定地址 |
| `SPEEDTEST_PORT` | `8080` | 监听端口 |
| `SPEEDTEST_MODE` | `time` | 测速模式：`size` 或 `time` |
| `SPEEDTEST_DURATION` | `15` | 时间模式持续时间（秒） |
| `SPEEDTEST_DOWNLOAD_SIZE` | `25` | 下载测速大小（MB，size 模式） |
| `SPEEDTEST_UPLOAD_SIZE` | `10` | 上传测速大小（MB，size 模式） |
| `SPEEDTEST_STREAMS` | `4` | 并行流数量 |

示例：

```bash
SPEEDTEST_PORT=3000 SPEEDTEST_MODE=size go run main.go
```

## API 端点

| 端点 | 方法 | 功能 |
|------|------|------|
| `/` | GET | 前端测速页面 |
| `/api/config` | GET | 获取服务器测速配置 |
| `/api/ip` | GET | 获取客户端 IP 地址 |
| `/api/ping` | GET | 延迟探测 |
| `/api/download` | GET | 下载测速数据流 |
| `/api/upload` | POST | 上传测速数据接收 |

## 项目结构

```
speedtest-go/
├── main.go                    # 程序入口
├── go.mod                     # Go 模块定义
├── .goreleaser.yaml           # GoReleaser 构建配置
├── static/                    # 前端静态资源（内嵌到二进制）
│   ├── index.html             # 主页面
│   ├── style.css              # 样式（含暗色/亮色主题）
│   └── app.js                 # 前端逻辑（测速、语言切换、主题）
├── internal/
│   ├── config/                # 配置管理
│   │   ├── config.go
│   │   └── config_test.go
│   └── handler/               # HTTP 处理器
│       ├── handler.go
│       └── handler_test.go
└── docs/                      # 文档
    ├── usage.md
    ├── api.md
    ├── configuration.md
    ├── deployment.md
    └── architecture.md
```

## 技术栈

- **后端**: Go 1.25+
- **前端**: 纯 HTML/CSS/JS（无框架，通过 `embed` 内嵌）
- **构建工具**: GoReleaser
- **部署方式**: 单二进制文件，零依赖

## 测试

```bash
# 运行单元测试
go test ./...

# 运行并查看覆盖率
go test -cover ./...
```

## 部署

### Docker 部署

```dockerfile
FROM gcr.io/distroless/static-debian12
COPY speedtest /speedtest
EXPOSE 8080
ENTRYPOINT ["/speedtest"]
```

### Systemd 服务

创建 `/etc/systemd/system/speedtest.service`：

```ini
[Unit]
Description=Speedtest Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/speedtest
Restart=always
Environment="SPEEDTEST_PORT=8080"

[Install]
WantedBy=multi-user.target
```

然后执行：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now speedtest
```

## 文档

- [使用说明](docs/usage.md)
- [API 文档](docs/api.md)
- [配置说明](docs/configuration.md)
- [部署指南](docs/deployment.md)
- [架构说明](docs/architecture.md)

## License

MIT
