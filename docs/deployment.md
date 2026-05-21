# 部署指南

## 系统要求

- **操作系统**: Linux, macOS, Windows
- **架构**: amd64, arm64
- **内存**: 最低 16 MB，推荐 64 MB 以上
- **磁盘**: 约 6 MB（单二进制文件）
- **网络**: 需要开放的入站端口

## 部署方式

### 1. 直接运行二进制

最简单的方式，适用于快速测试或开发环境。

```bash
# 下载对应平台的二进制
wget https://github.com/chancelyg/speedtest-go/releases/latest/download/speedtest_linux_amd64.tar.gz
tar -xzf speedtest_linux_amd64.tar.gz

# 运行
./speedtest

# 后台运行
nohup ./speedtest > /var/log/speedtest.log 2>&1 &
```

### 2. Systemd 服务（推荐用于 Linux 生产环境）

创建服务文件 `/etc/systemd/system/speedtest.service`：

```ini
[Unit]
Description=Speedtest Server
After=network.target

[Service]
Type=simple
User=speedtest
Group=speedtest
WorkingDirectory=/opt/speedtest
ExecStart=/opt/speedtest/speedtest
Restart=always
RestartSec=5

# 环境变量
# 默认 SPEEDTEST_HOST=:: 监听 v4 + v6 双栈，无需在 systemd 中显式设置；
# 仅当内核禁用 IPv6 双栈或部署策略要求仅 v4 时才显式覆盖为 0.0.0.0。
Environment="SPEEDTEST_PORT=8080"
Environment="SPEEDTEST_MODE=time"
Environment="SPEEDTEST_DURATION=15"
Environment="SPEEDTEST_STREAMS=4"
# 公网部署可启用每 IP 限流（默认 0 = 关闭）
# Environment="SPEEDTEST_RATE_PER_MIN=60"

# 资源限制
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

创建用户和目录：

```bash
# 创建用户
sudo useradd -r -s /bin/false speedtest

# 创建目录
sudo mkdir -p /opt/speedtest
sudo cp speedtest /opt/speedtest/
sudo chown -R speedtest:speedtest /opt/speedtest

# 启动服务
sudo systemctl daemon-reload
sudo systemctl enable --now speedtest

# 查看状态
sudo systemctl status speedtest
sudo journalctl -u speedtest -f
```

### 3. Docker 部署

#### 使用 Dockerfile

```dockerfile
FROM gcr.io/distroless/static-debian12

COPY speedtest /speedtest

EXPOSE 8080

ENTRYPOINT ["/speedtest"]
```

构建和运行：

```bash
# 构建镜像
docker build -t speedtest-go .

# 运行容器
docker run -d \
  --name speedtest \
  -p 8080:8080 \
  -e SPEEDTEST_PORT=8080 \
  -e SPEEDTEST_MODE=time \
  -e SPEEDTEST_DURATION=15 \
  speedtest-go
```

#### 使用 docker-compose

创建 `docker-compose.yml`：

```yaml
version: '3.8'

services:
  speedtest:
    image: speedtest-go:latest
    container_name: speedtest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - SPEEDTEST_HOST=0.0.0.0
      - SPEEDTEST_PORT=8080
      - SPEEDTEST_MODE=time
      - SPEEDTEST_DURATION=15
      - SPEEDTEST_STREAMS=4
    deploy:
      resources:
        limits:
          memory: 128M
        reservations:
          memory: 32M
```

运行：

```bash
docker-compose up -d
```

### 4. Nginx 反向代理

如果需要通过域名访问或添加 HTTPS，可以使用 Nginx 作为反向代理。

```nginx
server {
    listen 80;
    server_name speedtest.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # 上传测速需要较大的请求体
        client_max_body_size 500M;
        
        # WebSocket 支持（如果需要）
        proxy_read_timeout 86400;
    }
}
```

启用配置：

```bash
sudo ln -s /etc/nginx/sites-available/speedtest /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### 5. HTTPS（Let's Encrypt）

使用 Certbot 获取免费 SSL 证书：

```bash
# 安装 Certbot
sudo apt install certbot python3-certbot-nginx

# 获取证书
sudo certbot --nginx -d speedtest.example.com

# 自动续期已配置，无需手动操作
```

