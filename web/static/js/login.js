/**
 * 登录页逻辑
 */
document.addEventListener('DOMContentLoaded', function() {
    // 如果已经登录，直接跳转到首页
    if (Auth.isLoggedIn() || Auth.hasRefreshToken()) {
        Auth.ensureValidToken().then(valid => {
            if (valid) {
                window.location.href = '/';
            }
        });
    }

    const form = document.getElementById('loginForm');
    const errorEl = document.getElementById('loginError');

    form.addEventListener('submit', async function(e) {
        e.preventDefault();

        const username = document.getElementById('username').value.trim();
        const password = document.getElementById('password').value;
        const remember = document.getElementById('remember').checked;

        errorEl.style.display = 'none';

        try {
            const data = await API.login(username, password);

            if (remember) {
                Auth.setTokens(data.access_token, data.refresh_token, data.username);
            } else {
                // 不记住的话用 sessionStorage
                sessionStorage.setItem('access_token', data.access_token);
                sessionStorage.setItem('refresh_token', data.refresh_token);
                sessionStorage.setItem('username', data.username);
            }

            // 登录成功，跳转到首页
            window.location.href = '/';
        } catch (err) {
            errorEl.textContent = err.message;
            errorEl.style.display = 'block';
        }
    });
});
