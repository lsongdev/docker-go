import { html, useState } from '../lib/html.js';
import { api } from '../lib/api.js';
import { href, navigate } from '../lib/router.js';
import { Page, Field, Notice, Tabs, ListInput, startJob } from '../lib/ui.js';

const sample = `FROM alpine:3.20
RUN apk add --no-cache curl
CMD ["sh"]
`;

export default function ImageBuild() {
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
      await startJob(() => api.buildImage({ ...form, dockerfile, build_args }), {
        onDone: job => job.status === 'succeeded' && navigate('/images'),
      });
    } catch (err) { setError(err); }
  };
  const switchSource = source => setForm(f => ({ ...f, source, dockerfile: source === 'dockerfile' ? sample : '' }));
  return html`<${Page} title="构建镜像" actions=${html`<a class="btn" href=${href('/images')}>返回</a>`}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <${Field} label="镜像标签" required hint="例如 myapp:1.0">
        <input class="input" required value=${form.tag} onInput=${set('tag')} placeholder="myapp:latest" /><//>
      <${Tabs} active=${form.source} onChange=${switchSource} tabs=${[['dockerfile', 'Dockerfile'], ['directory', '服务器目录'], ['git', 'Git 仓库']]} />
      ${form.source === 'dockerfile' ? html`
        <${Field} label="Dockerfile"><textarea class="input code" spellcheck="false" value=${form.dockerfile} onInput=${set('dockerfile')}></textarea><//>` : null}
      ${form.source === 'directory' ? html`<div class="form-grid">
        <${Field} label="构建目录" required hint="服务器上的绝对路径，作为构建上下文；支持 .dockerignore">
          <input class="input mono" required value=${form.directory} onInput=${set('directory')} placeholder="/srv/myapp" /><//>
        <${Field} label="Dockerfile 路径" hint="相对于构建目录，默认 Dockerfile">
          <input class="input mono" value=${form.dockerfile} onInput=${set('dockerfile')} placeholder="Dockerfile" /><//>
      </div>` : null}
      ${form.source === 'git' ? html`<div class="form-grid">
        <${Field} label="仓库地址" required><input class="input mono" required value=${form.repository} onInput=${set('repository')} placeholder="https://github.com/user/repo.git" /><//>
        <${Field} label="分支或标签" hint="留空使用默认分支"><input class="input" value=${form.ref} onInput=${set('ref')} /><//>
        <${Field} label="Dockerfile 路径" hint="相对于仓库根目录"><input class="input mono" value=${form.dockerfile} onInput=${set('dockerfile')} placeholder="Dockerfile" /><//>
      </div>` : null}
      <${Field} label="构建参数" hint="KEY=VALUE，对应 Dockerfile 中的 ARG"><${ListInput} values=${args} onChange=${setArgs} placeholder="VERSION=1.0" /><//>
      <div class="form-actions">
        <label class="check"><input type="checkbox" checked=${form.pull} onChange=${set('pull')} />拉取最新基础镜像</label>
        <label class="check"><input type="checkbox" checked=${form.no_cache} onChange=${set('no_cache')} />不使用缓存</label>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary">开始构建</button>
        <a class="btn" href=${href('/images')}>取消</a>
      </div>
    </form>
  <//>`;
}
