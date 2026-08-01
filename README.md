# dn42-dns

面向 [dn42](https://dn42.eu/) 网络的智能 DNS 分流服务器

一个功能完整的 DNS 服务器，专为 dn42 网络设计，支持 dn42 域名解析、公网域名分流、广告拦截、rDNS 反向解析等功能。

## 功能特性

- **dn42 域名解析**：自动将 `.dn42` 及相关顶级域转发到 dn42 权威 DNS
- **智能分流**：dn42 内网域名走 dn42 网络，公网域名走公网上游
- **广告/追踪拦截**：基于 Radix 树的高性能域名匹配，支持多种规则格式
- **多协议上游**：支持 UDP/TCP/DoT/DoH 多种 DNS 协议
- **rDNS 反向解析**：支持 dn42 地址段的反向解析转发
- **本地 hosts 支持**：自定义本地域名解析记录
- **智能缓存**：LRU 缓存 + 惰性缓存 + 预取，提升解析速度
- **高可用**：多上游轮询 + 故障自动转移
- **详细统计**：查询量、拦截率、缓存命中率等实时统计
- **配置化管理**：YAML 配置文件，灵活定制

## dn42 支持

### 支持的顶级域

默认配置自动转发以下 dn42 相关顶级域到 dn42 权威 DNS：

| TLD | 说明 |
|-----|------|
| `.dn42` | dn42 主顶级域 |
| `.fdn` | French Data Network |
| `.NeoNetwork` | NeoNetwork |
| `.hack` | HackLAN |
| `.hub.dn42` | dn42 HUB |
| `.dn42.` (反向解析 | 172.20.0.0/14 反向解析 |
| 20.172.in-addr.arpa | dn42 IPv4 反向解析 |
| 21.172.in-addr.arpa | |
| 22.172.in-addr.arpa | |
| 23.172.in-addr.arpa | |
| ip6.arpa (fd00::/8 段 | dn42 IPv6 反向解析 |

### 推荐的 dn42 权威 DNS

以下是 dn42 网络中的公共递归 DNS 服务器：

| 服务器 | IPv4 | IPv6 | 位置 |
|-------|------|------|------|
| a.root-servers.dn42 | 172.20.0.53 | fd42:d42:d42:54::1 | 全球 Anycast |
| b.root-servers.dn42 | 172.20.1.53 | fd42:d42:d42:53::1 | 欧洲 |
| c.root-servers.dn42 | 172.20.2.53 | fd42:d42:d42:52::1 | 亚洲 |
| d.root-servers.dn42 | 172.20.3.53 | fd42:d42:d42:51::1 | 北美 |

> 建议使用 `a.root-servers.dn42` 的 anycast 地址作为主上游。

## 架构概览

```
客户端请求
    │
    ▼
┌─────────────────────────────────────────┐
│         DNS 服务器入口 (UDP/TCP)        │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│         本地 hosts / rDNS 解析              │  ← 本地记录
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│        广告/追踪过滤引擎                  │  ← Radix 树 + 白名单
└─────────────────────────────────────────┘
    │ 未拦截
    ▼
┌─────────────────────────────────────────┐
│        DNS 缓存 (LRU + 惰性缓存)       │
└─────────────────────────────────────────┘
    │ 未命中
    ▼
┌─────────────────────────────────────────┐
│        分流路由器                      │
│  ┌─────────┐      ┌──────────────┐    │
│  │ dn42域名 │      │   公网域名   │    │
│  └────┬────┘      └──────┬───────┘    │
└───────┼──────────────────┼────────────┘
        │                  │
        ▼                  ▼
┌───────────────┐  ┌──────────────────┐
│  dn42 上游   │  │   公网上游      │
│  (172.20.x) │  │   (DoH/DoT/UDP)│
└───────────────┘  └──────────────────┘
```

## 快速开始

### 编译

```bash
git clone https://github.com/anncix/dn42-dns.git
cd dn42-dns
go mod download
go build -o dn42-dns ./cmd/smartdns
```

### 运行

```bash
# 使用默认 dn42 配置（推荐）
sudo ./dn42-dns -c configs/dn42.yaml

# 指定其他配置
sudo ./dn42-dns -c /path/to/config.yaml

# 查看版本
./dn42-dns -v
```

> 注意：绑定 53 端口需要 root 权限。

### 配置客户端

在 dn42 节点上使用：

```bash
# 修改 /etc/resolv.conf
echo "nameserver 127.0.0.1" > /etc/resolv.conf

# 或在 BIRD 中配置
# 无需额外配置，确保 可直接使用
```

### 测试

```bash
# 测试 dn42 域名解析
dig @127.0.0.1 whoami.dn42

# 测试公网域名解析
dig @127.0.0.1 www.google.com

# 测试广告拦截
dig @127.0.0.1 doubleclick.net

# 测试 dn42 反向解析
dig @127.0.0.1 -x 172.20.0.53

# 测试本地 rDNS
dig @127.0.0.1 -x 172.22.xxx.xxx
```

## 配置说明

配置文件为 YAML 格式，以下是 dn42 场景的推荐配置。

### 监听配置

```yaml
listen:
  - addr: ":53"
    protocol: udp
  - addr: ":53"
    protocol: tcp
```

### 上游 DNS 配置

```yaml
upstreams:
  # 默认上游（公网）
  default:
    - addr: "223.5.5.5:53"
      name: "阿里 DNS"
    - addr: "119.29.29.29:53"
      name: "腾讯 DNS"

  # dn42 上游
  groups:
    dn42:
      - addr: "172.20.0.53:53"
        name: "a.root-servers.dn42"
      - addr: "172.20.1.53:53"
        name: "b.root-servers.dn42"
```

### 分流规则（dn42 专用）

```yaml
routing:
  domain_suffix:
    dn42:
      - ".dn42"
      - ".fdn"
      - ".NeoNetwork"
      - ".hack"
      - ".hub.dn42"
      # dn42 IPv4 反向解析
      - "20.172.in-addr.arpa"
      - "21.172.in-addr.arpa"
      - "22.172.in-addr.arpa"
      - "23.172.in-addr.arpa"
      - "16.172.in-addr.arpa"
      - "17.172.in-addr.arpa"
      - "18.172.in-addr.arpa"
      - "19.172.in-addr.arpa"
      # dn42 IPv6 反向解析 (fd00::/8)
      - "d.f.ip6.arpa"
  default_group: default
```

### 广告拦截配置

```yaml
adblock:
  enabled: true
  block_mode: nxdomain
  blacklist_files:
    - "rules/blacklist.txt"
    # 可选：订阅 StevenBlack 规则
    # - "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
  whitelist_files:
    - "rules/whitelist.txt"
```

### rDNS 配置（dn42 本地记录）

```yaml
rdns:
  enabled: true
  allowed_networks:
    - "172.20.0.0/14"   # dn42 IPv4
    - "10.0.0.0/8"      # 内网
    - "fd00::/8"         # dn42 IPv6 ULA
    - "127.0.0.0/8"     # 本地
  local_records:
    # 你的 dn42 节点信息
    "172.22.xxx.xxx": "your-node.dn42"
    "172.22.xxx.1": "your-gw.dn42"
```

### 缓存配置

```yaml
cache:
  enabled: true
  max_size: 50000
  min_ttl: 30
  max_ttl: 86400
  neg_ttl: 60
  lazy_cache: true
  lazy_cache_ttl: 7200
  prefetch: true
```

## 部署建议

### 作为 dn42 节点的本地 DNS

这是最常见的用法，dn42-dns 运行在你的 dn42 节点上，为节点本身和局域网提供 DNS 服务。

```
局域网客户端 ──► dn42-dns (你的节点) ──┬──► dn42 权威 DNS (dn42 网络)
                                        │
                                        └──► 公网上游 DNS (公网)
```

### 作为 dn42 网络的公共递归 DNS

如果你想为 dn42 网络提供公共递归 DNS 服务：

1. 确保你的节点网络稳定
2. 配置适当的访问控制（防止滥用）
3. 在 dn42 wiki 上登记你的 DNS 服务
4. 考虑加入 anycast

### Docker 部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o dn42-dns ./cmd/smartdns

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/dn42-dns /usr/local/bin/
COPY configs/dn42.yaml /etc/dn42-dns/config.yaml
COPY rules/ /etc/dn42-dns/rules/
EXPOSE 53/udp 53/tcp
CMD ["dn42-dns", "-c", "/etc/dn42-dns/config.yaml"]
```

```bash
docker build -t dn42-dns .
docker run -d --name dn42-dns -p 53:53/udp -p 53:53/tcp dn42-dns
```

### systemd 服务

创建 `/etc/systemd/system/dn42-dns.service`:

```ini
[Unit]
Description=dn42 DNS Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/dn42-dns -c /etc/dn42-dns/config.yaml
Restart=always
RestartSec=5
User=root
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now dn42-dns
sudo systemctl status dn42-dns
```

## 规则格式

支持多种规则格式：

### 1. 纯域名格式
```
example.com
ads.example.net
```

### 2. Hosts 格式
```
0.0.0.0 ads.example.com
127.0.0.1 tracker.example.net
```

### 3. AdGuard 格式
```
||ads.example.com^
||*.doubleclick.net^
```

## 性能特点

- **Radix 树匹配**：域名反转存储，后缀匹配转前缀匹配，单次匹配 <1ms
- **LRU 缓存**：高频查询直接命中缓存，大幅降低延迟
- **惰性缓存**：上游故障时可临时返回过期结果，提升可用性
- **并发安全**：读写锁保护，高并发场景稳定
- **轮询 + 故障转移**：多上游自动切换，保证可用性

## 与其他 dn42 DNS 方案对比

| 特性 | dn42-dns | dnsmasq | unbound | BIND | CoreDNS |
|------|----------|---------|---------|------|---------|
| dn42 分流 | ✅ 原生支持 | ⚠️ 需手动配置 | ⚠️ 需配置转发 | ✅ 原生 | ⚠️ 需插件 |
| 广告拦截 | ✅ Radix 树 | ❌ hosts 方式 | ❌ | ❌ | ❌ |
| 缓存 | ✅ LRU+惰性 | ✅ 基础 | ✅ 完整 | ✅ 完整 | ✅ 插件 |
| rDNS 本地记录 | ✅ | ✅ | ⚠️ 需配置区域 | ✅ | ⚠️ 需插件 |
| 资源占用 | 中 | 极低 | 中高 | 高 | 中 |
| 配置难度 | 低 | 低 | 中 | 高 | 中 |
| Web 界面 | ❌ | ❌ | ❌ | ❌ | ❌ |

## 项目结构

```
dn42-dns/
├── cmd/
│   └── smartdns/
│       └── main.go          # 主入口
├── internal/
│   ├── config/
│   │   └── config.go        # 配置管理
│   ├── server/
│   │   └── server.go        # DNS 服务器核心
│   ├── filter/
│   │   └── filter.go        # 广告过滤引擎 (Radix 树)
│   ├── cache/
│   │   └── cache.go         # DNS 缓存
│   ├── upstream/
│   │   └── upstream.go      # 上游 DNS 管理
│   └── router/
│       └── router.go        # 分流路由
├── configs/
│   ├── dn42.yaml            # dn42 推荐配置
│   └── config.yaml          # 通用配置
├── rules/
│   ├── blacklist.txt        # 广告黑名单
│   └── whitelist.txt        # 白名单
├── go.mod
└── README.md
```

## 常见问题

### Q: dn42 域名解析失败怎么办？

A: 请检查：
1. 你的节点是否已正确接入 dn42 网络
2. 是否能 ping 通 `172.20.0.53`
3. 防火墙是否允许 UDP/TCP 53 端口的出站流量

### Q: 如何添加更多 dn42 顶级域？

A: 在配置的 `routing.domain_suffix.dn42` 中添加即可。例如添加 `.opennic`：
```yaml
routing:
  domain_suffix:
    dn42:
      - ".dn42"
      - ".opennic"
```

### Q: 如何订阅更多广告规则？

A: 在 `adblock.blacklist_files` 中添加规则源 URL：
```yaml
blacklist_files:
  - "rules/blacklist.txt"
  - "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
  - "https://big.oisd.nl/basic"
```

### Q: 支持哪些操作系统？

A: 支持 Linux、macOS、FreeBSD、Windows 等 Go 语言支持的所有平台。推荐在 Linux 上部署。

### Q: 如何与 BIRD/FRR 配合使用？

A: dn42-dns 与路由软件完全独立，只需确保：
1. dn42-dns 能通过 dn42 网络访问到上游 DNS 服务器
2. 路由表中已有 dn42 相关网段的路由
3. 建议将 dn42-dns 部署在与 BIRD/FRR 同一台机器上

## 相关链接

- [dn42 官方网站](https://dn42.eu/)
- [dn42 Wiki - DNS](https://dn42.eu/howto/DNS)
- [dn42 公共递归 DNS 列表](https://dn42.eu/services/DNS)
- [Radix 树算法详解](https://en.wikipedia.org/wiki/Radix_tree)

## License

GPLv3
