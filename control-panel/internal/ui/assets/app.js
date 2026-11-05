/* AGinX Control Panel — SPA tanpa build step.
   Semua string dirender lewat esc() agar config/log tidak menjadi vektor XSS. */

const app = document.getElementById('app');
const modalRoot = document.getElementById('modal-root');
const toastRoot = document.getElementById('toasts');

const state = {
  me: null,
  csrf: null,
  version: '',
  sites: [],
  overview: null,
  config: null,
};

let renderToken = 0;

const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (ch) => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}[ch]));

async function api(path, options = {}) {
  const { method = 'GET', body } = options;
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && state.csrf) headers['X-CSRF-Token'] = state.csrf;

  let response;
  try {
    response = await fetch('/api/v1' + path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (cause) {
    const error = new Error('Tidak dapat menghubungi panel');
    error.cause = cause;
    throw error;
  }

  if (response.status === 401 && !path.startsWith('/auth/login')) {
    state.me = null;
    state.csrf = null;
    if (location.pathname !== '/login') navigate('/login', true);
    throw new Error('Sesi berakhir. Silakan login kembali.');
  }

  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    const error = new Error(payload?.error?.message || `Permintaan gagal (${response.status})`);
    error.code = payload?.error?.code;
    error.details = payload?.error?.details;
    error.status = response.status;
    throw error;
  }
  return payload ? payload.data : null;
}

function navigate(path, replace = false) {
  if (replace) history.replaceState({}, '', path);
  else history.pushState({}, '', path);
  render();
}

window.addEventListener('popstate', () => render());
document.addEventListener('click', (event) => {
  const link = event.target.closest('a[data-link]');
  if (link) {
    event.preventDefault();
    navigate(link.getAttribute('href'));
    return;
  }
  const actionEl = event.target.closest('[data-action]');
  if (!actionEl) return;
  const action = actionEl.dataset.action;
  if (action === 'logout') doLogout();
  if (action === 'toggle-sidebar') document.getElementById('layout')?.classList.toggle('sidebar-open');
  if (action === 'close-modal') closeModal();
  if (action === 'create-site') openSiteModal();
  if (action === 'delete-site') openDeleteModal(actionEl.dataset.id, actionEl.dataset.slug);
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') closeModal();
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault();
    navigate('/sites');
  }
});

