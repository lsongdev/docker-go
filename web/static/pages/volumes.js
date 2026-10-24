import { html, useMemo } from '../lib/html.js';
import { api } from '../lib/api.js';
import { href, navigate, setQuery } from '../lib/router.js';
import { useResource, useDockerEvents, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, PruneButton, toast, toastError, confirmDanger } from '../lib/ui.js';
import { ago } from '../lib/format.js';

export default function Volumes({ query }) {
  const volumes = useResource(signal => api.volumes({ signal }));
  useDockerEvents(['volume', 'container'], volumes.reload);
  const [busy, run] = useBusy(toastError);
  const search = query.q || '';
  const filtered = useMemo(() => (volumes.data || []).filter(v =>
    !search || `${v.name} ${v.project || ''}`.toLowerCase().includes(search.toLowerCase())), [volumes.data, search]);
  const remove = v => {
    if (!confirmDanger(`删除存储卷 ${v.name}？卷中的数据将永久丢失。`)) return;
    run(v.name, async () => {
      await api.removeVolume(v.name);
      toast(`已删除 ${v.name}`, 'success');
      volumes.reload();
    });
  };
  return html`<${Page} title="存储卷" actions=${html`
    <${PruneButton} title="清理存储卷" description="删除未被任何容器使用的匿名卷。数据将永久丢失。"
      allLabel="同时删除未使用的命名卷" prune=${api.pruneVolumes} onDone=${volumes.reload} />
    <button class="btn btn-primary" onClick=${() => navigate('/volumes/new')}>创建存储卷</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="搜索名称或项目" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
    </div>
    <${Resource} resource=${{ ...volumes, data: volumes.data && filtered }} empty="没有存储卷">${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>名称</th><th>驱动</th><th>使用</th><th>挂载点</th><th>创建</th><th class="actions">操作</th></tr></thead>
        <tbody>${list.map(v => html`<tr key=${v.name}>
          <td><span class="cell-main mono">${v.name.length > 40 ? v.name.slice(0, 12) + '…' : v.name}</span>
            ${v.project ? html`<div class="cell-sub"><a href=${href(`/stacks/${v.project}`)}>${v.project}</a></div>` : null}</td>
          <td>${v.driver}</td>
          <td>${v.containers.length ? html`<${Badge} tone="blue" title=${v.containers.join('\n')}>${v.containers.join(', ')}<//>` : html`<span class="muted">未使用</span>`}</td>
          <td class="truncate mono" style="max-width:280px" title=${v.mountpoint}>${v.mountpoint}</td>
          <td class="nowrap">${ago(v.created_at)}</td>
          <td class="actions"><button class="btn btn-sm btn-danger" disabled=${busy[v.name]} onClick=${() => remove(v)}>删除</button></td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
  <//>`;
}
