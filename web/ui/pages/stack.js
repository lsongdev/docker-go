import { html, useEffect, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Tabs, Field, Notice, Badge, ConfirmButton, toast, Table } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, startJob } from '../docker.js';

export default function Stack({ params, query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const stack = useResource(signal => api.stack(params.name, { signal }), [params.name]);
  useDockerEvents(['container'], stack.reload, 800);
  const tab = query.tab || 'services';
  const s = stack.data;
  const act = action => startJob(api, () => api.stackAction(params.name, action), { onDone: stack.reload }).catch(() => {});
  const remove = volumes => startJob(api, () => api.deleteStack(params.name, volumes), {
    onDone: job => job.status === 'succeeded' && navigate('/stacks'),
  }).catch(() => {}); // startJob already reported the error
  return html`<${Page} title=${params.name}
    subtitle=${s ? html`<${Status} state=${s.status} /> <span class="mono">${s.dir}</span>` : null}
    back=${{ href: href('/stacks'), label: 'Compose' }}
    actions=${s ? html`
      <button class="button button-primary" onClick=${() => act('up')}>Deploy</button>
      ${s.status !== 'inactive' ? html`
        <button class="button" onClick=${() => act('restart')}>Restart</button>
        <button class="button" onClick=${() => act('stop')}>Stop</button>
        <button class="button" onClick=${() => act('down')}>Down</button>` : null}
      <button class="button" onClick=${() => act('pull')}>Pull images</button>
      ${s.managed ? html`<${ConfirmButton} label="Delete" title=${`Delete ${params.name}`} confirmLabel="Delete"
        description="Stop the stack, delete its containers and networks, and remove the project directory." checkbox="Also delete its volumes (their data is lost for good)" onConfirm=${remove} />` : null}` : null}>
    <${Resource} resource=${stack}>${s => html`
      ${s.managed ? null : html`<${Notice} tone="info">This project was created outside this module, so its files are read-only; it can still be deployed, stopped and so on.<//>`}
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['services', `Services (${s.services.length})`], ['config', 'Configuration']]} />
      ${tab === 'services' ? html`<${Services} services=${s.services} />` : html`<${Editor} stack=${s} onSaved=${stack.reload} />`}`}
    <//>
  <//>`;
}

function Services({ services }) {
  const { href } = usePanel();
  if (!services.length) return html`<div class="empty">Not deployed yet: click Deploy to create the containers</div>`;
  return html`<${Table}>
    <thead><tr><th>Service</th><th>Container</th><th>Image</th><th>Status</th><th>Ports</th></tr></thead>
    <tbody>${services.map(sv => html`<tr key=${sv.ID}>
      <td class="cell-main">${sv.Service}</td>
      <td><a href=${href(`/containers/${sv.ID}`)}>${sv.Name}</a></td>
      <td class="truncate" style="max-width:240px" title=${sv.Image}>${sv.Image}</td>
      <td><${Status} state=${sv.Health || sv.State} title=${sv.Status} /><div class="cell-sub">${sv.Status}</div></td>
      <td><div class="tags">${(sv.Publishers || []).filter(p => p.PublishedPort).map(p =>
        html`<code>${p.PublishedPort}→${p.TargetPort}/${p.Protocol}</code>`)}</div></td>
    </tr>`)}</tbody>
  <//>`;
}

function Editor({ stack, onSaved }) {
  const { api } = usePanel();
  const [compose, setCompose] = useState(stack.compose);
  const [env, setEnv] = useState(stack.env);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => { setCompose(stack.compose); setEnv(stack.env); }, [stack.name]);
  const dirty = compose !== stack.compose || env !== stack.env;
  const save = async deploy => {
    setError(null);
    setSaving(true);
    try {
      await api.updateStack(stack.name, { compose, env });
      toast('Configuration saved', 'success');
      onSaved();
      if (deploy) await startJob(api, () => api.stackAction(stack.name, 'up'), { onDone: onSaved }).catch(() => {});
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };
  const readOnly = !stack.managed;
  return html`<div class="form card">
    <${Notice} error=${error} />
    <div class="grid-2">
      <${Field} label=${html`${stack.config_files[0]?.split('/').pop() || 'compose.yaml'} ${readOnly ? html`<${Badge}>Read-only<//>` : null}`}>
        <textarea class="input code" spellcheck="false" readOnly=${readOnly} value=${compose} onInput=${e => setCompose(e.target.value)}></textarea><//>
      <${Field} label=".env"><textarea class="input code" spellcheck="false" readOnly=${readOnly} value=${env} onInput=${e => setEnv(e.target.value)}></textarea><//>
    </div>
    ${readOnly ? null : html`<div class="form-actions">
      <button class="button button-primary" disabled=${!dirty || saving} onClick=${() => save(true)}>Save and redeploy</button>
      <button class="button" disabled=${!dirty || saving} onClick=${() => save(false)}>Save only</button>
      ${dirty ? html`<button class="button button-ghost" onClick=${() => { setCompose(stack.compose); setEnv(stack.env); }}>Discard changes</button>` : null}
    </div>`}
  </div>`;
}
