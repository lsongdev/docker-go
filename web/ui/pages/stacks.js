import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Badge, Table } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, startJob } from '../docker.js';

export default function Stacks() {
  const { api, href, navigate } = usePanel();
  const stacks = useResource(signal => api.stacks({ signal }));
  useDockerEvents(['container'], stacks.reload, 800);
  const act = (name, action) => startJob(api, () => api.stackAction(name, action), { onDone: stacks.reload }).catch(() => {});
  return html`<${Page} title="Compose" subtitle="Multi-container applications run with docker compose"
    actions=${html`<button class="button button-primary" onClick=${() => navigate('/stacks/new')}>New stack</button>`}>
    <${Resource} resource=${stacks} empty="No compose projects yet">${list => html`
      <${Table}>
        <thead><tr><th>Name</th><th>Status</th><th>Containers</th><th>Compose file</th><th class="actions">Actions</th></tr></thead>
        <tbody>${list.map(s => html`<tr key=${s.name}>
          <td><a class="cell-main" href=${href(`/stacks/${s.name}`)}>${s.name}</a>
            ${s.managed ? null : html` <${Badge} title="Created outside this module; only lifecycle actions are available">External<//>`}</td>
          <td><${Status} state=${s.status} /></td>
          <td class="cell-sub">${s.containers || '—'}</td>
          <td class="truncate mono" style="max-width:320px" title=${s.config_files.join('\n')}>${s.config_files[0] || '—'}</td>
          <td class="actions"><span class="button-row">
            ${s.status === 'running' || s.status === 'partial'
              ? html`<button class="button button-small" onClick=${() => act(s.name, 'restart')}>Restart</button>
                     <button class="button button-small" onClick=${() => act(s.name, 'stop')}>Stop</button>`
              : html`<button class="button button-small" onClick=${() => act(s.name, 'up')}>Start</button>`}
          </span></td>
        </tr>`)}</tbody>
      <//>`}
    <//>
  <//>`;
}
