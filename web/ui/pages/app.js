import { html, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, KV, Tabs, Field, Notice, Badge, ConfirmButton } from '../lib/ui.js';
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
    subtitle=${a ? html`<${Status} state=${a.status} /> 版本 ${a.version}` : null}
    actions=${a ? html`
      ${a.url && a.status === 'running' ? html`<a class="btn btn-primary" href=${a.url} target="_blank" rel="noopener">打开 ↗</a>` : null}
      ${running
        ? html`<button class="btn" onClick=${() => act('restart')}>重启</button><button class="btn" onClick=${() => act('stop')}>停止</button>`
        : html`<button class="btn" onClick=${() => act('start')}>启动</button>`}
      ${a.update_available ? html`<button class="btn" onClick=${update}>更新到 ${a.latest_version}</button>` : null}
      <${ConfirmButton} label="卸载" title=${`卸载 ${a.app?.name || a.id}`} confirmLabel="卸载"
        description="停止并删除应用的容器和网络。" checkbox="保留数据目录和数据卷，以便以后重新安装" onConfirm=${uninstall} />
      <a class="btn" href=${href('/apps')}>返回</a>` : null}>
    <${Resource} resource=${app}>${a => html`
      ${a.app ? null : html`<${Notice} tone="warn">此应用已从应用目录中移除，仍可启动、停止和卸载，但无法更新或修改设置。<//>`}
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['overview', '概览'], ['settings', '设置']]} />
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
        ['访问地址', a.url ? html`<a href=${a.url} target="_blank" rel="noopener">${a.url}</a>` : null],
        ['域名', a.domain || null],
        ['端口', a.port || null],
        ['版本', html`${a.version} ${a.update_available ? html`<${Badge} tone="blue">最新 ${a.latest_version}<//>` : null}`],
        ['安装于', datetime(a.installed_at)],
        ['更新于', datetime(a.updated_at)],
        ['目录', html`<code>${a.dir}</code>`],
        ['Compose', html`<a href=${href(`/stacks/${a.id}`)}>${a.id}</a>`],
      ]} />
    </div>
    <div>
      <div class="card"><h2>容器</h2>
        ${a.containers.length ? html`<table class="table"><tbody>${a.containers.map(c => html`<tr key=${c.id}>
          <td><a href=${href(`/containers/${c.id}`)}>${c.name}</a><div class="cell-sub">${c.image}</div></td>
          <td><${Status} state=${c.state} title=${c.status} /></td>
        </tr>`)}</tbody></table>` : html`<span class="muted">没有容器，点击"启动"创建</span>`}
      </div>
      ${secrets.length ? html`<div class="card"><h2>凭据</h2>
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
    <button class="btn btn-sm btn-ghost" onClick=${() => setShown(s => !s)}>${shown ? '隐藏' : '显示'}</button>
    <button class="btn btn-sm btn-ghost" onClick=${copy}>复制</button>
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
  if (!a.app) return html`<${Notice} tone="info">应用已从目录移除，无法修改设置。<//>`;
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
    <${Notice} tone="info">保存后将使用当前目录中的模板重新部署应用；自动生成的密钥保持不变。<//>
    <${Notice} error=${error} />
    ${a.port ? html`<${Field} label="访问端口" required error=${errors.port}>
      <input class="input" type="number" min="1" max="65535" required value=${port} onInput=${e => setPort(e.target.value)} /><//>` : null}
    ${exposable || a.domain ? html`<${DomainField} value=${domain} onChange=${setDomain} error=${errors.domain} />` : null}
    <${AppForm} fields=${fields} values=${values} errors=${errors} onChange=${setValues} />
    <div class="form-actions"><button class="btn btn-primary">保存并重新部署</button></div>
  </form>`;
}
