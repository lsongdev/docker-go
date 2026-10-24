import { html, useState } from '../lib/html.js';
import { Page, Field, Notice, toast } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';
import { startJob } from '../docker.js';

const template = `services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "\${WEB_PORT:-8080}:80"
`;

export default function StackNew() {
  const { api, href, navigate } = usePanel();
  const [name, setName] = useState('');
  const [compose, setCompose] = useState(template);
  const [env, setEnv] = useState('WEB_PORT=8080\n');
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const save = async deploy => {
    setError(null);
    setSaving(true);
    try {
      await api.createStack({ name, compose, env });
      toast(`已创建 ${name}`, 'success');
      if (deploy) await startJob(api, () => api.stackAction(name, 'up')).catch(() => {});
      navigate(`/stacks/${name}`);
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };
  return html`<${Page} title="新建 Stack" actions=${html`<a class="btn" href=${href('/stacks')}>返回</a>`}>
    <form class="form card" onSubmit=${e => { e.preventDefault(); save(true); }}>
      <${Notice} error=${error} />
      <${Field} label="名称" required hint="小写字母、数字、- 和 _，用作 compose 项目名">
        <input class="input" required pattern="[a-z0-9][a-z0-9_\\-]*" value=${name} onInput=${e => setName(e.target.value)} /><//>
      <div class="grid-2">
        <${Field} label="compose.yaml"><textarea class="input code" spellcheck="false" value=${compose} onInput=${e => setCompose(e.target.value)}></textarea><//>
        <${Field} label=".env" hint="compose 文件中 \${VAR} 引用的变量"><textarea class="input code" spellcheck="false" value=${env} onInput=${e => setEnv(e.target.value)}></textarea><//>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" disabled=${saving}>保存并部署</button>
        <button type="button" class="btn" disabled=${saving} onClick=${() => save(false)}>仅保存</button>
        <a class="btn" href=${href('/stacks')}>取消</a>
      </div>
    </form>
  <//>`;
}
