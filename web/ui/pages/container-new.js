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
  return html`<${Page} title="创建容器" actions=${html`<a class="btn" href=${href('/containers')}>返回</a>`}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <div class="form-grid">
        <${Field} label="镜像" required hint="例如 nginx:alpine，本地不存在时会自动拉取">
          <input class="input" required value=${form.image} onInput=${set('image')} placeholder="nginx:alpine" /><//>
        <${Field} label="容器名称" hint="留空则自动生成">
          <input class="input" value=${form.name} onInput=${set('name')} pattern="[a-zA-Z0-9][a-zA-Z0-9_.\\-]*" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="端口映射" hint="主机端口:容器端口[/协议]，如 8080:80 或 127.0.0.1:5432:5432">
          <${ListInput} values=${form.ports} onChange=${setList('ports')} placeholder="8080:80" /><//>
        <${Field} label="挂载" hint="主机路径或卷名:容器路径[:ro]">
          <${ListInput} values=${form.volumes} onChange=${setList('volumes')} placeholder="/srv/data:/data" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="环境变量" hint="KEY=VALUE">
          <${ListInput} values=${form.env} onChange=${setList('env')} placeholder="TZ=Asia/Shanghai" /><//>
        <${Field} label="标签" hint="KEY=VALUE">
          <${ListInput} values=${form.labels} onChange=${setList('labels')} placeholder="com.example.role=web" /><//>
      </div>
      <div class="form-grid">
        <${Field} label="重启策略">
          <select class="input" value=${form.restart} onChange=${set('restart')}>
            <option value="no">不自动重启</option><option value="always">总是</option>
            <option value="unless-stopped">除非手动停止</option><option value="on-failure">失败时</option>
          </select><//>
        <${Field} label="网络">
          <select class="input" value=${form.network} onChange=${set('network')}>
            <option value="">默认 (bridge)</option>
            ${(networks.data || []).filter(n => n.name !== 'bridge').map(n => html`<option value=${n.name}>${n.name}</option>`)}
          </select><//>
        <${Field} label="拉取策略">
          <select class="input" value=${form.pull} onChange=${set('pull')}>
            <option value="missing">本地缺失时拉取</option><option value="always">总是拉取最新</option><option value="never">从不拉取</option>
          </select><//>
      </div>
      <details>
        <summary class="field-label">高级选项</summary>
        <div class="form-grid" style="margin-top:12px">
          <${Field} label="命令" hint="覆盖镜像的 CMD，支持引号"><input class="input mono" value=${form.command} onInput=${set('command')} /><//>
          <${Field} label="入口点" hint="覆盖镜像的 ENTRYPOINT"><input class="input mono" value=${form.entrypoint} onInput=${set('entrypoint')} /><//>
          <${Field} label="工作目录"><input class="input" value=${form.working_dir} onInput=${set('working_dir')} /><//>
          <${Field} label="用户"><input class="input" value=${form.user} onInput=${set('user')} placeholder="1000:1000" /><//>
        </div>
        <div class="form-actions" style="margin-top:12px">
          <label class="check"><input type="checkbox" checked=${form.tty} onChange=${set('tty')} />分配 TTY</label>
          <label class="check"><input type="checkbox" checked=${form.privileged} onChange=${set('privileged')} />特权模式</label>
        </div>
      </details>
      <div class="form-actions">
        <button class="btn btn-primary" disabled=${submitting}>创建</button>
        <label class="check"><input type="checkbox" checked=${form.start} onChange=${set('start')} />创建后立即启动</label>
        <span class="spacer"></span>
        <a class="btn" href=${href('/containers')}>取消</a>
      </div>
    </form>
  <//>`;
}
