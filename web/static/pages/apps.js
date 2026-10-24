import { html } from '../lib/html.js';
import { api } from '../lib/api.js';
import { href, navigate } from '../lib/router.js';
import { useResource, useDockerEvents } from '../lib/hooks.js';
import { Page, Resource, Status, Badge } from '../lib/ui.js';
import { AppIcon } from './store.js';

export default function Apps() {
  const apps = useResource(signal => api.apps({ signal }));
  useDockerEvents(['container'], apps.reload, 800);
  return html`<${Page} title="已安装应用"
    actions=${html`<button class="btn btn-primary" onClick=${() => navigate('/store')}>浏览应用商店</button>`}>
    <${Resource} resource=${apps} empty=${html`还没有安装应用，去 <a href=${href('/store')}>应用商店</a> 看看`}>${list => html`
      <div class="app-grid">${list.map(a => html`
        <div class="card app-card" key=${a.id}>
          <a class="app-head" href=${href(`/apps/${a.id}`)}>
            <${AppIcon} id=${a.id} />
            <div style="min-width:0">
              <h3>${a.app?.name || a.id}</h3>
              <div class="cell-sub">${a.version}${a.update_available ? html` → <span style="color:var(--primary)">${a.latest_version}</span>` : null}</div>
            </div>
          </a>
          <div class="tags">
            <${Status} state=${a.status} />
            ${a.update_available ? html`<${Badge} tone="blue">可更新<//>` : null}
            ${a.app ? null : html`<${Badge} tone="amber">已从目录移除<//>`}
          </div>
          <div class="form-actions">
            ${a.url && a.status === 'running' ? html`<a class="btn btn-sm btn-primary" href=${a.url} target="_blank" rel="noopener">打开 ↗</a>` : null}
            <a class="btn btn-sm" href=${href(`/apps/${a.id}`)}>管理</a>
          </div>
        </div>`)}
      </div>`}
    <//>
  <//>`;
}
