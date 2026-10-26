import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Badge } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents } from '../docker.js';
import { AppIcon } from './store.js';

export default function Apps() {
  const { api, href, navigate } = usePanel();
  const apps = useResource(signal => api.apps({ signal }));
  useDockerEvents(['container'], apps.reload, 800);
  return html`<${Page} title="Installed apps"
    actions=${html`<button class="button button-primary" onClick=${() => navigate('/store')}>Browse the app store</button>`}>
    <${Resource} resource=${apps} empty=${html`No apps installed yet. Have a look in the <a href=${href('/store')}>app store</a>.`}>${list => html`
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
            ${a.update_available ? html`<${Badge} tone="blue">Update available<//>` : null}
            ${a.app ? null : html`<${Badge} tone="amber">Removed from the catalog<//>`}
          </div>
          <div class="form-actions">
            ${a.url && a.status === 'running' ? html`<a class="button button-small button-primary" href=${a.url} target="_blank" rel="noopener">Open ↗</a>` : null}
            <a class="button button-small" href=${href(`/apps/${a.id}`)}>Manage</a>
          </div>
        </div>`)}
      </div>`}
    <//>
  <//>`;
}
