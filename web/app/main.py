"""
dn42-dns 管理面板 - FastAPI 后端
"""
import os
from pathlib import Path
from typing import Optional

from fastapi import FastAPI, Depends, HTTPException, Header, Request, Form, status
from fastapi.responses import HTMLResponse, JSONResponse, RedirectResponse, FileResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

from . import auth
from . import dns_client

# 路径
BASE_DIR = Path(__file__).resolve().parent.parent
STATIC_DIR = BASE_DIR / "static"
TEMPLATES_DIR = BASE_DIR / "templates"

# 创建应用
app = FastAPI(title="dn42-dns Admin", version="1.0.0")

# 静态文件
app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")


# ============ Pydantic 模型 ============

class LoginRequest(BaseModel):
    username: str
    password: str


class RefreshRequest(BaseModel):
    refresh_token: str


class ChangePasswordRequest(BaseModel):
    old_password: str
    new_password: str


class MXRecordRequest(BaseModel):
    action: str  # add / delete
    domain: str
    server: str = ""
    priority: int = 10


class ARecordRequest(BaseModel):
    action: str  # add / delete
    domain: str
    ip: str = ""


class SPFRecordRequest(BaseModel):
    domain: str
    value: str


class DKIMRecordRequest(BaseModel):
    selector: str
    domain: str
    value: str


class DMARCRecordRequest(BaseModel):
    domain: str
    value: str


# ============ 页面路由 ============

@app.get("/", response_class=HTMLResponse)
async def index(request: Request):
    """首页 - 管理面板"""
    return FileResponse(str(TEMPLATES_DIR / "index.html"))


@app.get("/login", response_class=HTMLResponse)
async def login_page(request: Request):
    """登录页"""
    return FileResponse(str(TEMPLATES_DIR / "login.html"))


# ============ 认证 API ============

@app.post("/api/auth/login")
async def api_login(req: LoginRequest):
    """登录 - 获取 token 对"""
    if not auth.verify_user(req.username, req.password):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="用户名或密码错误"
        )

    tokens = auth.create_token_pair(req.username)
    return {
        "access_token": tokens.access_token,
        "refresh_token": tokens.refresh_token,
        "token_type": "bearer",
        "username": req.username
    }


@app.post("/api/auth/refresh")
async def api_refresh(req: RefreshRequest):
    """刷新 access token（无感登录核心）"""
    new_tokens = auth.refresh_access_token(req.refresh_token)
    if not new_tokens:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Refresh token 无效或已过期"
        )

    return {
        "access_token": new_tokens.access_token,
        "refresh_token": new_tokens.refresh_token,
        "token_type": "bearer",
    }


@app.post("/api/auth/change-password")
async def api_change_password(
    req: ChangePasswordRequest,
    authorization: Optional[str] = Header(None)
):
    """修改密码"""
    username = auth.get_current_user(authorization or "")
    if not auth.change_password(username, req.old_password, req.new_password):
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="旧密码错误"
        )
    return {"status": "ok", "message": "密码修改成功"}


# ============ DNS 代理 API（需要认证） ============

def _get_auth_header(authorization: Optional[str]) -> str:
    """获取并验证认证 header"""
    return auth.get_current_user(authorization or "")


@app.get("/api/dns/health")
async def api_dns_health(authorization: Optional[str] = Header(None)):
    """DNS 服务器健康检查"""
    _get_auth_header(authorization)
    return await dns_client.get_health()


@app.get("/api/dns/stats")
async def api_dns_stats(authorization: Optional[str] = Header(None)):
    """DNS 统计信息"""
    _get_auth_header(authorization)
    return await dns_client.get_stats()


@app.get("/api/dns/cache/stats")
async def api_dns_cache_stats(authorization: Optional[str] = Header(None)):
    """缓存统计"""
    _get_auth_header(authorization)
    return await dns_client.get_cache_stats()


@app.post("/api/dns/cache/flush")
async def api_dns_cache_flush(authorization: Optional[str] = Header(None)):
    """清空缓存"""
    _get_auth_header(authorization)
    return await dns_client.flush_cache()


@app.get("/api/dns/rdns/records")
async def api_dns_rdns_records(authorization: Optional[str] = Header(None)):
    """rDNS 记录"""
    _get_auth_header(authorization)
    return await dns_client.get_rdns_records()


@app.get("/api/dns/upstreams")
async def api_dns_upstreams(
    group: str = "default",
    authorization: Optional[str] = Header(None)
):
    """上游服务器状态"""
    _get_auth_header(authorization)
    return await dns_client.get_upstream_stats(group)


@app.get("/api/dns/ha/status")
async def api_dns_ha_status(authorization: Optional[str] = Header(None)):
    """HA 状态"""
    _get_auth_header(authorization)
    return await dns_client.get_ha_status()


# ============ 邮局 DNS API ============

@app.get("/api/dns/mail/records")
async def api_dns_mail_records(authorization: Optional[str] = Header(None)):
    """邮局DNS所有记录"""
    _get_auth_header(authorization)
    return await dns_client.get_mail_records()


@app.get("/api/dns/mail/stats")
async def api_dns_mail_stats(authorization: Optional[str] = Header(None)):
    """邮局DNS统计"""
    _get_auth_header(authorization)
    return await dns_client.get_mail_stats()


@app.post("/api/dns/mail/mx")
async def api_dns_mail_mx(
    req: MXRecordRequest,
    authorization: Optional[str] = Header(None)
):
    """MX 记录管理"""
    _get_auth_header(authorization)
    if req.action == "add":
        return await dns_client.add_mx_record(req.domain, req.server, req.priority)
    elif req.action == "delete":
        return await dns_client.delete_mx_record(req.domain, req.server)
    raise HTTPException(status_code=400, detail="无效的操作")


@app.post("/api/dns/mail/a")
async def api_dns_mail_a(
    req: ARecordRequest,
    authorization: Optional[str] = Header(None)
):
    """A 记录管理"""
    _get_auth_header(authorization)
    if req.action == "add":
        return await dns_client.add_a_record(req.domain, req.ip)
    elif req.action == "delete":
        return await dns_client.delete_a_record(req.domain)
    raise HTTPException(status_code=400, detail="无效的操作")


@app.post("/api/dns/mail/spf")
async def api_dns_mail_spf(
    req: SPFRecordRequest,
    authorization: Optional[str] = Header(None)
):
    """SPF 记录管理"""
    _get_auth_header(authorization)
    return await dns_client.add_spf_record(req.domain, req.value)


@app.post("/api/dns/mail/dkim")
async def api_dns_mail_dkim(
    req: DKIMRecordRequest,
    authorization: Optional[str] = Header(None)
):
    """DKIM 记录管理"""
    _get_auth_header(authorization)
    return await dns_client.add_dkim_record(req.selector, req.domain, req.value)


@app.post("/api/dns/mail/dmarc")
async def api_dns_mail_dmarc(
    req: DMARCRecordRequest,
    authorization: Optional[str] = Header(None)
):
    """DMARC 记录管理"""
    _get_auth_header(authorization)
    return await dns_client.add_dmarc_record(req.domain, req.value)


@app.get("/api/dns/dashboard")
async def api_dns_dashboard(authorization: Optional[str] = Header(None)):
    """仪表盘聚合数据"""
    _get_auth_header(authorization)
    return await dns_client.get_dashboard_data()


# ============ 启动配置 ============

def main():
    import uvicorn
    host = os.environ.get("WEB_HOST", "127.0.0.1")
    port = int(os.environ.get("WEB_PORT", "8000"))
    uvicorn.run(app, host=host, port=port, log_level="info")


if __name__ == "__main__":
    main()
