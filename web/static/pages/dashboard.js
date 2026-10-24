import { html } from '../lib/html.js';
import { api } from '../lib/api.js';
import { href } from '../lib/router.js';
import { useResource, useDockerEvents } from '../lib/hooks.js';
import { Page, Resource, Status, KV } from '../lib/ui.js';
import { bytes, datetime } from '../lib/format.js';

function Stat({ label, value, sub, to }) {
  return html`<a class="card stat" href=${href(to)}>
    <span class="stat-label">${label}</span>
    <span class="stat-value">${value}</span>
    ${sub ? html`<span class="stat-sub">${sub}</span>` : null}
  </a>`;
}

export default function Dashboard() {
  const system = useResource(signal => api.system({ signal }));
  const installed = useResource(signal => api.apps({ signal }));
  useDockerEvents(['container', 'image'], () => { system.reload(); installed.reload(); });
  return html`<${Page} title="仪表盘">
    <${Resource} resource=${system}>${s => html`
      <div class="grid">
        <${Stat} label="容器" value=${s.containers} sub=${`${s.running} 运行 · ${s.stopped} 停止${s.paused ? ` · ${s.paused} 暂停` : ''}`} to="/containers" />
        <${Stat} label="镜像" value=${s.images} to="/images" />
        <${Stat} label="已安装应用" value=${installed.data?.length ?? '—'}
          sub=${installed.data ? `${installed.data.filter(a => a.status === 'running').length} 运行中` : ''} to="/apps" />
        <${Stat} label="应用商店" value=${s.catalog_apps} sub=${s.catalog_loaded_at ? `更新于 ${datetime(s.catalog_loaded_at)}` : '尚未同步'} to="/store" />
      </div>
      <div class="grid-2 section">
        <div class="card"><h2>Docker</h2>
          <${KV} items=${[
            ['版本', `${s.version} (API ${s.api_version})`],
            ['Compose', s.compose_version],
            ['主机', s.name],
            ['系统', s.os],
            ['内核', s.kernel],
            ['架构', s.arch],
            ['CPU / 内存', `${s.cpus} 核 / ${bytes(s.memory)}`],
            ['存储驱动', s.storage_driver],
            ['数据目录', html`<code>${s.root_dir}</code>`],
            ['连接', html`<code>${s.host}</code>`],
          ]} />
        </div>
        <div class="card"><h2>应用</h2>
          <${Resource} resource=${installed} empty=${html`还没有安装应用，去 <a href=${href('/store')}>应用商店</a> 看看`}>${list => html`
            <table class="table"><tbody>
              ${list.map(a => html`<tr key=${a.id}>
                <td><a class="cell-main" href=${href(`/apps/${a.id}`)}>${a.app?.name || a.id}</a></td>
                <td><${Status} state=${a.status} /></td>
                <td class="actions">${a.url && a.status === 'running' ? html`<a href=${a.url} target="_blank" rel="noopener">打开 ↗</a>` : null}</td>
              </tr>`)}
            </tbody></table>`}
          <//>
        </div>
      </div>`}
    <//>
  <//>`;
}
