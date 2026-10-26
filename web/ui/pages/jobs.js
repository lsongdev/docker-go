import { html, useEffect } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, Table } from '../lib/ui.js';
import { ago, datetime } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { openJob, onJobsChanged } from '../docker.js';

function duration(job) {
  const end = job.finished_at ? new Date(job.finished_at) : new Date();
  const s = Math.round((end - new Date(job.started_at)) / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export default function Jobs() {
  const { api } = usePanel();
  const jobs = useResource(signal => api.jobs({ signal }));
  useEffect(() => onJobsChanged(jobs.reload), []);
  useEffect(() => {
    if (!jobs.data?.some(j => j.status === 'running')) return;
    const t = setTimeout(jobs.reload, 2000);
    return () => clearTimeout(t);
  }, [jobs.data]);
  return html`<${Page} title="Jobs" subtitle="Long operations such as installs, pulls and builds run in the background; their logs stay in memory">
    <${Resource} resource=${jobs} empty="No jobs yet">${list => html`
      <${Table}>
        <thead><tr><th>Job</th><th>Status</th><th>Started</th><th>Duration</th><th class="actions"></th></tr></thead>
        <tbody>${list.map(j => html`<tr key=${j.id}>
          <td><div class="cell-main">${j.title}</div>${j.error ? html`<div class="cell-sub" style="color:var(--danger)">${j.error}</div>` : null}</td>
          <td><${Status} state=${j.status} /></td>
          <td class="nowrap" title=${datetime(j.started_at)}>${ago(j.started_at)}</td>
          <td class="nowrap">${duration(j)}</td>
          <td class="actions"><button class="button button-small" onClick=${() => openJob(api, j.id, jobs.reload)}>Log</button></td>
        </tr>`)}</tbody>
      <//>`}
    <//>
  <//>`;
}
