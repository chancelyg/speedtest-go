# Spec: Speedtest Web 站点

## Objective
构建一个单点部署的 Golang 网络测速 Web 站点，二进制文件即可运行，不依赖外部 API 或 CDN。前端设计简洁优雅，展示下载速度、上传速度、延迟、抖动、丢包率、IP 地址、连接方式（IPv4）等信息，并提供开始测速按钮。支持中英文切换和自动暗色/亮色主题。

## Tech Stack
- **后端**: Go 1.21+
- **前端**: 纯 HTML/CSS/JS（通过 `embed` 内嵌到二进制）
- **构建工具**: GoReleaser
- **部署方式**: 单二进制文件，零依赖

## Commands
```bash
# 开发运行
go run main.go

# 构建（当前平台）
go build -o speedtest .

# 使用 GoReleaser 构建多平台二进制
goreleaser build --snapshot --clean

# 发布（需要配置）
goreleaser release
```

## Project Structure
```
speedtest-go/
├── main.go              # 入口文件，HTTP 服务 + embed 静态资源
├── go.mod               # Go 模块定义
├── go.sum               # 依赖校验
├── .goreleaser.yaml     # GoReleaser 配置
├── static/
│   ├── index.html       # 主页面
│   ├── style.css        # 样式（含暗色/亮色主题）
│   └── app.js           # 前端逻辑（测速、语言切换、主题）
└── internal/
    └── speedtest/
        └── simulator.go # 模拟测速数据生成器
```

## Code Style
- Go: 标准格式化（`gofmt`），简洁明了
- 前端: 原生 JS，无框架，CSS 变量管理主题

## Testing Strategy
- 手动测试为主：构建后运行二进制，浏览器访问验证
- 验证项：
  - 页面加载正常
  - 开始测速按钮工作
  - 数据展示正确
  - 中英文切换正常
  - 主题切换正常

## Boundaries
- **Always**: 保持单二进制部署，不引入外部依赖
- **Ask first**: 添加新的 Go 依赖或前端库
- **Never**: 依赖外部 API/CDN，引入复杂前端构建流程

## Success Criteria
- [ ] 单二进制文件可运行（`./speedtest` 后浏览器访问 `localhost:8080`）
- [ ] 页面显示：下载速度、上传速度、延迟、抖动、丢包率、IP、IPv4 连接方式
- [ ] 点击开始按钮触发模拟测速动画并显示结果
- [ ] 支持中英文切换
- [ ] 支持自动暗色/亮色主题（跟随系统偏好）
- [ ] GoReleaser 可成功构建多平台二进制

## Open Questions
- 监听端口是否固定为 8080？（建议默认 8080，可通过环境变量覆盖）
- IP 地址显示真实 IP 还是模拟？（建议显示请求来源 IP）