/* ---------- Format helpers ---------- */
function bytes(value) {
  const n = Number(value) || 0;
  if (n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
function pct(used, total) {
  if (!total) return 0;
  return Math.min(100, Math.round((used / total) * 100));
}
function duration(seconds) {
  const s = Math.max(0, Number(seconds) || 0);
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const mins = Math.floor((s % 3600) / 60);
  if (days) return `${days} hari ${hours} jam`;
  if (hours) return `${hours} jam ${mins} menit`;
  return `${mins} menit`;
}
function dateTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });
}
function timeAgo(value) {
  if (!value) return '—';
  const diff = Date.now() - new Date(value).getTime();
  if (Number.isNaN(diff)) return '—';
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'baru saja';
  if (mins < 60) return `${mins} menit lalu`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} jam lalu`;
  return `${Math.floor(hours / 24)} hari lalu`;
}
const meterClass = (value) => (value >= 90 ? 'bad' : value >= 75 ? 'warn' : 'ok');
const statusBadge = (status) => {
  const map = {
    active: ['green', 'Aktif'], published: ['green', 'Aktif'],
    draft: ['blue', 'Draft'], draft_invalid: ['amber', 'Draft invalid'],
    superseded: ['', 'Digantikan'], disabled: ['amber', 'Nonaktif'], error: ['red', 'Error'],
  };
  const [cls, label] = map[status] || ['', status];
  return `<span class="badge ${cls}">${esc(label)}</span>`;
};
const initials = (name) => String(name || '?').trim().split(/\s+/).map((part) => part[0]).slice(0, 2).join('').toUpperCase();

/* ---------- Toast & modal ---------- */
function toast(message, kind = 'ok') {
  const el = document.createElement('div');
  el.className = `toast ${kind === 'err' ? 'err' : 'ok'}`;
  el.innerHTML = `<span>${esc(message)}</span><button class="x" aria-label="Tutup">×</button>`;
  el.querySelector('.x').addEventListener('click', () => el.remove());
  toastRoot.appendChild(el);
  setTimeout(() => el.remove(), kind === 'err' ? 8000 : 4500);
}
function closeModal() {
  modalRoot.innerHTML = '';
}
function openModal(html) {
  modalRoot.innerHTML = `<div class="modal-backdrop" data-backdrop>${html}</div>`;
  const backdrop = modalRoot.querySelector('[data-backdrop]');
  backdrop.addEventListener('click', (event) => {
    if (event.target === backdrop) closeModal();
  });
}

/* ---------- Auth ---------- */
function authLayout({ eyebrow, title, subtitle, body, footer }) {
  return `
  <div class="auth">
    <section class="auth-brand">
      <div class="auth-logo"><span class="mark">AG</span> AGinX Control Panel</div>
      <div class="auth-headline">
        <h1>Kontrol penuh.<br>Tanpa lapisan tambahan.</h1>
        <p>Panel native untuk engine AGinX. Kelola situs, .ginxaccess, dan operasi server dari satu tempat — tanpa Nginx, Apache, atau Docker.</p>
        <ul class="auth-points">
          <li><span class="dot">✓</span><span>Config per direktori divalidasi engine sebelum aktif</span></li>
          <li><span class="dot">✓</span><span>Publish atomik dengan revisi dan rollback</span></li>
          <li><span class="dot">✓</span><span>Audit setiap perubahan, tanpa eksekusi shell</span></li>
        </ul>
      </div>
      <div class="auth-foot">AGinX · native web server · ${esc(state.version || 'v0.1')}</div>
    </section>
    <section class="auth-panel">
      <div class="auth-card">
        <div class="small muted" style="margin-bottom:10px">${esc(eyebrow)}</div>
        <h2>${esc(title)}</h2>
        <p class="sub">${esc(subtitle)}</p>
        ${body}
        <div class="auth-note">${footer}</div>
      </div>
    </section>
  </div>`;
}

function renderLogin(message = '') {
  app.innerHTML = authLayout({
    eyebrow: 'Masuk ke panel',
    title: 'Selamat datang kembali',
    subtitle: 'Gunakan akun owner panel Anda.',
    body: `
      <form class="form" id="login-form">
        <div class="field">
          <label for="login-user">Username atau email</label>
          <input class="input" id="login-user" name="username" autocomplete="username" required autofocus>
        </div>
        <div class="field">
          <label for="login-pass">Password</label>
          <input class="input" id="login-pass" name="password" type="password" autocomplete="current-password" required>
        </div>
        <div class="form-error" id="login-error" style="display:${message ? 'block' : 'none'}">${esc(message)}</div>
        <button class="btn primary" type="submit" style="width:100%">Masuk</button>
      </form>`,
    footer: 'Panel listen di localhost secara default. Akses melalui SSH tunnel untuk produksi.',
  });
  document.getElementById('login-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type=submit]');
    const errorBox = document.getElementById('login-error');
    button.disabled = true;
    button.textContent = 'Memeriksa…';
    errorBox.style.display = 'none';
    try {
      const response = await api('/auth/login', {
        method: 'POST',
        body: {
          username: form.username.value.trim(),
          password: form.password.value,
        },
      });
      state.me = response.user;
      state.csrf = response.csrf;
      toast(`Selamat datang, ${response.user.display_name}`);
      navigate('/');
    } catch (error) {
      errorBox.textContent = error.message;
      errorBox.style.display = 'block';
      form.password.value = '';
      form.password.focus();
    } finally {
      button.disabled = false;
      button.textContent = 'Masuk';
    }
  });
}

function renderSetup(initial = false) {
  app.innerHTML = authLayout({
    eyebrow: initial ? 'Setup pertama' : 'Setup panel',
    title: 'Buat akun owner',
    subtitle: 'Setup token tercetak di terminal dan tersimpan di data/setup.token.',
    body: `
      <form class="form" id="setup-form">
        <div class="field">
          <label for="setup-token">Setup token</label>
          <input class="input mono" id="setup-token" name="token" autocomplete="off" required autofocus>
          <div class="help">Salin dari output aginx-panel atau file setup.token.</div>
        </div>
        <div class="field">
          <label for="setup-user">Username</label>
          <input class="input" id="setup-user" name="username" autocomplete="username" required>
        </div>
        <div class="field">
          <label for="setup-email">Email</label>
          <input class="input" id="setup-email" name="email" type="email" autocomplete="email" required>
        </div>
        <div class="field">
          <label for="setup-pass">Password</label>
          <input class="input" id="setup-pass" name="password" type="password" autocomplete="new-password" minlength="12" required>
          <div class="help">Minimal 12 karakter, bukan password umum, tidak memuat username/email.</div>
        </div>
        <div class="form-error" id="setup-error" style="display:none"></div>
        <button class="btn primary" type="submit" style="width:100%">Buat akun & masuk</button>
      </form>`,
    footer: 'Tidak ada email reset pada slice ini. Gunakan CLI panel untuk reset password.',
  });
  document.getElementById('setup-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type=submit]');
    const errorBox = document.getElementById('setup-error');
    button.disabled = true;
    button.textContent = 'Menyiapkan…';
    errorBox.style.display = 'none';
    try {
      const response = await api('/setup', {
        method: 'POST',
        body: {
          token: form.token.value.trim(),
          username: form.username.value.trim(),
          email: form.email.value.trim(),
          password: form.password.value,
        },
      });
      state.me = response.user;
      state.csrf = response.csrf;
      toast('Akun owner dibuat');
      navigate('/');
    } catch (error) {
      errorBox.textContent = error.message;
      errorBox.style.display = 'block';
    } finally {
      button.disabled = false;
      button.textContent = 'Buat akun & masuk';
    }
  });
}

async function doLogout() {
  try {
    await api('/auth/logout', { method: 'POST' });
  } catch (error) {
    // Sesi mungkin sudah tidak valid; tetap bersihkan state lokal.
  }
  state.me = null;
  state.csrf = null;
  navigate('/login', true);
}

/* ---------- Shell ---------- */
function shell({ title, subtitle, actions = '', body, active = '' }) {
  const groups = [
    {
      label: 'Utama',
      items: [{ href: '/', label: 'Dashboard', icon: '◉', key: 'dashboard' }],
    },
    {
      label: 'Situs',
      items: [
        { href: '/sites', label: 'Websites', icon: '▣', key: 'sites' },
        { href: '/config', label: 'Config Studio', icon: '⌘', key: 'config' },
      ],
    },
    {
      label: 'Sistem',
      items: [
        { href: '/audit', label: 'Log Audit', icon: '☰', key: 'audit' },
        { href: '/settings', label: 'Settings', icon: '⚙', key: 'settings' },
      ],
    },
  ];
  const nav = groups.map((group) => `
    <div class="nav-group">
      <div class="nav-label">${group.label}</div>
      ${group.items.map((item) => `
        <a class="nav-item ${active === item.key ? 'active' : ''}" href="${item.href}" data-link>
          <span class="nav-icon">${item.icon}</span>
          <span>${item.label}</span>
          ${item.key === 'sites' && state.sites.length ? `<span class="nav-badge">${state.sites.length}</span>` : ''}
        </a>`).join('')}
    </div>`).join('');

  const user = state.me || { display_name: 'Pengguna', username: '', role: '' };
  app.innerHTML = `
  <div class="layout" id="layout">
    <aside class="sidebar">
      <div class="brand">
        <span class="mark">AG</span>
        <span class="brand-text"><strong>AGinX</strong><span>Control Panel</span></span>
      </div>
      <nav>${nav}</nav>
      <div class="sidebar-foot">
        <strong>v${esc(state.version || '0.1.0')}</strong><br>
        Native engine · ${esc(location.host)}
      </div>
    </aside>
    <div class="main">
      <header class="topbar">
        <button class="icon-btn menu-toggle" data-action="toggle-sidebar" aria-label="Buka menu">☰</button>
        <div class="crumb"><span>AGinX</span><span>/</span><strong>${esc(title)}</strong></div>
        <div class="spacer"></div>
        <span class="badge green"><span class="dot-live"></span> Panel aktif</span>
        <button class="btn ghost sm" data-action="logout">Keluar</button>
        <div class="user-chip">
          <div class="avatar">${esc(initials(user.display_name || user.username))}</div>
          <div class="user-meta"><strong>${esc(user.display_name || user.username)}</strong><span>${esc(user.role || '')}</span></div>
        </div>
      </header>
      <main class="content">
        <div class="page-head">
          <div><h1>${esc(title)}</h1><p>${esc(subtitle)}</p></div>
          <div class="page-actions">${actions}</div>
        </div>
        ${body}
      </main>
    </div>
  </div>`;
}

function loadingShell(title, subtitle) {
  shell({
    title,
    subtitle,
    body: `<div class="grid stats">${'<div class="card card-pad"><div class="skeleton" style="height:70px"></div></div>'.repeat(4)}</div>`,
  });
}

function errorBody(error) {
  return `<div class="card card-pad"><div class="alert bad"><div><strong>Tidak dapat memuat data.</strong><br>${esc(error.message || error)}</div></div>
    <div class="row" style="margin-top:14px"><button class="btn ghost" data-action="reload">Coba lagi</button></div></div>`;
}

document.addEventListener('click', (event) => {
  if (event.target.closest('[data-action="reload"]')) render();
});

/* ---------- Dashboard ---------- */
async function renderDashboard(token) {
  loadingShell('Dashboard', 'Ringkasan server dan engine AGinX');
  try {
    const [overview, sitesPayload] = await Promise.all([api('/overview'), api('/sites')]);
    if (token !== renderToken) return;
    state.overview = overview;
    state.version = overview.version;
    state.sites = sitesPayload.map((item) => item.site);
    const system = overview.system;
    const cpu = Math.round(system.cpu_percent || 0);
    const ram = pct(system.mem_used, system.mem_total);
    const disk = pct(system.disk_used, system.disk_total);
    const engine = overview.engine;

    const siteRows = sitesPayload.slice(0, 5).map(({ site, configs }) => `
      <tr>
        <td><a class="name" href="/sites/${encodeURIComponent(site.id)}" data-link>${esc(site.name)}</a><div class="small muted">${esc(site.docroot)}</div></td>
        <td class="mono">${esc(site.domain)}</td>
        <td>${statusBadge(site.status)}</td>
        <td>${configs.length} file</td>
        <td class="actions"><a class="btn ghost sm" href="/sites/${encodeURIComponent(site.id)}/config" data-link>Config</a></td>
      </tr>`).join('') || `<tr class="empty-row"><td colspan="5">Belum ada situs. Mulai dengan menambah situs pertama.</td></tr>`;

    const auditRows = (overview.recent_audit || []).map((event) => `
      <tr>
        <td style="width:170px" class="small muted">${esc(timeAgo(event.ts))}</td>
        <td><strong>${esc(event.actor_name || 'sistem')}</strong></td>
        <td><span class="mono-chip">${esc(event.action)}</span></td>
        <td class="small">${esc(event.target || '—')}</td>
        <td>${event.result === 'success' ? '<span class="badge green">OK</span>' : `<span class="badge red">${esc(event.result)}</span>`}</td>
      </tr>`).join('') || `<tr class="empty-row"><td colspan="5">Belum ada aktivitas.</td></tr>`;

    shell({
      title: 'Dashboard',
      subtitle: 'Ringkasan server dan engine AGinX',
      active: 'dashboard',
      actions: `<button class="btn primary" data-action="create-site">+ Tambah situs</button>`,
      body: `
      <div class="grid stats">
        <div class="card metric"><div class="label">CPU <span>${system.num_cpu} core</span></div><div class="value">${cpu}%</div><div class="meter ${meterClass(cpu)}"><span style="width:${cpu}%"></span></div><div class="sub">load ${esc(system.load1?.toFixed(2) ?? '0.00')}</div></div>
        <div class="card metric"><div class="label">RAM <span>${bytes(system.mem_total)}</span></div><div class="value">${ram}%</div><div class="meter ${meterClass(ram)}"><span style="width:${ram}%"></span></div><div class="sub">${bytes(system.mem_used)} terpakai</div></div>
        <div class="card metric"><div class="label">Disk <span>${bytes(system.disk_total)}</span></div><div class="value">${disk}%</div><div class="meter ${meterClass(disk)}"><span style="width:${disk}%"></span></div><div class="sub">${bytes(system.disk_used)} terpakai</div></div>
        <div class="card metric"><div class="label">Uptime <span>panel ${duration(system.panel_uptime)}</span></div><div class="value" style="font-size:22px">${duration(system.uptime)}</div><div class="meter"><span style="width:100%;background:#cbd5e1"></span></div><div class="sub">waktu server ${esc(dateTime(system.server_time))}</div></div>
      </div>

      <div class="grid two-col" style="margin-top:16px">
        <div class="card">
          <div class="card-head"><h2>AGinX Engine</h2>${engine.state === 'configured' ? '<span class="badge green">terhubung</span>' : '<span class="badge amber">degraded</span>'}</div>
          <div class="card-body">
            <dl class="kv">
              <dt>Status</dt><dd>${engine.state === 'configured' ? 'PID file terkonfigurasi, reload via SIGHUP' : 'Belum terhubung ke engine (PID file kosong)'}</dd>
              <dt>Versi panel</dt><dd class="mono">${esc(overview.version)}</dd>
              <dt>Basis docroot</dt><dd class="mono">${esc(overview.www_base)}</dd>
              <dt>Publish</dt><dd>Validasi engine → tulis atomik → ${engine.state === 'configured' ? 'SIGHUP' : 'reload dilewati'}</dd>
            </dl>
            ${engine.state !== 'configured' ? `<div class="alert warn" style="margin-top:14px"><div>Jalankan panel dengan <span class="mono">-engine-pid-file</span> agar publish memicu reload engine. Tanpa itu, file tetap ditulis dan direvisi.</div></div>` : ''}
          </div>
        </div>
        <div class="card">
          <div class="card-head"><h2>Situs</h2><a href="/sites" data-link class="small">Lihat semua</a></div>
          <div class="card-body">
            <div class="row between" style="margin-bottom:12px"><div><div class="value" style="font-size:26px;font-weight:700">${overview.sites.total}</div><div class="small muted">total situs</div></div><div><div class="value" style="font-size:26px;font-weight:700;color:var(--success)">${overview.sites.active}</div><div class="small muted">aktif</div></div></div>
            <a class="btn ghost" href="/config" data-link style="width:100%">Buka Config Studio</a>
          </div>
        </div>
      </div>

      <div class="card" style="margin-top:16px">
        <div class="card-head"><h2>Situs terbaru</h2><a href="/sites" data-link class="small">Kelola</a></div>
        <div class="table-wrap"><table class="table"><thead><tr><th>Nama</th><th>Domain</th><th>Status</th><th>Config</th><th></th></tr></thead><tbody>${siteRows}</tbody></table></div>
      </div>

      <div class="card" style="margin-top:16px">
        <div class="card-head"><h2>Aktivitas terbaru</h2><a href="/audit" data-link class="small">Semua audit</a></div>
        <div class="table-wrap"><table class="table"><thead><tr><th>Waktu</th><th>Aktor</th><th>Aksi</th><th>Target</th><th>Hasil</th></tr></thead><tbody>${auditRows}</tbody></table></div>
      </div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Dashboard', subtitle: 'Ringkasan server', body: errorBody(error) });
  }
}

