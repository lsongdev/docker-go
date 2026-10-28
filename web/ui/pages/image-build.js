import { html, useState } from '../lib/html.js';
import { Page, Field, Notice, Tabs, ListInput } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { startJob } from '../docker.js';

const sample = `FROM alpine:3.20
RUN apk add --no-cache curl
CMD ["sh"]
`;

export default function ImageBuild() {
  const { api, href, navigate } = usePanel();
  const [form, setForm] = useState({ tag: '', source: 'dockerfile', dockerfile: sample, directory: '', repository: '', ref: '', pull: false, no_cache: false });
  const [args, setArgs] = useState([]);
  const [error, setError] = useState(null);
  const set = key => e => setForm(f => ({ ...f, [key]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const submit = async e => {
    e.preventDefault();
    setError(null);
    const build_args = Object.fromEntries(args.filter(a => a.includes('=')).map(a => [a.slice(0, a.indexOf('=')), a.slice(a.indexOf('=') + 1)]));
    // In directory/git mode the field holds a path inside the context.
    const dockerfile = form.source === 'dockerfile' ? form.dockerfile : form.dockerfile.includes('\n') ? '' : form.dockerfile;
    try {
      await startJob(api, () => api.buildImage({ ...form, dockerfile, build_args }), {
        onDone: job => job.status === 'succeeded' && navigate('/images'),
      });
    } catch (err) { setError(err); }
  };
  const switchSource = source => setForm(f => ({ ...f, source, dockerfile: source === 'dockerfile' ? sample : '' }));
  return html`<${Page} title="Build image" back=${{ href: href('/images'), label: 'Images' }}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <${Field} label="Tag" required hint="Such as myapp:1.0">
        <input class="input" required value=${form.tag} onInput=${set('tag')} placeholder="myapp:latest" /><//>
      <${Tabs} active=${form.source} onChange=${switchSource} tabs=${[['dockerfile', 'Dockerfile'], ['directory', 'Server directory'], ['git', 'Git repository']]} />
      ${form.source === 'dockerfile' ? html`
        <${Field} label="Dockerfile"><textarea class="input code" spellcheck="false" value=${form.dockerfile} onInput=${set('dockerfile')}></textarea><//>` : null}
      ${form.source === 'directory' ? html`<div class="form-grid">
        <${Field} label="Directory" required hint="An absolute path on the server, used as the build context; .dockerignore applies">
          <input class="input mono" required value=${form.directory} onInput=${set('directory')} placeholder="/srv/myapp" /><//>
        <${Field} label="Dockerfile path" hint="Relative to the directory; Dockerfile by default">
          <input class="input mono" value=${form.dockerfile} onInput=${set('dockerfile')} placeholder="Dockerfile" /><//>
      </div>` : null}
      ${form.source === 'git' ? html`<div class="form-grid">
        <${Field} label="Repository URL" required><input class="input mono" required value=${form.repository} onInput=${set('repository')} placeholder="https://github.com/user/repo.git" /><//>
        <${Field} label="Branch or tag" hint="Empty for the default branch"><input class="input" value=${form.ref} onInput=${set('ref')} /><//>
        <${Field} label="Dockerfile path" hint="Relative to the repository root"><input class="input mono" value=${form.dockerfile} onInput=${set('dockerfile')} placeholder="Dockerfile" /><//>
      </div>` : null}
      <${Field} label="Build arguments" hint="KEY=VALUE, for the ARG instructions in the Dockerfile"><${ListInput} values=${args} onChange=${setArgs} placeholder="VERSION=1.0" /><//>
      <div class="form-actions">
        <label class="field-check"><input type="checkbox" checked=${form.pull} onChange=${set('pull')} />Pull newer base images</label>
        <label class="field-check"><input type="checkbox" checked=${form.no_cache} onChange=${set('no_cache')} />Do not use the cache</label>
      </div>
      <div class="form-actions">
        <button class="button button-primary">Build</button>
        <a class="button" href=${href('/images')}>Cancel</a>
      </div>
    </form>
  <//>`;
}
