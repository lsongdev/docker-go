import { html, useMemo } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Status, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
import { ago, shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, PruneButton } from '../docker.js';

export function portLabel(p) {
  return p.public ? `${p.public}→${p.private}/${p.protocol}` : `${p.private}/${p.protocol}`;
}

// ContainerActions renders lifecycle buttons for one container.
export function ContainerActions({ c, onChange, compact }) {
  const { api } = usePanel();
  const [busy, run] = useBusy(toastError);
  const act = (action, label) => run(action, async () => {
    await api.containerAction(c.id, action);
    toast(`${c.name}: ${label}`, 'success');
    onChange?.();
  });
  const remove = () => {
    if (!confirmDanger(`Delete the container ${c.name}?${c.state === 'running' ? '\nIt is running and will be stopped forcibly.' : ''}`)) return;
    run('remove', async () => {
      await api.removeContainer(c.id, { force: true });
      toast(`Deleted ${c.name}`, 'success');
      onChange?.('removed');
    });
  };
  const size = compact ? 'button button-small' : 'button';
  const running = c.state === 'running', paused = c.state === 'paused';
  return html`<span class="button-row">
    ${running ? html`
      <button class=${size} disabled=${busy.stop} onClick=${() => act('stop', 'stopped')}>Stop</button>
      <button class=${size} disabled=${busy.restart} onClick=${() => act('restart', 'restarted')}>Restart</button>
      ${compact ? null : html`<button class=${size} disabled=${busy.pause} onClick=${() => act('pause', 'paused')}>Pause</button>`}`
    : paused ? html`<button class=${size} disabled=${busy.unpause} onClick=${() => act('unpause', 'resumed')}>Resume</button>`
    : html`<button class=${size} disabled=${busy.start} onClick=${() => act('start', 'started')}>Start</button>`}
    <button class=${`${size} button-danger`} disabled=${busy.remove} onClick=${remove}>Delete</button>
  </span>`;
}

export default function Containers({ query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const containers = useResource(signal => api.containers({ signal }));
  useDockerEvents(['container'], containers.reload);
  const search = query.q || '';
  const state = query.state || '';
  const filtered = useMemo(() => (containers.data || []).filter(c =>
    (!state || (state === 'running' ? c.state === 'running' : c.state !== 'running')) &&
    (!search || `${c.name} ${c.image} ${c.project || ''}`.toLowerCase().includes(search.toLowerCase()))
  ), [containers.data, search, state]);
  return html`<${Page} title="Containers" actions=${html`
    <${PruneButton} title="Prune containers" description="Delete every stopped container." prune=${api.pruneContainers} onDone=${containers.reload} />
    <button class="button button-primary" onClick=${() => navigate('/containers/new')}>New container</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="Search names, images, projects" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
      <select class="input" style="max-width:140px" value=${state} onChange=${e => setQuery({ state: e.target.value })}>
        <option value="">All states</option><option value="running">Running</option><option value="stopped">Not running</option>
      </select>
    </div>
    <${Resource} resource=${{ ...containers, data: containers.data && filtered }}
      empty=${containers.data?.length ? 'No matching containers' : 'No containers yet'}>${list => html`
      <${Table}>
        <thead><tr><th>Name</th><th>Image</th><th>Status</th><th>Ports</th><th>Created</th><th class="actions">Actions</th></tr></thead>
        <tbody>${list.map(c => html`<tr key=${c.id}>
          <td><a class="cell-main" href=${href(`/containers/${c.id}`)}>${c.name}</a>
            <div class="cell-sub">${shortId(c.id)}${c.project ? html` · <a href=${href(`/stacks/${c.project}`)}>${c.project}</a>` : null}</div></td>
          <td class="truncate" style="max-width:260px" title=${c.image}>${c.image}</td>
          <td><${Status} state=${c.state} title=${c.status} /><div class="cell-sub">${c.status}</div></td>
          <td><div class="tags">${c.ports.filter(p => p.public).map(p => html`<code>${portLabel(p)}</code>`)}</div></td>
          <td class="nowrap">${ago(c.created)}</td>
          <td class="actions"><${ContainerActions} c=${c} compact onChange=${containers.reload} /></td>
        </tr>`)}</tbody>
      <//>`}
    <//>
  <//>`;
}
