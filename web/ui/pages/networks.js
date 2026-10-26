import { html } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
import { shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, PruneButton } from '../docker.js';

export default function Networks() {
  const { api, href, navigate } = usePanel();
  const networks = useResource(signal => api.networks({ signal }));
  useDockerEvents(['network', 'container'], networks.reload);
  const [busy, run] = useBusy(toastError);
  const remove = n => {
    if (!confirmDanger(`Delete the network ${n.name}?`)) return;
    run(n.id, async () => {
      await api.removeNetwork(n.id);
      toast(`Deleted ${n.name}`, 'success');
      networks.reload();
    });
  };
  return html`<${Page} title="Networks" actions=${html`
    <${PruneButton} title="Prune networks" description="Delete every custom network no container uses." prune=${api.pruneNetworks} onDone=${networks.reload} />
    <button class="button button-primary" onClick=${() => navigate('/networks/new')}>New network</button>`}>
    <${Resource} resource=${networks} empty="No networks">${list => html`
      <${Table}>
        <thead><tr><th>Name</th><th>Driver</th><th>Subnets</th><th>Containers</th><th>Attributes</th><th class="actions">Actions</th></tr></thead>
        <tbody>${list.map(n => html`<tr key=${n.id}>
          <td><a class="cell-main" href=${href(`/networks/${n.id}`)}>${n.name}</a><div class="cell-sub">${shortId(n.id)}</div></td>
          <td>${n.driver}</td>
          <td>${n.subnets.map(s => html`<div><code>${s.subnet}</code></div>`)}</td>
          <td>${n.containers.length || html`<span class="muted">0</span>`}</td>
          <td><div class="tags">
            ${n.builtin ? html`<${Badge}>Built-in<//>` : null}
            ${n.internal ? html`<${Badge} tone="amber">Internal<//>` : null}
            ${n.ipv6 ? html`<${Badge}>IPv6<//>` : null}
            ${n.project ? html`<${Badge} tone="blue">${n.project}<//>` : null}
          </div></td>
          <td class="actions">${n.builtin ? null : html`<button class="button button-small button-danger" disabled=${busy[n.id]} onClick=${() => remove(n)}>Delete</button>`}</td>
        </tr>`)}</tbody>
      <//>`}
    <//>
  <//>`;
}
