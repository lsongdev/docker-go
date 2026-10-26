import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, KV, Table } from '../lib/ui.js';
import { bytes, datetime } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents } from '../docker.js';

function Stat({ label, value, sub, to }) {
  const { href } = usePanel();
  return html`<a class="card stat" href=${href(to)}>
    <span class="stat-label">${label}</span>
    <span class="stat-value">${value}</span>
    ${sub ? html`<span class="stat-sub">${sub}</span>` : null}
  </a>`;
}

export default function Dashboard() {
  const { api, href } = usePanel();
  const system = useResource(signal => api.system({ signal }));
  const installed = useResource(signal => api.apps({ signal }));
  useDockerEvents(['container', 'image'], () => { system.reload(); installed.reload(); });
  return html`<${Page} title="Dashboard">
    <${Resource} resource=${system}>${s => html`
      <div class="grid">
        <${Stat} label="Containers" value=${s.containers} sub=${`${s.running} running · ${s.stopped} stopped${s.paused ? ` · ${s.paused} paused` : ''}`} to="/containers" />
        <${Stat} label="Images" value=${s.images} to="/images" />
        <${Stat} label="Installed apps" value=${installed.data?.length ?? '—'}
          sub=${installed.data ? `${installed.data.filter(a => a.status === 'running').length} running` : ''} to="/apps" />
        <${Stat} label="App store" value=${s.catalog_apps} sub=${s.catalog_loaded_at ? `Updated ${datetime(s.catalog_loaded_at)}` : 'Not synced yet'} to="/store" />
      </div>
      <div class="grid-2 section">
        <div class="card"><h2>Docker</h2>
          <${KV} items=${[
            ['Version', `${s.version} (API ${s.api_version})`],
            ['Compose', s.compose_version],
            ['Host', s.name],
            ['OS', s.os],
            ['Kernel', s.kernel],
            ['Architecture', s.arch],
            ['CPU / memory', `${s.cpus} cores / ${bytes(s.memory)}`],
            ['Storage driver', s.storage_driver],
            ['Data directory', html`<code>${s.root_dir}</code>`],
            ['Endpoint', html`<code>${s.host}</code>`],
          ]} />
        </div>
        <div class="card"><h2>Apps</h2>
          <${Resource} resource=${installed} empty=${html`No apps installed yet. Have a look in the <a href=${href('/store')}>app store</a>.`}>${list => html`
            <${Table} framed=${false}><tbody>
              ${list.map(a => html`<tr key=${a.id}>
                <td><a class="cell-main" href=${href(`/apps/${a.id}`)}>${a.app?.name || a.id}</a></td>
                <td><${Status} state=${a.status} /></td>
                <td class="actions">${a.url && a.status === 'running' ? html`<a href=${a.url} target="_blank" rel="noopener">Open ↗</a>` : null}</td>
              </tr>`)}
            </tbody><//>`}
          <//>
        </div>
      </div>`}
    <//>
  <//>`;
}
