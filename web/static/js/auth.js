/**
 * 认证模块 - 实现无感登录（JWT + 自动刷新）
 */
const Auth = (function() {
    const ACCESS_TOKEN_KEY = 'dn42_dns_access_token';
    const REFRESH_TOKEN_KEY = 'dn42_dns_refresh_token';
    const USERNAME_KEY = 'dn42_dns_username';

    // token 刷新提前量：过期前 60 秒就刷新
    const REFRESH_BEFORE_EXPIRE = 60 * 1000;
    let refreshTimer = null;

    /**
     * 解析 JWT payload
     */
    function parseJwt(token) {
        try {
            const base64Url = token.split('.')[1];
            const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
            const jsonPayload = decodeURIComponent(atob(base64).split('').map(function(c) {
                return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
            }).join(''));
            return JSON.parse(jsonPayload);
        } catch (e) {
            return null;
        }
    }

    /**
     * 获取 access token
     */
    function getAccessToken() {
        return localStorage.getItem(ACCESS_TOKEN_KEY) || '';
    }

    /**
     * 获取 refresh token
     */
    function getRefreshToken() {
        return localStorage.getItem(REFRESH_TOKEN_KEY) || '';
    }

    /**
     * 获取当前用户名
     */
    function getUsername() {
        return localStorage.getItem(USERNAME_KEY) || '';
    }

    /**
     * 存储 token
     */
    function setTokens(accessToken, refreshToken, username) {
        localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
        localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
        if (username) {
            localStorage.setItem(USERNAME_KEY, username);
        }
        scheduleRefresh();
    }

    /**
     * 清除 token（登出）
     */
    function clearTokens() {
        localStorage.removeItem(ACCESS_TOKEN_KEY);
        localStorage.removeItem(REFRESH_TOKEN_KEY);
        localStorage.removeItem(USERNAME_KEY);
        if (refreshTimer) {
            clearTimeout(refreshTimer);
            refreshTimer = null;
        }
    }

    /**
     * 检查是否已登录
     */
    function isLoggedIn() {
        const token = getAccessToken();
        if (!token) return false;

        const payload = parseJwt(token);
        if (!payload) return false;

        // 检查是否过期（给 60 秒缓冲）
        const exp = payload.exp * 1000;
        return exp > Date.now();
    }

    /**
     * 检查是否有有效的 refresh token
     */
    function hasRefreshToken() {
        const token = getRefreshToken();
        if (!token) return false;

        const payload = parseJwt(token);
        if (!payload) return false;

        const exp = payload.exp * 1000;
        return exp > Date.now();
    }

    /**
     * 刷新 access token
     */
    async function refreshToken() {
        const refresh = getRefreshToken();
        if (!refresh) return false;

        try {
            const resp = await fetch('/api/auth/refresh', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ refresh_token: refresh })
            });

            if (!resp.ok) {
                clearTokens();
                return false;
            }

            const data = await resp.json();
            setTokens(data.access_token, data.refresh_token);
            return true;
        } catch (e) {
            console.error('Token refresh failed:', e);
            return false;
        }
    }

    /**
     * 安排自动刷新（无感刷新的核心）
     */
    function scheduleRefresh() {
        if (refreshTimer) {
            clearTimeout(refreshTimer);
            refreshTimer = null;
        }

        const token = getAccessToken();
        if (!token) return;

        const payload = parseJwt(token);
        if (!payload) return;

        const exp = payload.exp * 1000;
        // 在过期前 REFRESH_BEFORE_EXPIRE 毫秒刷新
        const delay = exp - Date.now() - REFRESH_BEFORE_EXPIRE;

        if (delay > 0) {
            refreshTimer = setTimeout(async () => {
                await refreshToken();
            }, delay);
        } else if (hasRefreshToken()) {
            // 已经快过期了，立即刷新
            refreshToken();
        }
    }

    /**
     * 确保有有效的 access token（调用 API 前用）
     */
    async function ensureValidToken() {
        if (isLoggedIn()) return true;
        if (hasRefreshToken()) {
            return await refreshToken();
        }
        return false;
    }

    /**
     * 带认证的 fetch 封装
     */
    async function authFetch(url, options = {}) {
        const valid = await ensureValidToken();
        if (!valid) {
            // 跳到登录页
            if (window.location.pathname !== '/login') {
                window.location.href = '/login';
            }
            throw new Error('Not authenticated');
        }

        const headers = options.headers || {};
        headers['Authorization'] = `Bearer ${getAccessToken()}`;
        options.headers = headers;

        const resp = await fetch(url, options);

        // 如果返回 401，尝试刷新一次
        if (resp.status === 401) {
            const refreshed = await refreshToken();
            if (refreshed) {
                headers['Authorization'] = `Bearer ${getAccessToken()}`;
                return await fetch(url, options);
            } else {
                clearTokens();
                if (window.location.pathname !== '/login') {
                    window.location.href = '/login';
                }
                throw new Error('Session expired');
            }
        }

        return resp;
    }

    // 页面加载时安排刷新
    if (typeof window !== 'undefined') {
        if (isLoggedIn()) {
            scheduleRefresh();
        }
    }

    return {
        getAccessToken,
        getRefreshToken,
        getUsername,
        setTokens,
        clearTokens,
        isLoggedIn,
        hasRefreshToken,
        refreshToken,
        ensureValidToken,
        authFetch,
        parseJwt
    };
})();
