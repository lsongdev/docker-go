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
      toast(`Created ${name}`, 'success');
      if (deploy) await startJob(api, () => api.stackAction(name, 'up')).catch(() => {});
      navigate(`/stacks/${name}`);
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };
  return html`<${Page} title="New stack" back=${{ href: href('/stacks'), label: 'Compose' }}>
    <form class="form card" onSubmit=${e => { e.preventDefault(); save(true); }}>
      <${Notice} error=${error} />
      <${Field} label="Name" required hint="Lowercase letters, digits, - and _; the compose project name">
        <input class="input" required pattern="[a-z0-9][a-z0-9_\\-]*" value=${name} onInput=${e => setName(e.target.value)} /><//>
      <div class="grid-2">
        <${Field} label="compose.yaml"><textarea class="input code" spellcheck="false" value=${compose} onInput=${e => setCompose(e.target.value)}></textarea><//>
        <${Field} label=".env" hint="Variables the compose file refers to as \${VAR}"><textarea class="input code" spellcheck="false" value=${env} onInput=${e => setEnv(e.target.value)}></textarea><//>
      </div>
      <div class="form-actions">
        <button class="button button-primary" disabled=${saving}>Save and deploy</button>
        <button type="button" class="button" disabled=${saving} onClick=${() => save(false)}>Save only</button>
        <a class="button" href=${href('/stacks')}>Cancel</a>
      </div>
    </form>
  <//>`;
}