### 6. Caddy（auto-TLS）

[Caddy](https://caddyserver.com/) 是一个零配置即可自动从 Let's Encrypt
申请并续期证书的反向代理，适合不想手动维护 nginx + certbot 的场景。

最简 `Caddyfile`（自动启用 HTTPS）：

```caddyfile
speedtest.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

只要域名 A/AAAA 记录指向运行 Caddy 的主机、80/443 端口可达，Caddy 会
自动完成 ACME 挑战、获取证书并在到期前续期。无需额外脚本。

#### 屏蔽 `/healthz` 与 `/metrics`

`/healthz` 与 Prometheus `/metrics` 不应该暴露在公网上 —— 它们包含运行
时统计信息，并且 `/metrics` 是高频抓取目标，容易被滥用为带宽放大入口。
下面的 Caddyfile 将这两个路径限制到内网 CIDR：

```caddyfile
speedtest.example.com {
    # 内部路径仅允许 RFC1918 网段访问；其余路径走正常反代。
    @internal path /healthz /metrics
    handle @internal {
        @allowed remote_ip 10.0.0.0/8 192.168.0.0/16 172.16.0.0/12
        handle @allowed {
            reverse_proxy 127.0.0.1:8080
        }
        respond 403
    }

    reverse_proxy 127.0.0.1:8080

    # 上传测速默认 body 限制是 10 GB（由 binary 自身的
    # MaxBytesReader 控制），Caddy 不需要再设额外上限。
}
```

#### systemd 单元（运行 Caddy + speedtest）

如果两者都跑在同一台机器，建议各自一个 systemd 单元，便于独立重启。
speedtest 的单元已经在前面 "2. Systemd 服务" 给出；Caddy 通常由其官方
deb / rpm 包提供 `caddy.service`，安装后只需：

```bash
sudo systemctl enable --now caddy
sudo systemctl reload caddy   # 修改 Caddyfile 后热重载，不会中断现有连接
```

#### 验证

```bash
# TLS 握手成功且证书来自 Let's Encrypt
curl -vI https://speedtest.example.com

# 公网访问 /metrics 应该返回 403
curl -sI https://speedtest.example.com/metrics

# 内网（10.0.0.0/8 等）访问 /metrics 应该返回 200
curl -sI http://<内网 IP>:8080/metrics
```

## 防火墙配置

### UFW（Ubuntu/Debian）

```bash
sudo ufw allow 8080/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
```

### firewalld（CentOS/RHEL）

```bash
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
```

## 监控和日志

### 查看日志

```bash
# Systemd
sudo journalctl -u speedtest -f

# Docker
docker logs -f speedtest
```

### 健康检查

```bash
# 检查服务是否运行
curl -f http://localhost:8080/api/ping || echo "Service down"

# 检查配置
curl http://localhost:8080/api/config | jq
```

## 性能优化

### 系统调优

对于高并发或大带宽测试，可能需要调整系统参数：

```bash
# 编辑 /etc/sysctl.conf
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.ipv4.tcp_rmem = 4096 87380 134217728
net.ipv4.tcp_wmem = 4096 65536 134217728
net.core.netdev_max_backlog = 30000
net.ipv4.tcp_congestion_control = bbr

# 应用配置
sudo sysctl -p
```

### 文件描述符限制

```bash
# 编辑 /etc/security/limits.conf
speedtest soft nofile 65535
speedtest hard nofile 65535
```

## 备份和恢复

Speedtest-Go 是无状态服务，无需备份数据。只需保存二进制文件和启动脚本即可。

## 升级

```bash
# 1. 停止服务
sudo systemctl stop speedtest

# 2. 备份旧版本
sudo mv /opt/speedtest/speedtest /opt/speedtest/speedtest.bak

# 3. 部署新版本
sudo cp new-speedtest /opt/speedtest/speedtest
sudo chown speedtest:speedtest /opt/speedtest/speedtest

# 4. 启动服务
sudo systemctl start speedtest

# 5. 验证
sudo systemctl status speedtest
curl http://localhost:8080/api/ping
```
