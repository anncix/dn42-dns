/**
 * API 封装 - 与后端 API 调用
 */
const API = (function() {

    // 通用 GET
    async function get(path) {
        const resp = await Auth.authFetch(path);
        if (!resp.ok) {
            throw new Error(`HTTP ${resp.status}`);
        }
        return resp.json();
    }

    async function post(path, data) {
        const resp = await Auth.authFetch(path, {
            method: 'POST',
            headers: data ? { 'Content-Type': 'application/json' } : {},
            body: data ? JSON.stringify(data) : undefined
        });
        if (!resp.ok) {
            throw new Error(`HTTP ${resp.status}`);
        }
        return resp.json();
    }

    // DNS 相关 API
    const dns = {
        health: () => get('/api/dns/health'),
        stats: () => get('/api/dns/stats'),
        dashboard: () => get('/api/dns/dashboard'),
        cacheStats: () => get('/api/dns/cache/stats'),
        flushCache: () => post('/api/dns/cache/flush'),
        rdnsRecords: () => get('/api/dns/rdns/records'),
        upstreams: (group = 'default') => get(`/api/dns/upstreams?group=${encodeURIComponent(group)}`),
        haStatus: () => get('/api/dns/ha/status'),
        mailRecords: () => get('/api/dns/mail/records'),
        mailStats: () => get('/api/dns/mail/stats'),
        addMX: (domain, server, priority) => post('/api/dns/mail/mx', {
            action: 'add', domain, server, priority
        }),
        deleteMX: (domain, server) => post('/api/dns/mail/mx', {
            action: 'delete', domain, server
        }),
        addA: (domain, ip) => post('/api/dns/mail/a', {
            action: 'add', domain, ip
        }),
        deleteA: (domain) => post('/api/dns/mail/a', {
            action: 'delete', domain
        }),
        addSPF: (domain, value) => post('/api/dns/mail/spf', { domain, value }),
        addDKIM: (selector, domain, value) => post('/api/dns/mail/dkim', {
            selector, domain, value
        }),
        addDMARC: (domain, value) => post('/api/dns/mail/dmarc', { domain, value })
    };

    // 认证 API（不用 authFetch
    async function login(username, password) {
        const resp = await fetch('/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password })
        });
        if (!resp.ok) {
            const data = await resp.json().catch(() => ({}));
            throw new Error(data.detail || '登录失败');
        }
        return resp.json();
    }

    async function changePassword(oldPassword, newPassword) {
        return post('/api/auth/change-password', {
            old_password: oldPassword,
            new_password: newPassword
        });
    }

    return {
        get,
        post,
        dns,
        login,
        changePassword
    };
})();
