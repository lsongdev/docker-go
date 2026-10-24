import { html } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, toast, toastError, confirmDanger } from '../lib/ui.js';
import { shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, PruneButton } from '../docker.js';

export default function Networks() {
  const { api, href, navigate } = usePanel();
  const networks = useResource(signal => api.networks({ signal }));
  useDockerEvents(['network', 'container'], networks.reload);
  const [busy, run] = useBusy(toastError);
  const remove = n => {
    if (!confirmDanger(`删除网络 ${n.name}？`)) return;
    run(n.id, async () => {
      await api.removeNetwork(n.id);
      toast(`已删除 ${n.name}`, 'success');
      networks.reload();
    });
  };
  return html`<${Page} title="网络" actions=${html`
    <${PruneButton} title="清理网络" description="删除未被任何容器使用的自定义网络。" prune=${api.pruneNetworks} onDone=${networks.reload} />
    <button class="btn btn-primary" onClick=${() => navigate('/networks/new')}>创建网络</button>`}>
    <${Resource} resource=${networks} empty="没有网络">${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>名称</th><th>驱动</th><th>子网</th><th>容器</th><th>属性</th><th class="actions">操作</th></tr></thead>
        <tbody>${list.map(n => html`<tr key=${n.id}>
          <td><a class="cell-main" href=${href(`/networks/${n.id}`)}>${n.name}</a><div class="cell-sub">${shortId(n.id)}</div></td>
          <td>${n.driver}</td>
          <td>${n.subnets.map(s => html`<div><code>${s.subnet}</code></div>`)}</td>
          <td>${n.containers.length || html`<span class="muted">0</span>`}</td>
          <td><div class="tags">
            ${n.builtin ? html`<${Badge}>内置<//>` : null}
            ${n.internal ? html`<${Badge} tone="amber">内部<//>` : null}
            ${n.ipv6 ? html`<${Badge}>IPv6<//>` : null}
            ${n.project ? html`<${Badge} tone="blue">${n.project}<//>` : null}
          </div></td>
          <td class="actions">${n.builtin ? null : html`<button class="btn btn-sm btn-danger" disabled=${busy[n.id]} onClick=${() => remove(n)}>删除</button>`}</td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
  <//>`;
}
