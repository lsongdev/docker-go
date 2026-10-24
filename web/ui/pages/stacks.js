import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Badge } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, startJob } from '../docker.js';

export default function Stacks() {
  const { api, href, navigate } = usePanel();
  const stacks = useResource(signal => api.stacks({ signal }));
  useDockerEvents(['container'], stacks.reload, 800);
  const act = (name, action) => startJob(api, () => api.stackAction(name, action), { onDone: stacks.reload }).catch(() => {});
  return html`<${Page} title="Compose" subtitle="使用 docker compose 管理多容器应用"
    actions=${html`<button class="btn btn-primary" onClick=${() => navigate('/stacks/new')}>新建 Stack</button>`}>
    <${Resource} resource=${stacks} empty="还没有 Compose 项目">${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>名称</th><th>状态</th><th>容器</th><th>配置文件</th><th class="actions">操作</th></tr></thead>
        <tbody>${list.map(s => html`<tr key=${s.name}>
          <td><a class="cell-main" href=${href(`/stacks/${s.name}`)}>${s.name}</a>
            ${s.managed ? null : html` <${Badge} title="由面板外部创建，只能执行生命周期操作">外部<//>`}</td>
          <td><${Status} state=${s.status} /></td>
          <td class="cell-sub">${s.containers || '—'}</td>
          <td class="truncate mono" style="max-width:320px" title=${s.config_files.join('\n')}>${s.config_files[0] || '—'}</td>
          <td class="actions"><span class="btn-group">
            ${s.status === 'running' || s.status === 'partial'
              ? html`<button class="btn btn-sm" onClick=${() => act(s.name, 'restart')}>重启</button>
                     <button class="btn btn-sm" onClick=${() => act(s.name, 'stop')}>停止</button>`
              : html`<button class="btn btn-sm" onClick=${() => act(s.name, 'up')}>启动</button>`}
          </span></td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
  <//>`;
}
