import { html, useMemo } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
import { ago } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, PruneButton } from '../docker.js';

export default function Volumes({ query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const volumes = useResource(signal => api.volumes({ signal }));
  useDockerEvents(['volume', 'container'], volumes.reload);
  const [busy, run] = useBusy(toastError);
  const search = query.q || '';
  const filtered = useMemo(() => (volumes.data || []).filter(v =>
    !search || `${v.name} ${v.project || ''}`.toLowerCase().includes(search.toLowerCase())), [volumes.data, search]);
  const remove = v => {
    if (!confirmDanger(`Delete the volume ${v.name}? Its data is lost for good.`)) return;
    run(v.name, async () => {
      await api.removeVolume(v.name);
      toast(`Deleted ${v.name}`, 'success');
      volumes.reload();
    });
  };
  return html`<${Page} title="Volumes" actions=${html`
    <${PruneButton} title="Prune volumes" description="Delete every anonymous volume no container uses. Their data is lost for good."
      allLabel="Also delete unused named volumes" prune=${api.pruneVolumes} onDone=${volumes.reload} />
    <button class="button button-primary" onClick=${() => navigate('/volumes/new')}>New volume</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="Search names or projects" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
    </div>
    <${Resource} resource=${{ ...volumes, data: volumes.data && filtered }} empty="No volumes">${list => html`
      <${Table}>
        <thead><tr><th>Name</th><th>Driver</th><th>Used by</th><th>Mount point</th><th>Created</th><th class="actions">Actions</th></tr></thead>
        <tbody>${list.map(v => html`<tr key=${v.name}>
          <td><span class="cell-main mono">${v.name.length > 40 ? v.name.slice(0, 12) + '…' : v.name}</span>
            ${v.project ? html`<div class="cell-sub"><a href=${href(`/stacks/${v.project}`)}>${v.project}</a></div>` : null}</td>
          <td>${v.driver}</td>
          <td>${v.containers.length ? html`<${Badge} tone="blue" title=${v.containers.join('\n')}>${v.containers.join(', ')}<//>` : html`<span class="muted">Unused</span>`}</td>
          <td class="truncate mono" style="max-width:280px" title=${v.mountpoint}>${v.mountpoint}</td>
          <td class="nowrap">${ago(v.created_at)}</td>
          <td class="actions"><button class="button button-small button-danger" disabled=${busy[v.name]} onClick=${() => remove(v)}>Delete</button></td>
        </tr>`)}</tbody>
      <//>`}
    <//>
  <//>`;
}
