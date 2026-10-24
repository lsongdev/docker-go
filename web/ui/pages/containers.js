import { html, useMemo } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Status, toast, toastError, confirmDanger } from '../lib/ui.js';
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
    toast(`${c.name}：${label}`, 'success');
    onChange?.();
  });
  const remove = () => {
    if (!confirmDanger(`删除容器 ${c.name}？${c.state === 'running' ? '\n容器正在运行，将被强制停止。' : ''}`)) return;
    run('remove', async () => {
      await api.removeContainer(c.id, { force: true });
      toast(`已删除 ${c.name}`, 'success');
      onChange?.('removed');
    });
  };
  const size = compact ? 'btn btn-sm' : 'btn';
  const running = c.state === 'running', paused = c.state === 'paused';
  return html`<span class="btn-group">
    ${running ? html`
      <button class=${size} disabled=${busy.stop} onClick=${() => act('stop', '已停止')}>停止</button>
      <button class=${size} disabled=${busy.restart} onClick=${() => act('restart', '已重启')}>重启</button>
      ${compact ? null : html`<button class=${size} disabled=${busy.pause} onClick=${() => act('pause', '已暂停')}>暂停</button>`}`
    : paused ? html`<button class=${size} disabled=${busy.unpause} onClick=${() => act('unpause', '已恢复')}>恢复</button>`
    : html`<button class=${size} disabled=${busy.start} onClick=${() => act('start', '已启动')}>启动</button>`}
    <button class=${`${size} btn-danger`} disabled=${busy.remove} onClick=${remove}>删除</button>
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
  return html`<${Page} title="容器" actions=${html`
    <${PruneButton} title="清理容器" description="删除所有已停止的容器。" prune=${api.pruneContainers} onDone=${containers.reload} />
    <button class="btn btn-primary" onClick=${() => navigate('/containers/new')}>创建容器</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="搜索名称、镜像、项目" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
      <select class="input" style="max-width:140px" value=${state} onChange=${e => setQuery({ state: e.target.value })}>
        <option value="">全部状态</option><option value="running">运行中</option><option value="stopped">未运行</option>
      </select>
    </div>
    <${Resource} resource=${{ ...containers, data: containers.data && filtered }}
      empty=${containers.data?.length ? '没有匹配的容器' : '还没有容器'}>${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>名称</th><th>镜像</th><th>状态</th><th>端口</th><th>创建</th><th class="actions">操作</th></tr></thead>
        <tbody>${list.map(c => html`<tr key=${c.id}>
          <td><a class="cell-main" href=${href(`/containers/${c.id}`)}>${c.name}</a>
            <div class="cell-sub">${shortId(c.id)}${c.project ? html` · <a href=${href(`/stacks/${c.project}`)}>${c.project}</a>` : null}</div></td>
          <td class="truncate" style="max-width:260px" title=${c.image}>${c.image}</td>
          <td><${Status} state=${c.state} title=${c.status} /><div class="cell-sub">${c.status}</div></td>
          <td><div class="tags">${c.ports.filter(p => p.public).map(p => html`<code>${portLabel(p)}</code>`)}</div></td>
          <td class="nowrap">${ago(c.created)}</td>
          <td class="actions"><${ContainerActions} c=${c} compact onChange=${containers.reload} /></td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
  <//>`;
}
