import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, KV, toast, toastError, confirmDanger } from '../lib/ui.js';
import { datetime, shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents } from '../docker.js';

export default function Network({ params }) {
  const { api, href, navigate } = usePanel();
  const network = useResource(signal => api.network(params.id, { signal }), [params.id]);
  useDockerEvents(['network'], network.reload);
  const n = network.data;
  const remove = async () => {
    if (!confirmDanger(`删除网络 ${n.name}？`)) return;
    try {
      await api.removeNetwork(n.id);
      toast('网络已删除', 'success');
      navigate('/networks');
    } catch (err) { toastError(err); }
  };
  return html`<${Page} title=${n?.name || shortId(params.id)} subtitle=${n ? html`<code>${n.id}</code>` : null}
    actions=${html`${n && !n.builtin ? html`<button class="btn btn-danger" onClick=${remove}>删除</button>` : null}
      <a class="btn" href=${href('/networks')}>返回</a>`}>
    <${Resource} resource=${network}>${n => html`
      <div class="grid-2">
        <div class="card"><h2>配置</h2>
          <${KV} items=${[
            ['驱动', n.driver],
            ['范围', n.scope],
            ['子网', n.subnets.map(s => html`<div><code>${s.subnet}</code>${s.gateway ? html` 网关 <code>${s.gateway}</code>` : null}${s.ip_range ? html` 范围 <code>${s.ip_range}</code>` : null}</div>`)],
            ['内部网络', n.internal ? '是' : '否'],
            ['可附加', n.attachable ? '是' : '否'],
            ['IPv6', n.ipv6 ? '启用' : '未启用'],
            ['Compose', n.project ? html`<a href=${href(`/stacks/${n.project}`)}>${n.project}</a>` : null],
            ['创建', datetime(n.created)],
          ]} />
        </div>
        <div class="card"><h2>已连接容器</h2>
          ${n.containers.length ? html`<table class="table"><tbody>${n.containers.map(c => html`<tr>
            <td><a href=${href(`/containers/${c.id}`)}>${c.name}</a></td>
            <td><code>${c.ipv4 || '—'}</code></td><td class="cell-sub">${c.mac}</td>
          </tr>`)}</tbody></table>` : html`<span class="muted">没有容器连接到此网络</span>`}
        </div>
      </div>`}
    <//>
  <//>`;
}
