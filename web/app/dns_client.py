"""
DNS API 代理 - 与 Go DNS 服务器的 HTTP API 通信
"""
import os
import httpx

# DNS 服务器 API 地址
DNS_API_BASE = os.environ.get("DNS_API_URL", "http://127.0.0.1:8080")


async def dns_api_get(path: str) -> dict:
    """调用 DNS 服务器 GET API"""
    try:
        async with httpx.AsyncClient(timeout=3.0) as client:
            resp = await client.get(f"{DNS_API_BASE}{path}")
            if resp.status_code == 200:
                return resp.json()
            return {"error": f"HTTP {resp.status_code}"}
    except Exception as e:
        return {"error": str(e)}


async def dns_api_post(path: str, data: dict = None) -> dict:
    """调用 DNS 服务器 POST API"""
    try:
        async with httpx.AsyncClient(timeout=3.0) as client:
            resp = await client.post(f"{DNS_API_BASE}{path}", json=data or {})
            if resp.status_code == 200:
                return resp.json()
            return {"error": f"HTTP {resp.status_code}"}
    except Exception as e:
        return {"error": str(e)}


async def get_health() -> dict:
    """健康检查"""
    return await dns_api_get("/api/health")


async def get_stats() -> dict:
    """统计信息"""
    return await dns_api_get("/api/stats")


async def get_cache_stats() -> dict:
    """缓存统计"""
    return await dns_api_get("/api/cache/stats")


async def flush_cache() -> dict:
    """清空缓存"""
    return await dns_api_post("/api/cache/flush")


async def get_rdns_records() -> dict:
    """获取 rDNS 记录"""
    return await dns_api_get("/api/rdns/records")


async def get_upstream_stats(group: str = "default") -> dict:
    """获取上游服务器状态"""
    return await dns_api_get(f"/api/upstreams?group={group}")


async def get_ha_status() -> dict:
    """获取 HA 状态"""
    return await dns_api_get("/api/ha/status")


async def get_mail_records() -> dict:
    """获取邮局DNS所有记录"""
    return await dns_api_get("/api/mail/records")


async def get_mail_stats() -> dict:
    """获取邮局DNS统计"""
    return await dns_api_get("/api/mail/stats")


async def add_mx_record(domain: str, server: str, priority: int) -> dict:
    """添加 MX 记录"""
    return await dns_api_post("/api/mail/mx", {
        "action": "add",
        "domain": domain,
        "server": server,
        "priority": priority
    })


async def delete_mx_record(domain: str, server: str) -> dict:
    """删除 MX 记录"""
    return await dns_api_post("/api/mail/mx", {
        "action": "delete",
        "domain": domain,
        "server": server
    })


async def add_a_record(domain: str, ip: str) -> dict:
    """添加 A 记录"""
    return await dns_api_post("/api/mail/a", {
        "action": "add",
        "domain": domain,
        "ip": ip
    })


async def delete_a_record(domain: str) -> dict:
    """删除 A 记录"""
    return await dns_api_post("/api/mail/a", {
        "action": "delete",
        "domain": domain
    })


async def add_spf_record(domain: str, value: str) -> dict:
    """添加 SPF 记录"""
    return await dns_api_post("/api/mail/spf", {
        "action": "add",
        "domain": domain,
        "value": value
    })


async def add_dkim_record(selector: str, domain: str, value: str) -> dict:
    """添加 DKIM 记录"""
    return await dns_api_post("/api/mail/dkim", {
        "action": "add",
        "selector": selector,
        "domain": domain,
        "value": value
    })


async def add_dmarc_record(domain: str, value: str) -> dict:
    """添加 DMARC 记录"""
    return await dns_api_post("/api/mail/dmarc", {
        "action": "add",
        "domain": domain,
        "value": value
    })


async def get_dashboard_data() -> dict:
    """获取仪表盘所有数据（聚合多个 API）"""
    health, stats, cache_stats, ha = await asyncio.gather(
        get_health(),
        get_stats(),
        get_cache_stats(),
        get_ha_status(),
        return_exceptions=True
    )

    return {
        "health": health if not isinstance(health, Exception) else {"error": str(health)},
        "stats": stats if not isinstance(stats, Exception) else {"error": str(stats)},
        "cache": cache_stats if not isinstance(cache_stats, Exception) else {"error": str(cache_stats)},
        "ha": ha if not isinstance(ha, Exception) else {"error": str(ha)},
    }


import asyncio
