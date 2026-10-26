import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, KV, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
import { datetime, shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents } from '../docker.js';

export default function Network({ params }) {
  const { api, href, navigate } = usePanel();
  const network = useResource(signal => api.network(params.id, { signal }), [params.id]);
  useDockerEvents(['network'], network.reload);
  const n = network.data;
  const remove = async () => {
    if (!confirmDanger(`Delete the network ${n.name}?`)) return;
    try {
      await api.removeNetwork(n.id);
      toast('Network deleted', 'success');
      navigate('/networks');
    } catch (err) { toastError(err); }
  };
  return html`<${Page} title=${n?.name || shortId(params.id)} subtitle=${n ? html`<code>${n.id}</code>` : null}
    actions=${html`${n && !n.builtin ? html`<button class="button button-danger" onClick=${remove}>Delete</button>` : null}
      <a class="button" href=${href('/networks')}>Back</a>`}>
    <${Resource} resource=${network}>${n => html`
      <div class="grid-2">
        <div class="card"><h2>Configuration</h2>
          <${KV} items=${[
            ['Driver', n.driver],
            ['Scope', n.scope],
            ['Subnets', n.subnets.map(s => html`<div><code>${s.subnet}</code>${s.gateway ? html` gateway <code>${s.gateway}</code>` : null}${s.ip_range ? html` range <code>${s.ip_range}</code>` : null}</div>`)],
            ['Internal', n.internal ? 'Yes' : 'No'],
            ['Attachable', n.attachable ? 'Yes' : 'No'],
            ['IPv6', n.ipv6 ? 'Enabled' : 'Disabled'],
            ['Compose', n.project ? html`<a href=${href(`/stacks/${n.project}`)}>${n.project}</a>` : null],
            ['Created', datetime(n.created)],
          ]} />
        </div>
        <div class="card"><h2>Connected containers</h2>
          ${n.containers.length ? html`<${Table} framed=${false}><tbody>${n.containers.map(c => html`<tr>
            <td><a href=${href(`/containers/${c.id}`)}>${c.name}</a></td>
            <td><code>${c.ipv4 || '—'}</code></td><td class="cell-sub">${c.mac}</td>
          </tr>`)}</tbody><//>` : html`<span class="muted">No containers are connected</span>`}
        </div>
      </div>`}
    <//>
  <//>`;
}
