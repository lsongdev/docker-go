import { html, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Field, ListInput, Notice } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { startJob } from '../docker.js';

const initial = {
  name: '', image: '', command: '', entrypoint: '', env: [], ports: [], volumes: [], labels: [],
  network: '', restart: 'unless-stopped', working_dir: '', user: '', tty: false, privileged: false, pull: 'missing', start: true,
};

export default function ContainerNew({ query }) {
  const { api, href, navigate } = usePanel();
  const [form, setForm] = useState({ ...initial, image: query.image || '' });
  const [error, setError] = useState(null);
  const [submitting, setSubmitting] = useState(false);
  const networks = useResource(signal => api.networks({ signal }));
  const set = key => e => setForm(f => ({ ...f, [key]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const setList = key => value => setForm(f => ({ ...f, [key]: value }));
  const submit = async e => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    const body = { ...form, env: form.env.filter(Boolean), ports: form.ports.filter(Boolean), volumes: form.volumes.filter(Boolean), labels: form.labels.filter(Boolean) };
    try {
      await startJob(api, () => api.createContainer(body), {
        onDone: job => job.status === 'succeeded' && navigate(form.name ? `/containers/${form.name}` : '/containers'),
      });
    } catch (err) {
      setError(err);
    } finally {
      setSubmitting(false);
    }
  };
  return html`<${Page} title="New container" back=${{ href: href('/containers'), label: 'Containers' }}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <div class="form-grid">
        <${Field} label="Image" required hint="Such as nginx:alpine; pulled if not present">
          <input class="input" required value=${form.image} onInput=${set('image')} placeholder="nginx:alpine" /><//>
        <${Field} label="Name" hint="Empty for a generated one">
          <input class="input" value=${form.name} onInput=${set('name')} pattern="[a-zA-Z0-9][a-zA-Z0-9_.\\-]*" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="Ports" hint="host:container[/protocol], such as 8080:80 or 127.0.0.1:5432:5432">
          <${ListInput} values=${form.ports} onChange=${setList('ports')} placeholder="8080:80" /><//>
        <${Field} label="Mounts" hint="host path or volume:container path[:ro]">
          <${ListInput} values=${form.volumes} onChange=${setList('volumes')} placeholder="/srv/data:/data" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="Environment" hint="KEY=VALUE">
          <${ListInput} values=${form.env} onChange=${setList('env')} placeholder="TZ=Asia/Shanghai" /><//>
        <${Field} label="Labels" hint="KEY=VALUE">
          <${ListInput} values=${form.labels} onChange=${setList('labels')} placeholder="com.example.role=web" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="Restart policy">
          <select class="input" value=${form.restart} onChange=${set('restart')}>
            <option value="no">Never</option><option value="always">Always</option>
            <option value="unless-stopped">Unless stopped</option><option value="on-failure">On failure</option>
          </select><//>
        <${Field} label="Network">
          <select class="input" value=${form.network} onChange=${set('network')}>
            <option value="">Default (bridge)</option>
            ${(networks.data || []).filter(n => n.name !== 'bridge').map(n => html`<option value=${n.name}>${n.name}</option>`)}
          </select><//>
        <${Field} label="Pull">
          <select class="input" value=${form.pull} onChange=${set('pull')}>
            <option value="missing">If missing</option><option value="always">Always</option><option value="never">Never</option>
          </select><//>
      </div>
      <details>
        <summary class="field-label">Advanced</summary>
        <div class="form-grid" style="margin-top:12px">
          <${Field} label="Command" hint="Overrides the image CMD; quotes work"><input class="input mono" value=${form.command} onInput=${set('command')} /><//>
          <${Field} label="Entrypoint" hint="Overrides the image ENTRYPOINT"><input class="input mono" value=${form.entrypoint} onInput=${set('entrypoint')} /><//>
          <${Field} label="Working directory"><input class="input" value=${form.working_dir} onInput=${set('working_dir')} /><//>
          <${Field} label="User"><input class="input" value=${form.user} onInput=${set('user')} placeholder="1000:1000" /><//>
        </div>
        <div class="form-actions" style="margin-top:12px">
          <label class="field-check"><input type="checkbox" checked=${form.tty} onChange=${set('tty')} />Allocate a TTY</label>
          <label class="field-check"><input type="checkbox" checked=${form.privileged} onChange=${set('privileged')} />Privileged</label>
        </div>
      </details>
      <div class="form-actions">
        <button class="button button-primary" disabled=${submitting}>Create</button>
        <label class="field-check"><input type="checkbox" checked=${form.start} onChange=${set('start')} />Start after creating</label>
        <span class="spacer"></span>
        <a class="button" href=${href('/containers')}>Cancel</a>
      </div>
    </form>
  <//>`;
}
