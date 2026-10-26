import { html, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, KV, Tabs, Field, Notice, Badge, ConfirmButton, Table } from '../lib/ui.js';
import { datetime } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, startJob } from '../docker.js';
import { AppIcon } from './store.js';
import { AppForm, DomainField, initialValues } from './store-app.js';

export default function App({ params, query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const res = useResource(signal => api.app(params.id, { signal }), [params.id]);
  const app = { ...res, data: res.data?.app };
  useDockerEvents(['container'], app.reload, 800);
  const tab = query.tab || 'overview';
  const a = app.data;
  const act = action => startJob(api, () => api.appAction(params.id, action), { onDone: app.reload }).catch(() => {});
  const update = () => startJob(api, () => api.updateApp(params.id), { onDone: app.reload }).catch(() => {});
  const uninstall = keepData => startJob(api, () => api.uninstallApp(params.id, keepData), {
    onDone: job => job.status === 'succeeded' && navigate('/apps'),
  }).catch(() => {});
  const running = a && (a.status === 'running' || a.status === 'partial');
  return html`<${Page} title=${a?.app?.name || params.id}
    subtitle=${a ? html`<${Status} state=${a.status} /> Version ${a.version}` : null}
    actions=${a ? html`
      ${a.url && a.status === 'running' ? html`<a class="button button-primary" href=${a.url} target="_blank" rel="noopener">Open ↗</a>` : null}
      ${running
        ? html`<button class="button" onClick=${() => act('restart')}>Restart</button><button class="button" onClick=${() => act('stop')}>Stop</button>`
        : html`<button class="button" onClick=${() => act('start')}>Start</button>`}
      ${a.update_available ? html`<button class="button" onClick=${update}>Update to ${a.latest_version}</button>` : null}
      <${ConfirmButton} label="Uninstall" title=${`Uninstall ${a.app?.name || a.id}`} confirmLabel="Uninstall"
        description="Stop the app and delete its containers and networks." checkbox="Keep the data directory and volumes for a later reinstall" onConfirm=${uninstall} />
      <a class="button" href=${href('/apps')}>Back</a>` : null}>
    <${Resource} resource=${app}>${a => html`
      ${a.app ? null : html`<${Notice} tone="warn">This app was removed from the catalog. It can still be started, stopped and uninstalled, but not updated or reconfigured.<//>`}
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['overview', 'Overview'], ['settings', 'Settings']]} />
      ${tab === 'overview' ? html`<${Overview} a=${a} />` : html`<${Settings} a=${a} exposable=${res.data.exposable} onSaved=${app.reload} />`}`}
    <//>
  <//>`;
}

function Overview({ a }) {
  const { href } = usePanel();
  const secrets = (a.app?.form_fields || []).filter(f => f.type === 'random' || f.type === 'password');
  return html`<div class="grid-2">
    <div class="card">
      <div class="app-head" style="margin-bottom:14px">
        <${AppIcon} id=${a.id} large />
        <div><h2 style="margin:0">${a.app?.name || a.id}</h2><div class="muted">${a.app?.short_desc}</div></div>
      </div>
      <${KV} items=${[
        ['URL', a.url ? html`<a href=${a.url} target="_blank" rel="noopener">${a.url}</a>` : null],
        ['Domain', a.domain || null],
        ['Port', a.port || null],
        ['Version', html`${a.version} ${a.update_available ? html`<${Badge} tone="blue">Latest ${a.latest_version}<//>` : null}`],
        ['Installed', datetime(a.installed_at)],
        ['Updated', datetime(a.updated_at)],
        ['Directory', html`<code>${a.dir}</code>`],
        ['Compose', html`<a href=${href(`/stacks/${a.id}`)}>${a.id}</a>`],
      ]} />
    </div>
    <div>
      <div class="card"><h2>Containers</h2>
        ${a.containers.length ? html`<${Table} framed=${false}><tbody>${a.containers.map(c => html`<tr key=${c.id}>
          <td><a href=${href(`/containers/${c.id}`)}>${c.name}</a><div class="cell-sub">${c.image}</div></td>
          <td><${Status} state=${c.state} title=${c.status} /></td>
        </tr>`)}</tbody><//>` : html`<span class="muted">No containers; click Start to create them</span>`}
      </div>
      ${secrets.length ? html`<div class="card"><h2>Credentials</h2>
        <${KV} items=${secrets.map(f => [f.label, html`<${Secret} value=${a.values[f.env_variable]} />`])} />
      </div>` : null}
    </div>
  </div>`;
}

function Secret({ value }) {
  const [shown, setShown] = useState(false);
  if (!value) return html`<span class="muted">—</span>`;
  const copy = () => navigator.clipboard?.writeText(value);
  return html`<span class="secret">
    <code>${shown ? value : '••••••••'}</code>
    <button class="button button-small button-ghost" onClick=${() => setShown(s => !s)}>${shown ? 'Hide' : 'Show'}</button>
    <button class="button button-small button-ghost" onClick=${copy}>Copy</button>
  </span>`;
}

function Settings({ a, exposable, onSaved }) {
  const { api } = usePanel();
  const fields = a.app?.form_fields || [];
  const [values, setValues] = useState(() => initialValues(fields, a.values));
  const [port, setPort] = useState(a.port || '');
  const [domain, setDomain] = useState(a.domain || '');
  const [errors, setErrors] = useState({});
  const [error, setError] = useState(null);
  if (!a.app) return html`<${Notice} tone="info">The app was removed from the catalog, so its settings cannot change.<//>`;
  const submit = async e => {
    e.preventDefault();
    setErrors({});
    setError(null);
    try {
      await startJob(api, () => api.updateApp(a.id, { port: Number(port) || 0, values, ...(exposable || a.domain ? { domain } : {}) }), { onDone: onSaved });
    } catch (err) {
      setErrors(err.fields || {});
      setError(err.fields && Object.keys(err.fields).length ? null : err);
    }
  };
  return html`<form class="form card" onSubmit=${submit}>
    <${Notice} tone="info">Saving redeploys the app from the current catalog template; generated secrets stay the same.<//>
    <${Notice} error=${error} />
    ${a.port ? html`<${Field} label="Port" required error=${errors.port}>
      <input class="input" type="number" min="1" max="65535" required value=${port} onInput=${e => setPort(e.target.value)} /><//>` : null}
    ${exposable || a.domain ? html`<${DomainField} value=${domain} onChange=${setDomain} error=${errors.domain} />` : null}
    <${AppForm} fields=${fields} values=${values} errors=${errors} onChange=${setValues} />
    <div class="form-actions"><button class="button button-primary">Save and redeploy</button></div>
  </form>`;
}