/* ---------- Sites ---------- */
async function renderSites(token) {
  loadingShell('Websites', 'Daftar situs yang dikelola panel');
  try {
    const payload = await api('/sites');
    if (token !== renderToken) return;
    state.sites = payload.map((item) => item.site);
    const rows = payload.map(({ site, configs }) => `
      <tr>
        <td><a class="name" href="/sites/${encodeURIComponent(site.id)}" data-link>${esc(site.name)}</a><div class="small muted mono">${esc(site.slug)}</div></td>
        <td class="mono">${esc(site.domain)}</td>
        <td>${statusBadge(site.status)}</td>
        <td class="mono small">${esc(site.docroot)}</td>
        <td>${configs.length} file</td>
        <td class="actions">
          <a class="btn ghost sm" href="/sites/${encodeURIComponent(site.id)}/config" data-link>Config</a>
          <a class="btn ghost sm" href="/sites/${encodeURIComponent(site.id)}" data-link>Detail</a>
        </td>
      </tr>`).join('');

    shell({
      title: 'Websites',
      subtitle: 'Situs statis dan docroot yang dikelola AGinX',
      active: 'sites',
      actions: `<button class="btn primary" data-action="create-site">+ Tambah situs</button>`,
      body: `
      <div class="card">
        <div class="card-head"><h2>${payload.length} situs</h2><span class="hint">Docroot default di bawah ${esc(state.overview?.www_base || 'data/www')}</span></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>Nama</th><th>Domain</th><th>Status</th><th>Docroot</th><th>Config</th><th></th></tr></thead>
            <tbody>${rows || `<tr class="empty-row"><td colspan="6"><div class="empty"><div class="glyph">▣</div><h3>Belum ada situs</h3><p>Buat situs pertama; panel menyiapkan docroot, index, dan .ginxaccess awal.</p><button class="btn primary" data-action="create-site">Tambah situs</button></div></td></tr>`}</tbody>
          </table>
        </div>
      </div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Websites', subtitle: '', body: errorBody(error) });
  }
}

function openSiteModal() {
  openModal(`
    <div class="modal-head">
      <div><h2>Tambah situs</h2><p>Docroot dan konfigurasi awal dibuat otomatis.</p></div>
      <button class="icon-btn" data-action="close-modal" aria-label="Tutup">×</button>
    </div>
    <form id="site-form">
      <div class="modal-body form">
        <div class="field"><label for="site-name">Nama situs</label><input class="input" id="site-name" name="name" placeholder="Contoh: Company Profile" required maxlength="80"></div>
        <div class="field"><label for="site-slug">Slug</label><input class="input mono" id="site-slug" name="slug" placeholder="company-profile" required pattern="[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?"><div class="help">Huruf kecil, angka, tanda hubung. Menentukan docroot.</div></div>
        <div class="field"><label for="site-domain">Domain</label><input class="input mono" id="site-domain" name="domain" placeholder="example.com" required><div class="help">FQDN tanpa https:// atau path.</div></div>
        <div class="form-error" id="site-error" style="display:none"></div>
      </div>
      <div class="modal-foot"><button type="button" class="btn ghost" data-action="close-modal">Batal</button><button type="submit" class="btn primary">Buat situs</button></div>
    </form>`);	const form = document.getElementById('site-form');
	const name = document.getElementById('site-name');
	const slug = document.getElementById('site-slug');
	const domain = document.getElementById('site-domain');
	let slugEdited = false;
  name.addEventListener('input', () => {
    if (slugEdited) return;
    slug.value = name.value.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63);
  });
  slug.addEventListener('input', () => { slugEdited = true; });

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = form.querySelector('button[type=submit]');
    const errorBox = document.getElementById('site-error');
    button.disabled = true;
    button.textContent = 'Membuat…';
    errorBox.style.display = 'none';
    try {
      const site = await api('/sites', {
        method: 'POST',		body: { name: name.value.trim(), slug: slug.value.trim(), domain: domain.value.trim() },
      });
      closeModal();
      toast(`Situs ${site.name} dibuat`);
      navigate(`/sites/${encodeURIComponent(site.id)}`);
    } catch (error) {
      errorBox.textContent = error.message;
      errorBox.style.display = 'block';
      button.disabled = false;
      button.textContent = 'Buat situs';
    }
  });
}

function openDeleteModal(id, slug) {
  openModal(`
    <div class="modal-head">
      <div><h2>Hapus situs</h2><p>Docroot dipindahkan ke <span class="mono">.trash</span>, metadata dihapus.</p></div>
      <button class="icon-btn" data-action="close-modal" aria-label="Tutup">×</button>
    </div>
    <form id="delete-form">
      <div class="modal-body form">
        <div class="alert bad"><div>Tindakan ini menghentikan pengelolaan situs. File tidak langsung dihapus, tetapi tidak lagi tampil di panel.</div></div>
        <div class="field"><label for="delete-confirm">Ketik <span class="mono">${esc(slug)}</span> untuk konfirmasi</label><input class="input mono" id="delete-confirm" name="confirm" autocomplete="off" required></div>
        <div class="form-error" id="delete-error" style="display:none"></div>
      </div>
      <div class="modal-foot"><button type="button" class="btn ghost" data-action="close-modal">Batal</button><button type="submit" class="btn danger">Hapus situs</button></div>
    </form>`);
  const form = document.getElementById('delete-form');
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = form.querySelector('button[type=submit]');
    const errorBox = document.getElementById('delete-error');
    button.disabled = true;
    try {
      await api(`/sites/${encodeURIComponent(id)}`, { method: 'DELETE', body: { confirm: document.getElementById('delete-confirm').value } });
      closeModal();
      toast('Situs dihapus');
      navigate('/sites');
    } catch (error) {
      errorBox.textContent = error.message;
      errorBox.style.display = 'block';
      button.disabled = false;
    }
  });
}

async function renderSitePage(siteId, token) {
  loadingShell('Detail situs', 'Memuat data situs');
  try {
    const { site, configs } = await api(`/sites/${encodeURIComponent(siteId)}`);
    if (token !== renderToken) return;
    shell({
      title: site.name,
      subtitle: site.domain,
      active: 'sites',
      actions: `
        <a class="btn ghost" href="/sites" data-link>Kembali</a>
        <a class="btn primary" href="/sites/${encodeURIComponent(site.id)}/config" data-link>Buka Config Studio</a>`,
      body: `
      <div class="grid two-col">
        <div class="card">
          <div class="card-head"><h2>Informasi situs</h2>${statusBadge(site.status)}</div>
          <div class="card-body">
            <dl class="kv">
              <dt>Nama</dt><dd>${esc(site.name)}</dd>
              <dt>Slug</dt><dd class="mono">${esc(site.slug)}</dd>
              <dt>Domain</dt><dd class="mono">${esc(site.domain)}</dd>
              <dt>Docroot</dt><dd class="mono">${esc(site.docroot)}</dd>
              <dt>Dibuat</dt><dd>${esc(dateTime(site.created_at))}</dd>
            </dl>
            <div class="divider"></div>
            <div class="row between">
              <div><strong>Zona berbahaya</strong><div class="small muted">Docroot dipindahkan ke trash, bukan dihapus permanen.</div></div>
              <button class="btn danger sm" data-action="delete-site" data-id="${esc(site.id)}" data-slug="${esc(site.slug)}">Hapus situs</button>
            </div>
          </div>
        </div>
        <div class="card">
          <div class="card-head"><h2>Konfigurasi</h2><span class="hint">${configs.length} file</span></div>
          <div class="side-files">
            ${configs.map((path) => `<a class="file-item" href="/sites/${encodeURIComponent(site.id)}/config?path=${encodeURIComponent(path)}" data-link><span>⌘</span> ${esc(path === '/' ? '/.ginxaccess' : path + '/.ginxaccess')}</a>`).join('') || '<div class="empty"><p>Belum ada file konfigurasi.</p></div>'}
          </div>
          <div class="card-body" style="border-top:1px solid var(--border)"><a class="btn ghost" href="/sites/${encodeURIComponent(site.id)}/config" data-link style="width:100%">Kelola konfigurasi</a></div>
        </div>
      </div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Detail situs', subtitle: '', body: errorBody(error) });
  }
}

/* ---------- Config Studio ---------- */
async function renderConfigStudio(token) {
  loadingShell('Config Studio', '.ginxaccess per direktori');
  try {
    const payload = await api('/sites');
    if (token !== renderToken) return;
    state.sites = payload.map((item) => item.site);
    const cards = payload.map(({ site, configs }) => `
      <div class="card">
        <div class="card-body">
          <div class="row between" style="align-items:flex-start">
            <div><div style="font-weight:700;font-size:15px">${esc(site.name)}</div><div class="mono small muted">${esc(site.domain)}</div></div>
            ${statusBadge(site.status)}
          </div>
          <div class="divider"></div>
          <div class="small muted">${configs.length} file .ginxaccess</div>
          <div class="tag-list" style="margin-top:8px">${configs.slice(0, 4).map((path) => `<span class="mono-chip">${esc(path)}</span>`).join('') || '<span class="small muted">Belum ada konfigurasi</span>'}</div>
          <a class="btn ghost sm" style="margin-top:14px" href="/sites/${encodeURIComponent(site.id)}/config" data-link>Buka editor</a>
        </div>
      </div>`).join('');
    shell({
      title: 'Config Studio',
      subtitle: 'Edit, validasi, dan publikasikan .ginxaccess dengan aman',
      active: 'config',
      body: `<div class="grid" style="grid-template-columns:repeat(auto-fill,minmax(300px,1fr))">${cards || '<div class="card card-pad"><div class="empty"><div class="glyph">⌘</div><h3>Belum ada situs</h3><p>Config Studio aktif setelah situs dibuat.</p></div></div>'}</div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Config Studio', subtitle: '', body: errorBody(error) });
  }
}

async function renderConfigPage(siteId, token) {
  loadingShell('Config Studio', 'Memuat konfigurasi');
  try {
    const { site, configs } = await api(`/sites/${encodeURIComponent(siteId)}`);
    if (token !== renderToken) return;
    const query = new URLSearchParams(location.search);
    const path = query.get('path') || '/';
    const config = await api(`/sites/${encodeURIComponent(siteId)}/config?path=${encodeURIComponent(path)}`);
    const revisions = await api(`/sites/${encodeURIComponent(siteId)}/config/revisions?path=${encodeURIComponent(path)}`);
    if (token !== renderToken) return;

    state.config = { site, configs, path, content: config.content, revisions };

    const allPaths = Array.from(new Set(['/', ...configs, path]));
    shell({
      title: 'Config Studio',
      subtitle: `${site.name} · ${site.domain}`,
      active: 'config',
      actions: `<a class="btn ghost" href="/sites/${encodeURIComponent(site.id)}" data-link>Detail situs</a>
        <a class="btn ghost" href="/sites" data-link>Websites</a>`,
      body: `
      <div class="config-layout">
        <div class="card editor-panel">
          <div class="editor-toolbar">
            <input class="path" id="path-input" value="${esc(path)}" aria-label="Direktori konfigurasi" spellcheck="false">
            <button class="btn ghost sm" id="btn-open">Buka</button>
            <button class="btn ghost sm" id="btn-validate">Validasi</button>
            <button class="btn ghost sm" id="btn-draft">Simpan draft</button>
            <button class="btn primary sm" id="btn-publish">Publish</button>
          </div>
          <div class="code-editor">
            <pre class="gutter" id="gutter" aria-hidden="true">1</pre>
            <textarea class="code" id="editor" spellcheck="false" aria-label="Isi .ginxaccess"></textarea>
          </div>
          <div class="editor-foot"><span id="editor-count">0 baris · 0 / 65536 byte</span><span>${esc(path)}/.ginxaccess</span></div>
          <div class="issues" id="issues"><div class="small muted">Belum divalidasi. Publish selalu memvalidasi ulang di server.</div></div>
        </div>

        <div class="stack">
          <div class="card">
            <div class="card-head"><h2>File konfigurasi</h2><span class="hint">${allPaths.length}</span></div>
            <div class="side-files">
              ${allPaths.map((item) => `<a class="file-item ${item === path ? 'active' : ''}" href="/sites/${encodeURIComponent(site.id)}/config?path=${encodeURIComponent(item)}" data-link><span>⌘</span> ${esc(item === '/' ? '/.ginxaccess' : item + '/.ginxaccess')}</a>`).join('')}
            </div>
          </div>
          <div class="card">
            <div class="card-head"><h2>Riwayat</h2><span class="hint">${revisions.length}</span></div>
            <div class="revision-list" id="revisions">${revisionListHTML(revisions)}</div>
          </div>
          <div class="card card-pad">
            <div class="small muted">Alur publish</div>
            <ol class="small" style="margin:8px 0 0;padding-left:18px;color:#475569;line-height:1.8">
              <li>Parser engine memvalidasi isi</li>
              <li>File ditulis atomik (temp → rename)</li>
              <li>Revisi dicatat + audit</li>
              <li>Engine dikirimi SIGHUP bila dikonfigurasi</li>
            </ol>
          </div>
        </div>
      </div>`,
    });
    bindConfigEditor(siteId, path, config.content, revisions);
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Config Studio', subtitle: '', body: errorBody(error) });
  }
}

function revisionListHTML(revisions) {
  if (!revisions.length) return '<div class="empty"><p>Belum ada revisi untuk path ini.</p></div>';
  return revisions.slice(0, 12).map((rev) => `
    <div class="revision">
      <div class="meta">
        <strong>${esc(rev.author_name || 'sistem')} · ${esc(timeAgo(rev.created_at))}</strong>
        <span>${esc(rev.hash ? rev.hash.slice(0, 10) : '—')} · ${esc(rev.status)}</span>
      </div>
      <button class="btn text sm" data-rev-view="${esc(rev.id)}">Lihat</button>
      ${rev.status === 'published' || rev.status === 'superseded' ? `<button class="btn ghost sm" data-rev-restore="${esc(rev.id)}">Pulihkan</button>` : ''}
    </div>`).join('');
}

function bindConfigEditor(siteId, path, content, revisions) {
  const textarea = document.getElementById('editor');
  const gutter = document.getElementById('gutter');
  const count = document.getElementById('editor-count');
  const issuesBox = document.getElementById('issues');
  let currentRevisions = revisions;

  textarea.value = content;

  const sync = () => {
    const lines = textarea.value.split('\n');
    gutter.textContent = Array.from({ length: lines.length }, (_, index) => index + 1).join('\n');
    const size = new TextEncoder().encode(textarea.value).length;
    count.textContent = `${lines.length} baris · ${size} / 65536 byte`;
    count.style.color = size > 65536 ? '#f87171' : '';
  };
  sync();
  textarea.addEventListener('input', sync);
  textarea.addEventListener('scroll', () => { gutter.scrollTop = textarea.scrollTop; });
  textarea.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    event.preventDefault();
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    textarea.value = `${textarea.value.slice(0, start)}  ${textarea.value.slice(end)}`;
    textarea.selectionStart = textarea.selectionEnd = start + 2;
    sync();
  });

  const showIssues = (list, valid) => {
    if (valid) {
      issuesBox.innerHTML = '<div class="alert ok"><div>Konfigurasi valid menurut parser engine.</div></div>';
      return;
    }
    if (!list || !list.length) {
      issuesBox.innerHTML = '<div class="alert bad"><div>Validasi gagal tanpa detail.</div></div>';
      return;
    }
    issuesBox.innerHTML = `<h4>${list.length} masalah</h4>${list.map((issue) => `
      <div class="issue">
        <button data-line="${Number(issue.line) || 0}">baris ${Number(issue.line) || '?'}</button>
        <span>${esc(issue.message)}</span>
      </div>`).join('')}`;
    issuesBox.querySelectorAll('[data-line]').forEach((button) => {
      button.addEventListener('click', () => {
        const line = Number(button.dataset.line);
        if (!line) return;
        const lines = textarea.value.split('\n');
        const offset = lines.slice(0, line - 1).join('\n').length + (line > 1 ? 1 : 0);
        const length = (lines[line - 1] || '').length;
        textarea.focus();
        textarea.setSelectionRange(offset, offset + length);
        const lineHeight = 13 * 1.6;
        textarea.scrollTop = Math.max(0, (line - 1) * lineHeight - 80);
      });
    });
  };	const currentPath = () => document.getElementById('path-input').value.trim() || path;

	const setBusy = (busy) => {
    ['btn-validate', 'btn-draft', 'btn-publish'].forEach((id) => {
      const button = document.getElementById(id);
      if (button) button.disabled = busy;
    });
  };

  const loadRevisions = async () => {
    currentRevisions = await api(`/sites/${encodeURIComponent(siteId)}/config/revisions?path=${encodeURIComponent(path)}`);
    renderRevisionContainer();
  };

  const renderRevisionContainer = () => {
    const container = document.getElementById('revisions');
    if (!container) return;
    container.innerHTML = revisionListHTML(currentRevisions);
    bindRevisionActions();
  };

  const bindRevisionActions = () => {
    document.querySelectorAll('[data-rev-view]').forEach((button) => {
      button.onclick = async () => {
        try {
          const rev = await api(`/sites/${encodeURIComponent(siteId)}/config/revisions/${encodeURIComponent(button.dataset.revView)}`);
          textarea.value = rev.content || '';
          sync();
          toast('Isi revisi dimuat ke editor (belum diterapkan)');
        } catch (error) {
          toast(error.message, 'err');
        }
      };
    });
    document.querySelectorAll('[data-rev-restore]').forEach((button) => {
      button.onclick = async () => {
        if (!confirm('Pulihkan revisi ini? File aktif akan ditimpa setelah validasi.')) return;
        try {
          const result = await api(`/sites/${encodeURIComponent(siteId)}/config/rollback`, {
            method: 'POST',
            body: { revision_id: button.dataset.revRestore },
          });
          toast(`Revisi dipulihkan (reload: ${result.reload})`);
          renderConfigPage(siteId, ++renderToken);
        } catch (error) {
          toast(error.message, 'err');
        }
      };
    });
  };
  bindRevisionActions();

  document.getElementById('btn-open').onclick = () => {
    const next = document.getElementById('path-input').value.trim() || '/';
    navigate(`/sites/${encodeURIComponent(siteId)}/config?path=${encodeURIComponent(next)}`);
  };
  document.getElementById('path-input').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') document.getElementById('btn-open').click();
  });

  document.getElementById('btn-validate').onclick = async () => {
    setBusy(true);
    try {
      const result = await api(`/sites/${encodeURIComponent(siteId)}/config/validate`, {
        method: 'POST',
        body: { path: currentPath(), content: textarea.value },
      });
      showIssues(result.issues, result.valid);
      toast(result.valid ? 'Konfigurasi valid' : 'Konfigurasi tidak valid', result.valid ? 'ok' : 'err');
    } catch (error) {
      showIssues(error.details || [], false);
      toast(error.message, 'err');
    } finally {
      setBusy(false);
    }
  };

  document.getElementById('btn-draft').onclick = async () => {
    setBusy(true);
    try {
      const result = await api(`/sites/${encodeURIComponent(siteId)}/config/draft`, {
        method: 'PUT',
        body: { path: currentPath(), content: textarea.value },
      });
      showIssues(result.issues || [], !(result.issues || []).length);
      toast('Draft disimpan');
      await loadRevisions();
    } catch (error) {
      toast(error.message, 'err');
    } finally {
      setBusy(false);
    }
  };

  document.getElementById('btn-publish').onclick = async () => {
    setBusy(true);
    try {
      const result = await api(`/sites/${encodeURIComponent(siteId)}/config/publish`, {
        method: 'POST',
        body: { path: currentPath(), content: textarea.value },
      });
      showIssues([], true);
      const reloadText = result.reload === 'reloaded' ? 'engine di-reload' : 'reload engine dilewati';
      toast(`Config aktif (${reloadText})`);
      if (result.reload_error) toast(result.reload_error, 'err');
      await loadRevisions();
    } catch (error) {
      if (error.details && error.details.length) showIssues(error.details, false);
      toast(error.message, 'err');
    } finally {
      setBusy(false);
    }
  };
}

/* ---------- Audit & settings ---------- */
async function renderAudit(token) {
  loadingShell('Log Audit', 'Jejak perubahan panel');
  try {
    const events = await api('/audit?limit=200');
    if (token !== renderToken) return;
    const rows = events.map((event) => `
      <tr>
        <td class="small muted" style="white-space:nowrap">${esc(dateTime(event.ts))}</td>
        <td>${esc(event.actor_name || 'sistem')}</td>
        <td><span class="mono-chip">${esc(event.action)}</span></td>
        <td class="small">${esc(event.target || '—')}</td>
        <td>${event.result === 'success' ? '<span class="badge green">success</span>' : `<span class="badge red">${esc(event.result)}</span>`}</td>
        <td class="mono small">${esc(event.ip || '—')}</td>
      </tr>`).join('') || `<tr class="empty-row"><td colspan="6">Belum ada event audit.</td></tr>`;
    shell({
      title: 'Log Audit',
      subtitle: 'Setiap mutasi situs dan konfigurasi tercatat dengan hash berantai',
      active: 'audit',
      body: `<div class="card"><div class="card-head"><h2>${events.length} event terbaru</h2><span class="hint">Retensi maksimum 20.000 event</span></div>
        <div class="table-wrap"><table class="table"><thead><tr><th>Waktu</th><th>Aktor</th><th>Aksi</th><th>Target</th><th>Hasil</th><th>IP</th></tr></thead><tbody>${rows}</tbody></table></div></div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Log Audit', subtitle: '', body: errorBody(error) });
  }
}

async function renderSettings(token) {
  loadingShell('Settings', 'Informasi dan batas panel');
  try {
    const overview = await api('/overview');
    if (token !== renderToken) return;
    shell({
      title: 'Settings',
      subtitle: 'Konfigurasi global panel pada slice ini masih read-only',
      active: 'settings',
      body: `
      <div class="grid two-col">
        <div class="card">
          <div class="card-head"><h2>Panel</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>Versi</dt><dd class="mono">${esc(overview.version)}</dd>
              <dt>Basis docroot</dt><dd class="mono">${esc(overview.www_base)}</dd>
              <dt>Uptime panel</dt><dd>${esc(duration(overview.system.panel_uptime))}</dd>
              <dt>Waktu server</dt><dd>${esc(dateTime(overview.system.server_time))}</dd>
              <dt>Engine</dt><dd>${esc(overview.engine.state === 'configured' ? 'SIGHUP terkonfigurasi' : 'Belum terhubung')}</dd>
            </dl>
          </div>
        </div>
        <div class="card">
          <div class="card-head"><h2>Batas keamanan</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>Config</dt><dd>Maks 64 KiB per file, parser engine</dd>
              <dt>Path</dt><dd>Terkurung docroot, symlink ditolak</dd>
              <dt>Session</dt><dd>12 jam absolut, 30 menit idle</dd>
              <dt>Login</dt><dd>5 percobaan / 15 menit</dd>
              <dt>Audit</dt><dd>Hash berantai, 20.000 event</dd>
            </dl>
            <div class="alert" style="margin-top:14px"><div class="small">Konfigurasi tingkat lanjut (TLS panel, 2FA, firewall, backup) menyusul pada milestone berikutnya.</div></div>
          </div>
        </div>
      </div>`,
    });
  } catch (error) {
    if (token !== renderToken) return;
    shell({ title: 'Settings', subtitle: '', body: errorBody(error) });
  }
}

/* ---------- Router & boot ---------- */
async function render() {
  const token = ++renderToken;
  const path = location.pathname;

  if (path === '/setup') {
    renderSetup(true);
    return;
  }

  if (!state.me) {
    try {
      const me = await api('/auth/me');
      if (token !== renderToken) return;
      state.me = me.user;
      state.csrf = me.csrf;
      state.version = me.version;
    } catch (error) {
      if (token !== renderToken) return;
      if (path !== '/login') {
        navigate('/login', true);
        return;
      }
      renderLogin();
      return;
    }
  }

  if (path === '/login') {
    navigate('/', true);
    return;
  }
  if (path === '/') return renderDashboard(token);
  if (path === '/sites') return renderSites(token);
  if (path === '/config') return renderConfigStudio(token);
  if (path === '/audit') return renderAudit(token);
  if (path === '/settings') return renderSettings(token);

  const match = path.match(/^\/sites\/([^/]+)(\/config)?$/);
  if (match) {
    const siteId = decodeURIComponent(match[1]);
    if (match[2]) return renderConfigPage(siteId, token);
    return renderSitePage(siteId, token);
  }
  navigate('/', true);
}

async function boot() {
  try {
    const status = await api('/setup/status');
    if (status.needs_setup) {
      history.replaceState({}, '', '/setup');
      renderSetup(true);
      return;
    }
  } catch (error) {
    // Panel mungkin sedang start; render akan menampilkan error yang jelas.
  }
  render();
}

boot();
