import { html, useEffect } from '../lib/html.js';
import { api } from '../lib/api.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, openJob, onJobsChanged } from '../lib/ui.js';
import { ago, datetime } from '../lib/format.js';

function duration(job) {
  const end = job.finished_at ? new Date(job.finished_at) : new Date();
  const s = Math.round((end - new Date(job.started_at)) / 1000);
  return s < 60 ? `${s} 秒` : `${Math.floor(s / 60)} 分 ${s % 60} 秒`;
}

export default function Jobs() {
  const jobs = useResource(signal => api.jobs({ signal }));
  useEffect(() => onJobsChanged(jobs.reload), []);
  useEffect(() => {
    if (!jobs.data?.some(j => j.status === 'running')) return;
    const t = setTimeout(jobs.reload, 2000);
    return () => clearTimeout(t);
  }, [jobs.data]);
  return html`<${Page} title="任务" subtitle="安装、拉取、构建等耗时操作在后台运行，日志保留在内存中">
    <${Resource} resource=${jobs} empty="还没有任务">${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>任务</th><th>状态</th><th>开始</th><th>耗时</th><th class="actions"></th></tr></thead>
        <tbody>${list.map(j => html`<tr key=${j.id}>
          <td><div class="cell-main">${j.title}</div>${j.error ? html`<div class="cell-sub" style="color:var(--danger)">${j.error}</div>` : null}</td>
          <td><${Status} state=${j.status} /></td>
          <td class="nowrap" title=${datetime(j.started_at)}>${ago(j.started_at)}</td>
          <td class="nowrap">${duration(j)}</td>
          <td class="actions"><button class="btn btn-sm" onClick=${() => openJob(j.id, jobs.reload)}>日志</button></td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
  <//>`;
}
