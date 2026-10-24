import { html, useState } from '../lib/html.js';
import { Page, Field, Notice, toast } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';

export default function NetworkNew() {
  const { api, href, navigate } = usePanel();
  const [form, setForm] = useState({ name: '', driver: 'bridge', subnet: '', gateway: '', ip_range: '', internal: false, attachable: false, ipv6: false });
  const [error, setError] = useState(null);
  const set = key => e => setForm(f => ({ ...f, [key]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const submit = async e => {
    e.preventDefault();
    try {
      const n = await api.createNetwork(form);
      toast(`已创建 ${n.name}`, 'success');
      navigate(`/networks/${n.id}`);
    } catch (err) { setError(err); }
  };
  return html`<${Page} title="创建网络" actions=${html`<a class="btn" href=${href('/networks')}>返回</a>`}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <div class="form-grid">
        <${Field} label="名称" required><input class="input" required value=${form.name} onInput=${set('name')} /><//>
        <${Field} label="驱动">
          <select class="input" value=${form.driver} onChange=${set('driver')}>
            ${['bridge', 'macvlan', 'ipvlan', 'overlay'].map(d => html`<option value=${d}>${d}</option>`)}
          </select><//>
      </div>
      <div class="form-grid">
        <${Field} label="子网" hint="CIDR，留空自动分配"><input class="input mono" value=${form.subnet} onInput=${set('subnet')} placeholder="172.30.0.0/16" /><//>
        <${Field} label="网关"><input class="input mono" value=${form.gateway} onInput=${set('gateway')} placeholder="172.30.0.1" /><//>
        <${Field} label="IP 范围" hint="子网内用于分配的 CIDR"><input class="input mono" value=${form.ip_range} onInput=${set('ip_range')} /><//>
      </div>
      <div class="form-actions">
        <label class="check"><input type="checkbox" checked=${form.internal} onChange=${set('internal')} />内部网络（禁止访问外部）</label>
        <label class="check"><input type="checkbox" checked=${form.attachable} onChange=${set('attachable')} />允许手动附加</label>
        <label class="check"><input type="checkbox" checked=${form.ipv6} onChange=${set('ipv6')} />启用 IPv6</label>
      </div>
      <div class="form-actions"><button class="btn btn-primary">创建</button><a class="btn" href=${href('/networks')}>取消</a></div>
    </form>
  <//>`;
}
