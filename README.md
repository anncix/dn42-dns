# dn42-dns

面向 [dn42](https://dn42.eu/) 网络的轻量级智能 DNS 分流服务器 + 管理面板。

专为 dn42 设计，专注于核心功能：域名分流、反向解析（rDNS）、缓存加速、Web 管理面板。代码精简，易于部署和维护。

## 功能特性

- **智能分流**：`.dn42`、`.fdn` 等 dn42 域名走内部 DNS，公网域名走公网上游
- **反向解析 (rDNS)**：支持 IPv4 + IPv6 反向解析，本地记录优先，网段白名单控制
- **DNS 缓存**：LRU 缓存 + TTL 钳制 + 否定缓存，缓解 dn42 高延迟
- **高可用上游**：健康状态跟踪 + 冷却跳过 + 延迟优先智能调度
- **主从模式**：支持 master/slave 主从部署，健康检查 + rDNS 记录同步
- **Web 管理面板**：FastAPI + 原生前端，仪表盘、缓存管理、rDNS、上游状态一目了然
- **无感登录**：JWT + Refresh Token，访问期间自动续期，无需重复登录
- **邮局 DNS**：MX/SPF/DKIM/DMARC 本地记录，支持 dn42 内自建邮件系统
- **轻量高效**：Go + Python 双栈，无多余依赖，适合路由器/嵌入式设备

## 快速开始

### 编译

```bash
git clone https://github.com/anncix/dn42-dns.git
cd dn42-dns
go build -o dn42-dns ./cmd/smartdns
```

### 运行

```bash
# 使用 dn42 配置（默认）
sudo ./dn42-dns -c configs/dn42.yaml

# 查看版本
./dn42-dns -v
```

> 绑定 53 端口需要 root 权限。

### 信号控制

| 信号 | 作用 |
|------|------|
| `SIGINT` / `SIGTERM` | 停止服务（退出时打印统计） |
| `SIGUSR1` | 清空 DNS 缓存 |

```bash
# 清空缓存
kill -USR1 $(pidof dn42-dns)
```

## 配置说明

配置文件为 YAML 格式，核心部分：

```yaml
# 监听地址
listen:
  - addr: ":53"
    protocol: udp
  - addr: ":53"
    protocol: tcp

# 上游 DNS
upstreams:
  default:  # 公网上游
    - addr: "223.5.5.5:53"
      name: "阿里DNS"
  groups:
    dn42:   # dn42 内部上游
      - addr: "172.20.0.53:53"
        name: "a.root-servers.dn42"

# 分流规则
routing:
  domain_suffix:
    dn42:
      - ".dn42"
      - ".fdn"
      - "20.172.in-addr.arpa"  # dn42 反向解析
      - "d.f.ip6.arpa"          # dn42 IPv6 反向解析
  default_group: default

# 缓存
cache:
  enabled: true
  max_size: 10000
  min_ttl: 10
  max_ttl: 86400
  neg_ttl: 300

# 反向 DNS
rdns:
  enabled: true
  allowed_networks:
    - "172.20.0.0/14"
    - "fd00::/8"
    - "127.0.0.0/8"
  local_records:
    "127.0.0.1": "localhost"
    "::1": "localhost"

# 日志
log:
  level: info
  query_log: true
```

## 项目结构

```
dn42-dns/
├── dns/                       # DNS 服务器（Go）
│   ├── cmd/smartdns/          # 程序入口
│   ├── internal/              # 核心模块
│   │   ├── config/            # 配置加载
│   │   ├── server/            # DNS 服务器核心
│   │   ├── router/            # 域名分流路由
│   │   ├── resolver/          # 上游 DNS 解析器
│   │   ├── cache/             # DNS 缓存 (LRU)
│   │   └── rdns/              # 反向 DNS 处理
│   └── go.mod
├── web/                       # Web 管理面板（Python/FastAPI）
│   ├── app/                   # 后端
│   │   ├── main.py            # 主应用
│   │   ├── auth.py            # JWT 认证
│   │   └── dns_client.py      # DNS API 客户端
│   ├── templates/             # 前端页面
│   ├── static/                # 静态资源
│   └── requirements.txt
└── configs/
    └── dn42.yaml              # dn42 开箱即用配置
```

## 核心模块说明

### 路由 (router)

基于域名后缀匹配，最长匹配优先。例如：
- `example.dn42` → dn42 组
- `sub.example.dn42` → dn42 组
- `google.com` → default 组

### 反向 DNS (rdns)

- 只响应 `allowed_networks` 网段内的 PTR 查询
- 优先返回 `local_records` 本地记录
- 本地无记录时转发到对应上游组
- 公网 IP 的 PTR 查询直接拒绝，避免泄露公网 DNS 请求

### 缓存 (cache)

- LRU 淘汰策略
- TTL 钳制（min_ttl / max_ttl）
- NXDOMAIN 否定缓存
- 自动更新剩余 TTL

## HTTP 管理 API

内置轻量 HTTP API，默认监听 `:8080`，可用于监控和管理。

### 健康检查

```bash
curl http://localhost:8080/api/health
# {"status":"ok"}
```

### 统计信息

```bash
curl http://localhost:8080/api/stats
# {
#   "mode": "standalone",
#   "cache": { ... },
#   "cache_size": 1234,
#   "rdns": { ... },
#   "upstreams": ["default", "dn42"]
# }
```

### 缓存管理

```bash
# 查看缓存统计
curl http://localhost:8080/api/cache/stats

# 清空缓存
curl -X POST http://localhost:8080/api/cache/flush
```

### rDNS 记录

```bash
# 查看本地 rDNS 记录
curl http://localhost:8080/api/rdns/records
```

### 主从状态

```bash
curl http://localhost:8080/api/ha/status
```

## 主从模式 (HA)

支持 master/slave 主从部署，提升 DNS 服务可用性。

### 架构

```
        +----------+
        |  Master  |  <-- 主节点（处理查询 + 同步数据）
        +----------+
           /    \
          /      \
   +----------+  +----------+
   |  Slave1  |  |  Slave2  |  <-- 从节点（独立查询 + 同步rDNS）
   +----------+  +----------+
```

### 主节点配置

```yaml
api:
  enabled: true
  addr: ":8080"

ha:
  mode: master
  slaves:
    - "10.0.0.2:8080"
    - "10.0.0.3:8080"
  sync_interval: 30
```

### 从节点配置

```yaml
api:
  enabled: true
  addr: ":8080"

ha:
  mode: slave
  master: "10.0.0.1:8080"
  sync_interval: 30
```

### 功能说明

- **健康检查**：主从节点互相检测健康状态，默认 30 秒一次
- **rDNS 同步**：从节点定期从主节点拉取本地 rDNS 记录
- **独立运行**：从节点本身也能独立处理 DNS 查询，主节点故障不影响从节点
- **API 监控**：通过 `/api/ha/status` 查看主从状态

## Web 管理面板

基于 FastAPI 的简易管理面板，支持仪表盘、缓存管理、rDNS 查看、上游监控、主从状态等。

### 启动

```bash
# 安装依赖
cd web
pip install -r requirements.txt

# 启动（默认监听 8000）
uvicorn app.main:app --host 0.0.0.0 --port 8000
```

### 配置

在 `web/app/config.py` 或环境变量中配置：

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `ADMIN_USERNAME` | 管理员用户名 | admin |
| `ADMIN_PASSWORD` | 管理员密码 | admin123 |
| `JWT_SECRET` | JWT 密钥（请修改） | 随机生成 |
| `DNS_API_URL` | DNS 服务器 API 地址 | http://127.0.0.1:8080 |
| `ACCESS_TOKEN_EXPIRE_MINUTES` | Access Token 有效期 | 15 分钟 |
| `REFRESH_TOKEN_EXPIRE_DAYS` | Refresh Token 有效期 | 7 天 |

### 无感登录

采用 JWT + Refresh Token 双 Token 机制实现无感登录：

1. 登录时返回 `access_token`（短有效期）和 `refresh_token`（长有效期）
2. 前端在 access_token 过期前自动刷新，用户无感知
3. 每次刷新都会轮换 refresh_token，提升安全性
4. 支持主动登出，吊销 refresh_token

### 功能页面

- **仪表盘**：概览 DNS 运行状态、缓存统计、rDNS 统计、HA 模式
- **缓存管理**：查看缓存统计、一键清空缓存
- **rDNS 记录**：查看本地 rDNS 记录，支持搜索过滤
- **上游服务器**：查看各组上游状态、健康度、延迟
- **主从状态**：查看 HA 模式和节点健康状态
- **邮局 DNS**：MX/A/SPF/DKIM/DMARC 记录管理
- **系统设置**：修改密码

## 邮局 DNS

支持本地邮件相关 DNS 记录，方便在 dn42 网络内搭建邮件系统。

### 支持的记录类型

| 类型 | 说明 | 示例 |
|------|------|------|
| **MX** | 邮件交换记录，指定接收邮件的服务器 | `example.dn42 → mail.example.dn42` |
| **A** | 邮件服务器 IP 地址 | `mail.example.dn42 → 172.22.100.1` |
| **SPF** | 发件人策略框架，防止邮件伪造 | `v=spf1 mx ~all` |
| **DKIM** | 域名密钥识别邮件，数字签名验证 | `v=DKIM1; k=rsa; p=...` |
| **DMARC** | 邮件认证策略与报告 | `v=DMARC1; p=none` |

### 配置示例

```yaml
mail:
  enabled: true
  ttl: 3600
  mx:
    - domain: "example.dn42"
      server: "mail.example.dn42"
      priority: 10
  a:
    "mail.example.dn42": "172.22.100.1"
  spf:
    "example.dn42": "v=spf1 mx ~all"
  dkim:
    "default._domainkey.example.dn42": "v=DKIM1; k=rsa; p=..."
  dmarc:
    "_dmarc.example.dn42": "v=DMARC1; p=none"
```

### 查询测试

```bash
# 查询 MX 记录
dig @127.0.0.1 example.dn42 MX

# 查询 SPF 记录
dig @127.0.0.1 example.dn42 TXT

# 查询 DKIM 记录
dig @127.0.0.1 default._domainkey.example.dn42 TXT

# 查询 DMARC 记录
dig @127.0.0.1 _dmarc.example.dn42 TXT
```

### API 管理

- `GET /api/mail/records` - 获取所有邮件记录
- `GET /api/mail/stats` - 获取邮局 DNS 统计
- `POST /api/mail/mx` - 添加/删除 MX 记录
- `POST /api/mail/a` - 添加/删除 A 记录
- `POST /api/mail/spf` - 添加 SPF 记录
- `POST /api/mail/dkim` - 添加 DKIM 记录
- `POST /api/mail/dmarc` - 添加 DMARC 记录

也可以通过 Web 管理面板的「邮局 DNS」页面进行可视化管理。

## 上游智能调度

针对 dn42 网络不稳定的特点，上游 DNS 采用智能调度策略：

- **健康状态跟踪**：上游服务器失败后自动标记为不健康，进入 30 秒冷却期
- **冷却跳过**：冷却期内跳过故障服务器，避免每次查询都等待超时
- **自动恢复**：冷却期过后自动尝试恢复，成功后重新标记为健康
- **延迟优先**：健康服务器按平均响应时间排序，优先使用最快的
- **滑动窗口**：记录最近 10 次查询延迟，计算平滑平均值
- **API 监控**：通过 `/api/upstreams?group=dn42` 查看所有上游状态

## 性能

- 单实例可轻松处理每秒数万查询
- 内存占用低（主要是缓存条目）
- 零额外运行时依赖
- rDNS 网段匹配结果缓存，重复查询零开销

## 测试

```bash
# 运行所有单元测试
go test ./internal/... -v

# 测试覆盖率
go test ./internal/... -cover
```

当前测试覆盖：
- rDNS 模块：9 个测试用例
- Cache 模块：9 个测试用例  
- Router 模块：3 个测试用例
- Server 集成测试：10 个测试用例

## 版本

- **v3.1.0**：新增邮局 DNS 功能
  - 支持 MX、A、SPF、DKIM、DMARC 本地邮件记录
  - 配置文件 mail 段，支持预设记录
  - API 接口支持运行时增删记录
  - Web 管理面板新增邮局 DNS 页面（5 个 Tab）

- **v3.0.0**：新增 Web 管理面板 + 无感登录 + 仓库重组
  - FastAPI 后端管理面板，6 大功能页面
  - JWT + Refresh Token 双 Token 无感登录
  - 仓库目录重组：DNS 服务和管理面板合并为一个仓库
  - 上游智能调度优化：健康跟踪 + 延迟优先

- **v2.2.0**：上游智能调度与性能优化
  - 上游服务器健康状态跟踪，失败后自动冷却跳过
  - 智能排序：健康优先 + 延迟最低优先
  - rDNS 网段匹配结果缓存，重复查询零开销
  - API 新增上游状态端点 `/api/upstreams`
  - 默认配置优化：缓存 TTL 调优，API 默认只监听本地
  - 更完整的 dn42 域名分流规则（含 IPv6 rDNS）

- **v2.1.0**：新增 HTTP 管理 API 和主从模式
  - 内置 HTTP API：健康检查、统计、缓存管理、rDNS 查询
  - 主从模式：master/slave 部署，健康检查，rDNS 记录同步
  - 简洁的 Web 状态页

- **v2.0.0**：精简重构，聚焦 dn42 核心功能
  - 移除广告拦截、DoH/DoT、热重载等非核心功能
  - rDNS 独立为模块，完善 IPv6 支持
  - 代码量减少约 40%，配置项减少约 50%

## License

MIT
