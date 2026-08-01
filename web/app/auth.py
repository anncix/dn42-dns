"""
认证模块 - JWT + Refresh Token 实现无感登录
"""
import os
import time
import hashlib
import secrets
from typing import Optional
from dataclasses import dataclass

import jwt
from fastapi import HTTPException, status

# 配置
SECRET_KEY = os.environ.get("DN42_DNS_SECRET", secrets.token_hex(32))
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = 15    # access token 15分钟
REFRESH_TOKEN_EXPIRE_DAYS = 7       # refresh token 7天

# 默认管理员密码（首次登录用）
DEFAULT_USERNAME = "admin"
DEFAULT_PASSWORD = "admin123"

# 存储 refresh token（生产环境用数据库）
_refresh_tokens: dict[str, dict] = {}

# 用户密码（生产环境用数据库，这里用 hash 存储）
_users = {
    DEFAULT_USERNAME: hashlib.sha256(DEFAULT_PASSWORD.encode()).hexdigest()
}


@dataclass
class TokenPair:
    access_token: str
    refresh_token: str
    token_type: str = "bearer"


def hash_password(password: str) -> str:
    """密码哈希"""
    return hashlib.sha256(password.encode()).hexdigest()


def verify_user(username: str, password: str) -> bool:
    """验证用户名密码"""
    if username not in _users:
        return False
    return _users[username] == hash_password(password)


def create_access_token(username: str) -> str:
    """创建 access token"""
    expire = time.time() + ACCESS_TOKEN_EXPIRE_MINUTES * 60
    payload = {
        "sub": username,
        "exp": expire,
        "type": "access"
    }
    return jwt.encode(payload, SECRET_KEY, algorithm=ALGORITHM)


def create_refresh_token(username: str) -> str:
    """创建 refresh token"""
    expire = time.time() + REFRESH_TOKEN_EXPIRE_DAYS * 24 * 3600
    token_id = secrets.token_hex(16)
    payload = {
        "sub": username,
        "exp": expire,
        "type": "refresh",
        "jti": token_id
    }
    token = jwt.encode(payload, SECRET_KEY, algorithm=ALGORITHM)
    # 存储 refresh token，便于吊销
    _refresh_tokens[token_id] = {
        "username": username,
        "expire": expire,
        "token": token
    }
    return token


def create_token_pair(username: str) -> TokenPair:
    """创建 token 对"""
    return TokenPair(
        access_token=create_access_token(username),
        refresh_token=create_refresh_token(username)
    )


def decode_token(token: str, token_type: str = "access") -> Optional[dict]:
    """解码并验证 token"""
    try:
        payload = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
        if payload.get("type") != token_type:
            return None
        return payload
    except jwt.ExpiredSignatureError:
        return None
    except jwt.InvalidTokenError:
        return None


def refresh_access_token(refresh_token: str) -> Optional[TokenPair]:
    """用 refresh token 刷新 access token（无感刷新的核心）"""
    payload = decode_token(refresh_token, "refresh")
    if not payload:
        return None

    username = payload.get("sub")
    token_id = payload.get("jti")

    # 验证 refresh token 是否在存储中（防止被吊销）
    if token_id not in _refresh_tokens:
        return None

    # 验证用户名一致
    if _refresh_tokens[token_id]["username"] != username:
        return None

    # 生成新的 token 对（轮换 refresh token，更安全）
    new_pair = create_token_pair(username)

    # 吊销旧的 refresh token
    del _refresh_tokens[token_id]

    return new_pair


def get_current_user(authorization: str) -> str:
    """从 Authorization header 获取当前用户（依赖注入用）"""
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="无效的认证凭据",
            headers={"WWW-Authenticate": "Bearer"},
        )

    token = authorization.split(" ")[1]
    payload = decode_token(token, "access")
    if not payload:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Token 已过期或无效",
            headers={"WWW-Authenticate": "Bearer"},
        )

    return payload["sub"]


def change_password(username: str, old_password: str, new_password: str) -> bool:
    """修改密码"""
    if not verify_user(username, old_password):
        return False
    _users[username] = hash_password(new_password)
    return True
