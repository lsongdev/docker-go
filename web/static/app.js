import { html, render, useEffect, useState } from './lib/html.js';
import { useRoute, href } from './lib/router.js';
import { api } from './lib/api.js';
import { JobHost, Toaster, Notice, Loading, onJobsChanged, openJob } from './lib/ui.js';

// Pages are loaded on first visit with native dynamic import.
const page = loader => ({ loader });
const routes = [
  { path: '/', ...page(() => import('./pages/dashboard.js')) },
  { path: '/containers', ...page(() => import('./pages/containers.js')) },
  { path: '/containers/new', ...page(() => import('./pages/container-new.js')) },
  { path: '/containers/:id', ...page(() => import('./pages/container.js')) },
  { path: '/images', ...page(() => import('./pages/images.js')) },
  { path: '/images/build', ...page(() => import('./pages/image-build.js')) },
  { path: '/images/:id', ...page(() => import('./pages/image.js')) },
  { path: '/volumes', ...page(() => import('./pages/volumes.js')) },
  { path: '/volumes/new', ...page(() => import('./pages/volume-new.js')) },
  { path: '/networks', ...page(() => import('./pages/networks.js')) },
  { path: '/networks/new', ...page(() => import('./pages/network-new.js')) },
  { path: '/networks/:id', ...page(() => import('./pages/network.js')) },
  { path: '/stacks', ...page(() => import('./pages/stacks.js')) },
  { path: '/stacks/new', ...page(() => import('./pages/stack-new.js')) },
  { path: '/stacks/:name', ...page(() => import('./pages/stack.js')) },
  { path: '/store', ...page(() => import('./pages/store.js')) },
  { path: '/store/:id', ...page(() => import('./pages/store-app.js')) },
  { path: '/apps', ...page(() => import('./pages/apps.js')) },
  { path: '/apps/:id', ...page(() => import('./pages/app.js')) },
  { path: '/jobs', ...page(() => import('./pages/jobs.js')) },
];

const nav = [
  ['概览', [['/', '仪表盘', '◎']]],
  ['资源', [['/containers', '容器', '▣'], ['/images', '镜像', '◫'], ['/volumes', '存储卷', '⛁'], ['/networks', '网络', '⇄']]],
  ['编排', [['/stacks', 'Compose', '☰']]],
  ['应用', [['/store', '应用商店', '✦'], ['/apps', '已安装', '▤']]],
];

const modules = new Map();

function Lazy({ route, params, query }) {
  const [, force] = useState(0);
  const [error, setError] = useState(null);
  const mod = modules.get(route);
  useEffect(() => {
    if (mod) return;
    setError(null);
    route.loader().then(m => { modules.set(route, m); force(n => n + 1); }).catch(setError);
  }, [route]);
  if (error) return html`<${Notice} error=${error} />`;
  if (!mod) return html`<${Loading} />`;
  const Page = mod.default;
  return html`<${Page} params=${params} query=${query} />`;
}

function JobsIndicator() {
  const [running, setRunning] = useState([]);
  useEffect(() => {
    let timer;
    const refresh = () => api.jobs().then(list => {
      const active = list.filter(j => j.status === 'running');
      setRunning(active);
      clearTimeout(timer);
      if (active.length) timer = setTimeout(refresh, 3000);
    }).catch(() => {});
    refresh();
    const off = onJobsChanged(refresh);
    return () => { off(); clearTimeout(timer); };
  }, []);
  return html`<a class="nav-link" href=${href('/jobs')}>
    <span class="nav-icon">⟳</span>任务
    ${running.length ? html`<span class="nav-count" title=${running.map(j => j.title).join('\n')}
      onClick=${e => { if (running.length === 1) { e.preventDefault(); openJob(running[0].id); } }}>${running.length}</span>` : null}
  </a>`;
}

function App() {
  const location = useRoute(routes);
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => setMenuOpen(false), [location.path]);
  const section = '/' + (location.path.split('/')[1] || '');
  return html`<div class=${`layout ${menuOpen ? 'menu-open' : ''}`}>
    <aside class="sidebar">
      <a class="brand" href=${href('/')}><span class="brand-mark">◆</span>Docker Panel</a>
      <button class="btn btn-ghost menu-toggle" aria-label="菜单" onClick=${() => setMenuOpen(o => !o)}>☰</button>
      <nav class="nav">
        ${nav.map(([group, items]) => html`<div class="nav-group">
          <div class="nav-heading">${group}</div>
          ${items.map(([path, label, icon]) => html`<a class=${`nav-link ${section === path ? 'active' : ''}`} href=${href(path)}>
            <span class="nav-icon">${icon}</span>${label}</a>`)}
        </div>`)}
        <div class="nav-group"><div class="nav-heading">系统</div><${JobsIndicator} /></div>
      </nav>
    </aside>
    <main class="main">
      ${location.route
        ? html`<${Lazy} key=${location.route.path} route=${location.route} params=${location.params} query=${location.query} />`
        : html`<${Notice}>页面不存在<//>`}
    </main>
    <${JobHost} />
    <${Toaster} />
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
