import { html, useState } from '@lsongdev/panel/html.js';
import { PageHeader, Field, toast } from '@lsongdev/scripts/react/components/index.js';
import { Notice } from '../docker.js';
import { usePanel } from '@lsongdev/panel/panel.js';

export default function NetworkNew() {
  const { api, href, navigate } = usePanel();
  const [form, setForm] = useState({ name: '', driver: 'bridge', subnet: '', gateway: '', ip_range: '', internal: false, attachable: false, ipv6: false });
  const [error, setError] = useState(null);
  const set = key => e => setForm(f => ({ ...f, [key]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const submit = async e => {
    e.preventDefault();
    try {
      const n = await api.createNetwork(form);
      toast(`Created ${n.name}`, 'success');
      navigate(`/networks/${n.id}`);
    } catch (err) { setError(err); }
  };
  return html`<${PageHeader} title="New network" back=${{ href: href('/networks'), label: 'Networks' }} />
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <div class="docker-create-fields">
        <${Field} label="Name" required><input class="input" required value=${form.name} onInput=${set('name')} /><//>
        <${Field} label="Driver">
          <select class="input" value=${form.driver} onChange=${set('driver')}>
            ${['bridge', 'macvlan', 'ipvlan', 'overlay'].map(d => html`<option value=${d}>${d}</option>`)}
          </select><//>
      </div>
      <div class="docker-create-fields">
        <${Field} label="Subnet" hint="A CIDR; empty to assign one"><input class="input mono" value=${form.subnet} onInput=${set('subnet')} placeholder="172.30.0.0/16" /><//>
        <${Field} label="Gateway"><input class="input mono" value=${form.gateway} onInput=${set('gateway')} placeholder="172.30.0.1" /><//>
        <${Field} label="IP range" hint="The CIDR within the subnet to assign from"><input class="input mono" value=${form.ip_range} onInput=${set('ip_range')} /><//>
      </div>
      <div class="button-actions">
        <label class="field-check"><input type="checkbox" checked=${form.internal} onChange=${set('internal')} />Internal (no outside access)</label>
        <label class="field-check"><input type="checkbox" checked=${form.attachable} onChange=${set('attachable')} />Attachable</label>
        <label class="field-check"><input type="checkbox" checked=${form.ipv6} onChange=${set('ipv6')} />Enable IPv6</label>
      </div>
      <div class="button-actions"><button class="button button-primary">Create</button><a class="button" href=${href('/networks')}>Cancel</a></div>
    </form>`;
}
