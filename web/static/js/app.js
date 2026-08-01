/**
 * 主应用 - 管理面板页面逻辑
 */
const App = (function() {
    let currentPage = 'dashboard';
    let refreshInterval = null;

    // 页面标题映射
    const pageTitles = {
        dashboard: '仪表盘',
        cache: '缓存管理',
        rdns: 'rDNS 记录',
        upstreams: '上游服务器',
        ha: '主从状态',
        mail: '邮局DNS',
        settings: '系统设置'
    };

    // 初始化
    async function init() {
        // 检查登录状态
        if (!Auth.isLoggedIn() && !Auth.hasRefreshToken()) {
            window.location.href = '/login';
            return;
        }

        if (!Auth.isLoggedIn() && Auth.hasRefreshToken()) {
            const valid = await Auth.ensureValidToken();
            if (!valid) {
                window.location.href = '/login';
                return;
            }
        }

        // 显示用户名
        const usernameEl = document.getElementById('usernameDisplay');
        if (usernameEl) {
            usernameEl.textContent = Auth.getUsername() || 'admin';
        }

        // 绑定导航事件
        bindNavEvents();

        // 绑定登出事件
        document.getElementById('logoutBtn').addEventListener('click', function(e) {
            e.preventDefault();
            logout();
        });

        // 加载仪表盘
        loadPage('dashboard');

        // 启动自动刷新
        startAutoRefresh();
    }

    // 绑定导航
    function bindNavEvents() {
        document.querySelectorAll('.nav-item').forEach(item => {
            item.addEventListener('click', function(e) {
                e.preventDefault();
                const page = this.dataset.page;
                if (page && page !== currentPage) {
                    loadPage(page);
                }
            });
        });
    }

    // 切换页面
    function loadPage(page) {
        currentPage = page;

        // 更新导航激活状态
        document.querySelectorAll('.nav-item').forEach(item => {
            item.classList.toggle('active', item.dataset.page === page);
        });

        // 更新标题
        document.getElementById('pageTitle').textContent = pageTitles[page] || page;

        // 加载页面内容
        const content = document.getElementById('pageContent');
        content.innerHTML = '<div class="loading"><div class="spinner"></div>加载中...</div>';

        switch (page) {
            case 'dashboard':
                renderDashboard();
                break;
            case 'cache':
                renderCachePage();
                break;
            case 'rdns':
                renderRdnsPage();
                break;
            case 'upstreams':
                renderUpstreamsPage();
                break;
            case 'ha':
                renderHaPage();
                break;
            case 'mail':
                renderMailPage();
                break;
            case 'settings':
                renderSettingsPage();
                break;
        }
    }

    // 更新健康状态徽章
    async function updateHealthBadge() {
        try {
            const data = await API.dns.health();
            const badge = document.getElementById('healthBadge');
            const text = document.getElementById('healthText');

            if (data.status === 'ok') {
                badge.className = 'status-badge healthy';
                text.textContent = '服务正常';
            } else {
                badge.className = 'status-badge unhealthy';
                text.textContent = '服务异常';
            }
        } catch (e) {
            const badge = document.getElementById('healthBadge');
            const text = document.getElementById('healthText');
            badge.className = 'status-badge unhealthy';
            text.textContent = '连接失败';
        }
    }

    // ============ 仪表盘 ============
    async function renderDashboard() {
        try {
            const data = await API.dns.dashboard();
            const content = document.getElementById('pageContent');

            const stats = data.stats || {};
            const cache = data.cache || {};
            const ha = data.ha || {};
            const health = data.health || {};

            const cacheStats = stats.cache || { TotalQueries: 0, CacheHits: 0, CacheMisses: 0 };
            const rdnsStats = stats.rdns || { TotalQueries: 0, LocalHits: 0, Forwarded: 0, Dropped: 0 };

            content.innerHTML = `
                <div class="stats-grid">
                    <div class="stat-card">
                        <div class="stat-value accent">${cache.size || 0}</div>
                        <div class="stat-label">缓存条目</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value success">${cache.hit_rate || '0%'}</div>
                        <div class="stat-label">缓存命中率</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value">${rdnsStats.TotalQueries || 0}</div>
                        <div class="stat-label">rDNS 查询</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value warning">${ha.mode || 'standalone'}</div>
                        <div class="stat-label">运行模式</div>
                    </div>
                </div>

                <div class="card" style="margin-bottom: 24px;">
                    <div class="card-header">
                        <h3 class="card-title">运行状态</h3>
                    </div>
                    <table>
                        <tr>
                            <td style="width: 30%; font-weight: 500;">DNS 服务状态</td>
                            <td>${health.status === 'ok'
                                ? '<span class="badge badge-success">正常运行</span>'
                                : '<span class="badge badge-danger">异常</span>'}
                            </td>
                        </tr>
                        <tr>
                            <td style="font-weight: 500;">总查询数</td>
                            <td>${cacheStats.TotalQueries || 0}</td>
                        </tr>
                        <tr>
                            <td style="font-weight: 500;">缓存命中</td>
                            <td>${cacheStats.CacheHits || 0}</td>
                        </tr>
                        <tr>
                            <td style="font-weight: 500;">缓存未命中</td>
                            <td>${cacheStats.CacheMisses || 0}</td>
                        </tr>
                        <tr>
                            <td style="font-weight: 500;">rDNS 本地命中</td>
                            <td>${rdnsStats.LocalHits || 0}</td>
                        </tr>
                        <tr>
                            <td style="font-weight: 500;">rDNS 转发</td>
                            <td>${rdnsStats.Forwarded || 0}</td>
                        </tr>
                    </table>
                </div>

                <div class="card">
                    <div class="card-header">
                        <h3 class="card-title">快捷操作</h3>
                    </div>
                    <div style="display: flex; gap: 12px; flex-wrap: wrap;">
                        <button class="btn btn-danger" onclick="App.flushCache()">
                            🗑️ 清空缓存
                        </button>
                        <button class="btn btn-secondary" onclick="App.refreshPage()">
                            🔄 刷新数据
                        </button>
                    </div>
                </div>
            `;
        } catch (e) {
            document.getElementById('pageContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载数据失败：${e.message}</p>
                        <p style="margin-top: 8px; font-size: 13px;">请确保 DNS 服务器正在运行并启用了 API</p>
                    </div>
                </div>
            `;
        }
    }

    // ============ 缓存管理页 ============
    async function renderCachePage() {
        try {
            const data = await API.dns.cacheStats();
            const content = document.getElementById('pageContent');

            content.innerHTML = `
                <div class="stats-grid">
                    <div class="stat-card">
                        <div class="stat-value accent">${data.size || 0}</div>
                        <div class="stat-label">缓存条目数</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value success">${data.hit_rate || '0%'}</div>
                        <div class="stat-label">命中率</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value">${data.hits || 0}</div>
                        <div class="stat-label">命中次数</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-value warning">${data.misses || 0}</div>
                        <div class="stat-label">未命中次数</div>
                    </div>
                </div>

                <div class="card">
                    <div class="card-header">
                        <h3 class="card-title">缓存操作</h3>
                    </div>
                    <p style="margin-bottom: 16px; color: var(--muted);">
                        清空缓存后，所有 DNS 查询将重新向上游服务器请求。
                        建议在配置变更或排查问题时使用。
                    </p>
                    <button class="btn btn-danger" onclick="App.flushCache()">
                        🗑️ 清空所有缓存
                    </button>
                </div>
            `;
        } catch (e) {
            document.getElementById('pageContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载缓存数据失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    // ============ rDNS 记录页 ============
    async function renderRdnsPage() {
        try {
            const data = await API.dns.rdnsRecords();
            const content = document.getElementById('pageContent');
            const records = data.records || {};
            const recordList = Object.entries(records);

            content.innerHTML = `
                <div class="action-bar">
                    <div>
                        <input type="text" class="search-box" id="rdnsSearch" placeholder="搜索 IP 或域名..." oninput="App.filterRdns()">
                    </div>
                    <div>
                        <span style="color: var(--muted); font-size: 13px;">共 ${recordList.length} 条记录</span>
                    </div>
                </div>

                <div class="card">
                    <div class="table-wrap" id="rdnsTableWrap">
                        ${renderRdnsTable(recordList)}
                    </div>
                </div>
            `;
        } catch (e) {
            document.getElementById('pageContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载 rDNS 记录失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    function renderRdnsTable(records) {
        if (records.length === 0) {
            return `
                <div class="empty-state">
                    <div class="empty-icon">📋</div>
                    <p>暂无 rDNS 记录</p>
                </div>
            `;
        }

        return `
            <table>
                <thead>
                    <tr>
                        <th>IP 地址</th>
                        <th>域名</th>
                    </tr>
                </thead>
                <tbody>
                    ${records.map(([ip, domain]) => `
                        <tr>
                            <td><code>${ip}</code></td>
                            <td>${domain}</td>
                        </tr>
                    `).join('')}
                </tbody>
            </table>
        `;
    }

    function filterRdns() {
        const keyword = document.getElementById('rdnsSearch').value.toLowerCase();
        // 重新加载并过滤（简化实现：实际项目可缓存数据）
        API.dns.rdnsRecords().then(data => {
            const records = Object.entries(data.records || {}).filter(([ip, domain]) =>
                ip.toLowerCase().includes(keyword) || domain.toLowerCase().includes(keyword)
            );
            document.getElementById('rdnsTableWrap').innerHTML = renderRdnsTable(records);
        });
    }

    // ============ 上游服务器页 ============
    async function renderUpstreamsPage() {
        try {
            const groups = ['default', 'dn42'];
            const content = document.getElementById('pageContent');
            let html = '<div class="action-bar"><div>';

            // 组切换
            groups.forEach((g, i) => {
                html += `<button class="btn ${i === 0 ? 'btn-primary' : 'btn-secondary'} btn-sm"
                    onclick="App.loadUpstreamsGroup('${g}')" id="groupBtn_${g}">${g}</button> `;
            });

            html += '</div></div>';
            html += '<div id="upstreamsContent"><div class="loading"><div class="spinner"></div>加载中...</div></div>';

            content.innerHTML = html;

            // 加载默认组
            loadUpstreamsGroup('default');
        } catch (e) {
            document.getElementById('pageContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载上游状态失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    async function loadUpstreamsGroup(group) {
        // 更新按钮状态
        ['default', 'dn42'].forEach(g => {
            const btn = document.getElementById(`groupBtn_${g}`);
            if (btn) {
                btn.className = `btn ${g === group ? 'btn-primary' : 'btn-secondary'} btn-sm`;
            }
        });

        try {
            const data = await API.dns.upstreams(group);
            const servers = data.servers || [];

            document.getElementById('upstreamsContent').innerHTML = `
                <div class="card">
                    <div class="table-wrap">
                        <table>
                            <thead>
                                <tr>
                                    <th>名称</th>
                                    <th>地址</th>
                                    <th>协议</th>
                                    <th>状态</th>
                                    <th>平均延迟</th>
                                    <th>失败次数</th>
                                </tr>
                            </thead>
                            <tbody>
                                ${servers.map(s => `
                                    <tr>
                                        <td>${s.name || '-'}</td>
                                        <td><code>${s.addr}</code></td>
                                        <td>${s.protocol}</td>
                                        <td>${s.healthy
                                            ? '<span class="badge badge-success">健康</span>'
                                            : '<span class="badge badge-danger">异常</span>'}
                                        </td>
                                        <td>${s.avg_latency_ms > 0 ? s.avg_latency_ms + ' ms' : '-'}</td>
                                        <td>${s.fail_count || 0}</td>
                                    </tr>
                                `).join('')}
                            </tbody>
                        </table>
                    </div>
                </div>
            `;
        } catch (e) {
            document.getElementById('upstreamsContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    // ============ HA 状态页 ============
    async function renderHaPage() {
        try {
            const data = await API.dns.haStatus();
            const content = document.getElementById('pageContent');

            let html = `
                <div class="stats-grid">
                    <div class="stat-card">
                        <div class="stat-value accent">${data.mode || 'standalone'}</div>
                        <div class="stat-label">运行模式</div>
                    </div>
                </div>
            `;

            if (data.mode === 'master' && data.slaves) {
                const slaves = Object.values(data.slaves);
                html += `
                    <div class="card">
                        <div class="card-header">
                            <h3 class="card-title">从节点状态</h3>
                        </div>
                        <div class="table-wrap">
                            <table>
                                <thead>
                                    <tr>
                                        <th>地址</th>
                                        <th>状态</th>
                                        <th>延迟</th>
                                        <th>最后检查</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    ${slaves.map(s => `
                                        <tr>
                                            <td><code>${s.addr}</code></td>
                                            <td>${s.healthy
                                                ? '<span class="badge badge-success">在线</span>'
                                                : '<span class="badge badge-danger">离线</span>'}
                                            </td>
                                            <td>${s.latency_ms} ms</td>
                                            <td>${new Date(s.last_check).toLocaleString()}</td>
                                        </tr>
                                    `).join('')}
                                </tbody>
                            </table>
                        </div>
                    </div>
                `;
            } else if (data.mode === 'slave' && data.master) {
                const m = data.master;
                html += `
                    <div class="card">
                        <div class="card-header">
                            <h3 class="card-title">主节点状态</h3>
                        </div>
                        <table>
                            <tr>
                                <td style="width: 30%; font-weight: 500;">地址</td>
                                <td><code>${m.addr}</code></td>
                            </tr>
                            <tr>
                                <td style="font-weight: 500;">状态</td>
                                <td>${m.healthy
                                    ? '<span class="badge badge-success">在线</span>'
                                    : '<span class="badge badge-danger">离线</span>'}
                                </td>
                            </tr>
                            <tr>
                                <td style="font-weight: 500;">延迟</td>
                                <td>${m.latency_ms} ms</td>
                            </tr>
                            <tr>
                                <td style="font-weight: 500;">最后检查</td>
                                <td>${new Date(m.last_check).toLocaleString()}</td>
                            </tr>
                        </table>
                    </div>
                `;
            } else {
                html += `
                    <div class="card">
                        <div class="empty-state">
                            <div class="empty-icon">🔧</div>
                            <p>当前为单机模式</p>
                            <p style="margin-top: 8px; font-size: 13px; color: var(--muted);">
                                配置 ha.mode 为 master 或 slave 启用主从模式
                            </p>
                        </div>
                    </div>
                `;
            }

            content.innerHTML = html;
        } catch (e) {
            document.getElementById('pageContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载 HA 状态失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    // ============ 邮局DNS页 ============
    let mailCurrentTab = 'mx';

    async function renderMailPage() {
        const content = document.getElementById('pageContent');
        content.innerHTML = `
            <div class="tabs">
                <button class="tab-btn active" data-tab="mx" onclick="App.switchMailTab('mx')">MX 记录</button>
                <button class="tab-btn" data-tab="a" onclick="App.switchMailTab('a')">A 记录</button>
                <button class="tab-btn" data-tab="spf" onclick="App.switchMailTab('spf')">SPF</button>
                <button class="tab-btn" data-tab="dkim" onclick="App.switchMailTab('dkim')">DKIM</button>
                <button class="tab-btn" data-tab="dmarc" onclick="App.switchMailTab('dmarc')">DMARC</button>
            </div>
            <div id="mailTabContent">
                <div class="loading"><div class="spinner"></div>加载中...</div>
            </div>
        `;

        try {
            const data = await API.dns.mailRecords();
            window._mailRecords = data;
            renderMailTabContent('mx');
        } catch (e) {
            document.getElementById('mailTabContent').innerHTML = `
                <div class="card">
                    <div class="empty-state">
                        <div class="empty-icon">⚠️</div>
                        <p>加载失败：${e.message}</p>
                    </div>
                </div>
            `;
        }
    }

    function switchMailTab(tab) {
        mailCurrentTab = tab;
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.tab === tab);
        });
        renderMailTabContent(tab);
    }

    function renderMailTabContent(tab) {
        const data = window._mailRecords || {};
        const container = document.getElementById('mailTabContent');

        switch (tab) {
            case 'mx':
                renderMXRecords(data.mx || {});
                break;
            case 'a':
                renderARecords(data.a || {});
                break;
            case 'spf':
                renderSPFRecords(data.spf || {});
                break;
            case 'dkim':
                renderDKIMRecords(data.dkim || {});
                break;
            case 'dmarc':
                renderDMARCRecords(data.dmarc || {});
                break;
        }
    }

    function renderMXRecords(records) {
        // records 是 { domain: [MXRecord] } 结构
        const allRecords = [];
        for (const [domain, mxList] of Object.entries(records)) {
            for (const mx of mxList) {
                allRecords.push({ domain, server: mx.Server, priority: mx.Priority });
            }
        }

        document.getElementById('mailTabContent').innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">MX 记录（邮件交换）</h3>
                    <button class="btn btn-primary btn-sm" onclick="App.showAddMXModal()">+ 添加</button>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>域名</th>
                                <th>邮件服务器</th>
                                <th>优先级</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${allRecords.length === 0 ? `
                                <tr><td colspan="4" style="text-align:center; padding:40px; color: var(--muted);">
                                    暂无 MX 记录
                                </td></tr>
                            ` : allRecords.map(r => `
                                <tr>
                                    <td><code>${r.domain}</code></td>
                                    <td><code>${r.server}</code></td>
                                    <td>${r.priority}</td>
                                    <td>
                                        <button class="btn btn-danger btn-sm"
                                            onclick="App.deleteMX('${r.domain}', '${r.server}')">
                                            删除
                                        </button>
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    }

    function renderARecords(records) {
        const recordList = Object.entries(records);

        document.getElementById('mailTabContent').innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">A 记录（邮件服务器地址）</h3>
                    <button class="btn btn-primary btn-sm" onclick="App.showAddAModal()">+ 添加</button>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>主机名</th>
                                <th>IP 地址</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${recordList.length === 0 ? `
                                <tr><td colspan="3" style="text-align:center; padding:40px; color: var(--muted);">
                                    暂无 A 记录
                                </td></tr>
                            ` : recordList.map(([domain, ip]) => `
                                <tr>
                                    <td><code>${domain}</code></td>
                                    <td><code>${ip}</code></td>
                                    <td>
                                        <button class="btn btn-danger btn-sm"
                                            onclick="App.deleteA('${domain}')">
                                            删除
                                        </button>
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    }

    function renderSPFRecords(records) {
        const recordList = Object.entries(records);

        document.getElementById('mailTabContent').innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">SPF 记录（发件人策略）</h3>
                    <button class="btn btn-primary btn-sm" onclick="App.showAddSPFModal()">+ 添加</button>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>域名</th>
                                <th>SPF 值</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${recordList.length === 0 ? `
                                <tr><td colspan="2" style="text-align:center; padding:40px; color: var(--muted);">
                                    暂无 SPF 记录
                                </td></tr>
                            ` : recordList.map(([domain, value]) => `
                                <tr>
                                    <td><code>${domain}</code></td>
                                    <td style="max-width:400px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
                                        <code>${value}</code>
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    }

    function renderDKIMRecords(records) {
        const recordList = Object.entries(records);

        document.getElementById('mailTabContent').innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">DKIM 记录（域名密钥）</h3>
                    <button class="btn btn-primary btn-sm" onclick="App.showAddDKIMModal()">+ 添加</button>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>选择器</th>
                                <th>域名</th>
                                <th>公钥值</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${recordList.length === 0 ? `
                                <tr><td colspan="3" style="text-align:center; padding:40px; color: var(--muted);">
                                    暂无 DKIM 记录
                                </td></tr>
                            ` : recordList.map(([key, value]) => {
                                // key 格式: selector._domainkey.example.com
                                const parts = key.split('._domainkey.');
                                const selector = parts[0];
                                const domain = parts[1] || key;
                                return `
                                    <tr>
                                        <td><code>${selector}</code></td>
                                        <td><code>${domain}</code></td>
                                        <td style="max-width:300px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
                                            <code>${value.substring(0, 50)}...</code>
                                        </td>
                                    </tr>
                                `;
                            }).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    }

    function renderDMARCRecords(records) {
        const recordList = Object.entries(records);

        document.getElementById('mailTabContent').innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">DMARC 记录（邮件认证报告）</h3>
                    <button class="btn btn-primary btn-sm" onclick="App.showAddDMARCModal()">+ 添加</button>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>域名</th>
                                <th>DMARC 策略</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${recordList.length === 0 ? `
                                <tr><td colspan="2" style="text-align:center; padding:40px; color: var(--muted);">
                                    暂无 DMARC 记录
                                </td></tr>
                            ` : recordList.map(([key, value]) => {
                                // key 格式: _dmarc.example.com
                                const domain = key.replace('_dmarc.', '');
                                return `
                                    <tr>
                                        <td><code>${domain}</code></td>
                                        <td style="max-width:400px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
                                            <code>${value}</code>
                                        </td>
                                    </tr>
                                `;
                            }).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    }

    // ============ 邮局DNS操作 ============
    async function showAddMXModal() {
        const domain = prompt('请输入域名（如 example.dn42）:');
        if (!domain) return;
        const server = prompt('请输入邮件服务器（如 mail.example.dn42）:');
        if (!server) return;
        const priorityStr = prompt('请输入优先级（数字越小越优先，默认 10）:', '10');
        if (!priorityStr) return;
        const priority = parseInt(priorityStr) || 10;

        try {
            await API.dns.addMX(domain, server, priority);
            alert('添加成功');
            renderMailPage();
        } catch (e) {
            alert('添加失败: ' + e.message);
        }
    }

    async function deleteMX(domain, server) {
        if (!confirm(`确定删除 ${domain} 的 MX 记录 ${server} 吗？`)) return;
        try {
            await API.dns.deleteMX(domain, server);
            alert('删除成功');
            renderMailPage();
        } catch (e) {
            alert('删除失败: ' + e.message);
        }
    }

    async function showAddAModal() {
        const domain = prompt('请输入主机名（如 mail.example.dn42）:');
        if (!domain) return;
        const ip = prompt('请输入 IP 地址:');
        if (!ip) return;

        try {
            await API.dns.addA(domain, ip);
            alert('添加成功');
            renderMailPage();
        } catch (e) {
            alert('添加失败: ' + e.message);
        }
    }

    async function deleteA(domain) {
        if (!confirm(`确定删除 ${domain} 的 A 记录吗？`)) return;
        try {
            await API.dns.deleteA(domain);
            alert('删除成功');
            renderMailPage();
        } catch (e) {
            alert('删除失败: ' + e.message);
        }
    }

    async function showAddSPFModal() {
        const domain = prompt('请输入域名（如 example.dn42）:');
        if (!domain) return;
        const value = prompt('请输入 SPF 值（如 v=spf1 mx ~all）:', 'v=spf1 mx ~all');
        if (!value) return;

        try {
            await API.dns.addSPF(domain, value);
            alert('添加成功');
            renderMailPage();
        } catch (e) {
            alert('添加失败: ' + e.message);
        }
    }

    async function showAddDKIMModal() {
        const selector = prompt('请输入 DKIM 选择器（如 default）:', 'default');
        if (!selector) return;
        const domain = prompt('请输入域名（如 example.dn42）:');
        if (!domain) return;
        const value = prompt('请输入 DKIM 公钥值（v=DKIM1; k=rsa; p=...）:');
        if (!value) return;

        try {
            await API.dns.addDKIM(selector, domain, value);
            alert('添加成功');
            renderMailPage();
        } catch (e) {
            alert('添加失败: ' + e.message);
        }
    }

    async function showAddDMARCModal() {
        const domain = prompt('请输入域名（如 example.dn42）:');
        if (!domain) return;
        const value = prompt('请输入 DMARC 值（如 v=DMARC1; p=none; rua=mailto:dmarc@example.dn42）:',
            'v=DMARC1; p=none');
        if (!value) return;

        try {
            await API.dns.addDMARC(domain, value);
            alert('添加成功');
            renderMailPage();
        } catch (e) {
            alert('添加失败: ' + e.message);
        }
    }

    // ============ 系统设置页 ============
    function renderSettingsPage() {
        const content = document.getElementById('pageContent');

        content.innerHTML = `
            <div class="card" style="max-width: 500px;">
                <div class="card-header">
                    <h3 class="card-title">修改密码</h3>
                </div>
                <form id="changePasswordForm" onsubmit="App.handleChangePassword(event)">
                    <div style="margin-bottom: 16px;">
                        <label style="display:block; margin-bottom: 6px; font-weight: 500;">旧密码</label>
                        <input type="password" id="oldPassword" class="search-box" style="width: 100%;" required>
                    </div>
                    <div style="margin-bottom: 16px;">
                        <label style="display:block; margin-bottom: 6px; font-weight: 500;">新密码</label>
                        <input type="password" id="newPassword" class="search-box" style="width: 100%;" required>
                    </div>
                    <div style="margin-bottom: 20px;">
                        <label style="display:block; margin-bottom: 6px; font-weight: 500;">确认新密码</label>
                        <input type="password" id="confirmPassword" class="search-box" style="width: 100%;" required>
                    </div>
                    <div id="passwordResult"></div>
                    <button type="submit" class="btn btn-primary">修改密码</button>
                </form>
            </div>

            <div class="card" style="max-width: 500px; margin-top: 24px;">
                <div class="card-header">
                    <h3 class="card-title">关于</h3>
                </div>
                <table>
                    <tr>
                        <td style="width: 30%; font-weight: 500;">版本</td>
                        <td>v2.2.0</td>
                    </tr>
                    <tr>
                        <td style="font-weight: 500;">当前用户</td>
                        <td>${Auth.getUsername() || 'admin'}</td>
                    </tr>
                </table>
            </div>
        `;
    }

    async function handleChangePassword(e) {
        e.preventDefault();
        const oldPwd = document.getElementById('oldPassword').value;
        const newPwd = document.getElementById('newPassword').value;
        const confirmPwd = document.getElementById('confirmPassword').value;
        const resultEl = document.getElementById('passwordResult');

        if (newPwd !== confirmPwd) {
            resultEl.innerHTML = '<div class="error-message">两次输入的新密码不一致</div>';
            return;
        }

        try {
            await API.changePassword(oldPwd, newPwd);
            resultEl.innerHTML = '<div style="padding:10px 14px;background:var(--success-light);color:var(--success);border-radius:8px;margin-bottom:16px;">密码修改成功</div>';
            document.getElementById('changePasswordForm').reset();
        } catch (err) {
            resultEl.innerHTML = `<div class="error-message">${err.message}</div>`;
        }
    }

    // ============ 公共操作 ============
    async function flushCache() {
        if (!confirm('确定要清空所有缓存吗？')) return;

        try {
            await API.dns.flushCache();
            alert('缓存已清空');
            if (currentPage === 'dashboard' || currentPage === 'cache') {
                loadPage(currentPage);
            }
        } catch (e) {
            alert('清空失败: ' + e.message);
        }
    }

    function refreshPage() {
        loadPage(currentPage);
    }

    function logout() {
        if (!confirm('确定要退出登录吗？')) return;
        Auth.clearTokens();
        window.location.href = '/login';
    }

    // 自动刷新
    function startAutoRefresh() {
        if (refreshInterval) clearInterval(refreshInterval);
        refreshInterval = setInterval(() => {
            updateHealthBadge();
        }, 10000); // 10秒更新一次健康状态

        // 立即更新一次
        updateHealthBadge();
    }

    return {
        init,
        loadPage,
        flushCache,
        refreshPage,
        filterRdns,
        loadUpstreamsGroup,
        handleChangePassword,
        switchMailTab,
        showAddMXModal,
        deleteMX,
        showAddAModal,
        deleteA,
        showAddSPFModal,
        showAddDKIMModal,
        showAddDMARCModal,
        logout
    };
})();

// 启动应用
document.addEventListener('DOMContentLoaded', function() {
    App.init();
});
