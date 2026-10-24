import { html, useEffect, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Tabs, Field, Notice, Badge, ConfirmButton, toast } from '../lib/ui.js';
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
    actions=${s ? html`
      <button class="btn btn-primary" onClick=${() => act('up')}>部署</button>
      ${s.status !== 'inactive' ? html`
        <button class="btn" onClick=${() => act('restart')}>重启</button>
        <button class="btn" onClick=${() => act('stop')}>停止</button>
        <button class="btn" onClick=${() => act('down')}>下线</button>` : null}
      <button class="btn" onClick=${() => act('pull')}>拉取镜像</button>
      ${s.managed ? html`<${ConfirmButton} label="删除" title=${`删除 ${params.name}`} confirmLabel="删除"
        description="停止并删除该 Stack 的容器和网络，并移除项目目录。" checkbox="同时删除数据卷（数据将永久丢失）" onConfirm=${remove} />` : null}
      <a class="btn" href=${href('/stacks')}>返回</a>` : null}>
    <${Resource} resource=${stack}>${s => html`
      ${s.managed ? null : html`<${Notice} tone="info">这个项目不是由面板创建的，配置文件只读；可以执行部署、停止等操作。<//>`}
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['services', `服务 (${s.services.length})`], ['config', '配置']]} />
      ${tab === 'services' ? html`<${Services} services=${s.services} />` : html`<${Editor} stack=${s} onSaved=${stack.reload} />`}`}
    <//>
  <//>`;
}

function Services({ services }) {
  const { href } = usePanel();
  if (!services.length) return html`<div class="empty">尚未部署：点击"部署"创建容器</div>`;
  return html`<div class="table-wrap"><table class="table">
    <thead><tr><th>服务</th><th>容器</th><th>镜像</th><th>状态</th><th>端口</th></tr></thead>
    <tbody>${services.map(sv => html`<tr key=${sv.ID}>
      <td class="cell-main">${sv.Service}</td>
      <td><a href=${href(`/containers/${sv.ID}`)}>${sv.Name}</a></td>
      <td class="truncate" style="max-width:240px" title=${sv.Image}>${sv.Image}</td>
      <td><${Status} state=${sv.Health || sv.State} title=${sv.Status} /><div class="cell-sub">${sv.Status}</div></td>
      <td><div class="tags">${(sv.Publishers || []).filter(p => p.PublishedPort).map(p =>
        html`<code>${p.PublishedPort}→${p.TargetPort}/${p.Protocol}</code>`)}</div></td>
    </tr>`)}</tbody>
  </table></div>`;
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
      toast('配置已保存', 'success');
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
      <${Field} label=${html`${stack.config_files[0]?.split('/').pop() || 'compose.yaml'} ${readOnly ? html`<${Badge}>只读<//>` : null}`}>
        <textarea class="input code" spellcheck="false" readOnly=${readOnly} value=${compose} onInput=${e => setCompose(e.target.value)}></textarea><//>
      <${Field} label=".env"><textarea class="input code" spellcheck="false" readOnly=${readOnly} value=${env} onInput=${e => setEnv(e.target.value)}></textarea><//>
    </div>
    ${readOnly ? null : html`<div class="form-actions">
      <button class="btn btn-primary" disabled=${!dirty || saving} onClick=${() => save(true)}>保存并重新部署</button>
      <button class="btn" disabled=${!dirty || saving} onClick=${() => save(false)}>仅保存</button>
      ${dirty ? html`<button class="btn btn-ghost" onClick=${() => { setCompose(stack.compose); setEnv(stack.env); }}>撤销修改</button>` : null}
    </div>`}
  </div>`;
}
