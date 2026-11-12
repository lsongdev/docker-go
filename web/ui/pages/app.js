import { html, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { PageHeader, KeyValue, Tabs, Field, Badge, ConfirmButton } from '@lsongdev/scripts/react/components/index.js';
import { Status } from '../lib/ui.js';
import { datetime } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, startJob, Resource, Notice } from '../docker.js';
import { AppIcon } from './store.js';
import { AppForm, DomainField, initialValues, InstallForm } from '../components/app-form.js';

export default function App({ params, query }) {
  return html`<${AppDetail} key=${params.id} id=${params.id} query=${query} />`;
}

function AppDetail({ id, query }) {
  const { api, navigate, setQuery } = usePanel();
  const detail = useResource(signal => api.app(id, { signal }), [id], { interval: 5000 });
  useDockerEvents(['container'], detail.reload, 800);
  const [submitting, setSubmitting] = useState(false);
  const d = detail.data;
  const a = d?.app;
  const metadata = d?.catalog;
  const secrets = (a?.app?.form_fields || []).filter(f => f.type === 'random' || f.type === 'password');
  const tab = query.tab === 'settings' || (query.tab === 'credentials' && secrets.length) ? query.tab : 'readme';
  const act = action => startJob(api, () => api.appAction(id, action), { onDone: detail.reload }).catch(() => {});
  const update = () => startJob(api, () => api.updateApp(id), { onDone: detail.reload }).catch(() => {});
  const uninstall = keepData => startJob(api, () => api.uninstallApp(id, keepData), {
    onDone: job => {
      if (job.status !== 'succeeded') return detail.reload();
      if (!metadata) return navigate('/apps');
      detail.reload();
      setQuery({ tab: '' });
    },
  }).catch(() => {});
  const running = a && (a.status === 'running' || a.status === 'partial');
  return html`<${PageHeader} title=${metadata?.name || a?.app?.name || id}
    actions=${!d ? null : !a ? html`<button class="button button-primary" type="button" onClick=${() => setQuery({ tab: 'settings' })} disabled=${!d.supported || submitting}>${submitting ? 'Installing…' : 'Install'}</button>` : html`
      ${a.url && a.status === 'running' ? html`<a class="button button-small button-primary" href=${a.url} target="_blank" rel="noopener">Open ↗</a>` : null}
      ${running
        ? html`<button class="button" disabled=${submitting} onClick=${() => act('restart')}>Restart</button><button class="button" disabled=${submitting} onClick=${() => act('stop')}>Stop</button>`
        : html`<button class="button" disabled=${submitting} onClick=${() => act('start')}>Start</button>`}
      ${a.update_available ? html`<button class="button" onClick=${update}>Update to ${a.latest_version}</button>` : null}
      <${ConfirmButton} className="button" label="Uninstall" title=${`Uninstall ${metadata?.name || id}`} confirmLabel="Uninstall"
        description="Stop the app and delete its containers and networks." checkbox="Keep the data directory and volumes for a later reinstall" onConfirm=${uninstall} />`} />
    <${Resource} resource=${detail}>${d => html`
      ${d.installed && !d.catalog ? html`<${Notice} variant="warning">This app was removed from the catalog. It can still be started, stopped and uninstalled, but not updated or reconfigured.<//>` : null}
      <${AppSummary} id=${id} metadata=${d.catalog} a=${d.app} />
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t === 'readme' ? '' : t })}
        tabs=${[['readme', 'README'], ['settings', d.installed ? 'Settings' : 'Install'], ...(secrets.length ? [['credentials', 'Credentials']] : [])]} />
      <div hidden=${tab !== 'readme'}>
        <div class="card"><div class="readme">${d.readme || d.catalog?.description || 'No README available.'}</div></div>
      </div>
      <div hidden=${tab !== 'settings'}>
        ${d.installed ? html`<${Settings} key=${id} a=${d.app} exposable=${d.exposable} onSaved=${detail.reload} />`
          : d.supported ? html`<${InstallForm} key=${id} app=${d.catalog} exposable=${d.exposable} submitting=${submitting} setSubmitting=${setSubmitting} onDone=${detail.reload} />`
          : html`<div class="card"><${Notice} variant="warning">This app does not support this architecture (${d.arch})<//></div>`}
      </div>
      ${secrets.length ? html`<div hidden=${tab !== 'credentials'}><div class="card"><h2>Credentials</h2>
        <${KeyValue} items=${secrets.map(f => [f.label, html`<${Secret} value=${d.app.values[f.env_variable]} />`])} />
      </div></div>` : null}
    `}<//>`;
}

function AppSummary({ id, metadata, a }) {
  const { href } = usePanel();
  return html`<div class="card app-summary">
    <div class="app-head">
      <${AppIcon} id=${id} large />
      <div class="app-identity"><h2>${metadata?.name || a?.app?.name || id}</h2>
        ${a ? html`<${Status} state=${a.status} />` : null}
        <div class="muted">${metadata?.short_desc}</div>
      </div>
    </div>
    <${KeyValue} items=${[
      ...(a ? [
        ['URL', a.url ? html`<a href=${a.url} target="_blank" rel="noopener">${a.url}</a>` : null],
        ['Domain', a.domain || null], ['Port', a.port || null],
        ['Version', html`${a.version} ${a.update_available ? html`<${Badge} variant="info">Latest ${a.latest_version}<//>` : null}`],
        ['Compose', html`<a href=${href(`/stacks/${a.id}`)}>${a.id}</a>`],
        ['Containers', html`<${Containers} a=${a} />`],
      ] : [['Version', metadata?.version], ['Default port', metadata?.port || null]]),
      ['Author', metadata?.author], ['Categories', metadata?.categories?.join(', ')],
      ['Architectures', metadata?.supported_architectures?.join(', ')],
      ['Source', metadata?.source ? html`<a href=${metadata.source} target="_blank" rel="noopener noreferrer">${metadata.source}</a>` : null],
      ['Website', metadata?.website ? html`<a href=${metadata.website} target="_blank" rel="noopener noreferrer">${metadata.website}</a>` : null],
      ...(a ? [
        ['Installed', datetime(a.installed_at)], ['Updated', datetime(a.updated_at)],
        ['Directory', html`<code>${a.dir}</code>`],
      ] : []),
    ]} />
  </div>`;
}

function Containers({ a }) {
  const { href } = usePanel();
  return a.containers.length ? html`<div class="app-containers">${a.containers.map(c => html`<div key=${c.id}>
    <div class="row-actions"><a href=${href(`/containers/${c.id}`)}>${c.name}</a><${Status} state=${c.state} title=${c.status} /></div>
    <div class="cell-sub">${c.image}</div>
  </div>`)}</div>` : html`<span class="muted">No containers; click Start to create them</span>`;
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
  if (!a.app) return html`<${Notice} variant="info">The app was removed from the catalog, so its settings cannot change.<//>`;
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
    <${Notice} variant="info">Saving redeploys the app from the current catalog template; generated secrets stay the same.<//>
    <${Notice} error=${error} />
    ${a.port ? html`<${Field} label="Port" required error=${errors.port}>
      <input class="input" type="number" min="1" max="65535" required value=${port} onInput=${e => setPort(e.target.value)} /><//>` : null}
    ${exposable || a.domain ? html`<${DomainField} value=${domain} onChange=${setDomain} error=${errors.domain} />` : null}
    <${AppForm} fields=${fields} values=${values} errors=${errors} onChange=${setValues} />
    <div class="form-actions"><button class="button button-primary">Save and redeploy</button></div>
  </form>`;
}
