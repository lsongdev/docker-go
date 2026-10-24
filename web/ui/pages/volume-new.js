import { html, useState } from '../lib/html.js';
import { Page, Field, Notice, ListInput, toast } from '../lib/ui.js';
import { usePanel } from '../lib/panel.js';

const toMap = list => Object.fromEntries(list.filter(s => s.includes('=')).map(s => [s.slice(0, s.indexOf('=')), s.slice(s.indexOf('=') + 1)]));

export default function VolumeNew() {
  const { api, href, navigate } = usePanel();
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('local');
  const [options, setOptions] = useState([]);
  const [labels, setLabels] = useState([]);
  const [error, setError] = useState(null);
  const submit = async e => {
    e.preventDefault();
    try {
      const v = await api.createVolume({ name, driver, options: toMap(options), labels: toMap(labels) });
      toast(`已创建 ${v.name}`, 'success');
      navigate('/volumes');
    } catch (err) { setError(err); }
  };
  return html`<${Page} title="创建存储卷" actions=${html`<a class="btn" href=${href('/volumes')}>返回</a>`}>
    <form class="form card" onSubmit=${submit}>
      <${Notice} error=${error} />
      <div class="form-grid">
        <${Field} label="名称" hint="留空则生成随机名称"><input class="input" value=${name} onInput=${e => setName(e.target.value)} /><//>
        <${Field} label="驱动"><input class="input" value=${driver} onInput=${e => setDriver(e.target.value)} /><//>
      </div>
      <div class="form-grid">
        <${Field} label="驱动选项" hint="KEY=VALUE，例如挂载 NFS：type=nfs、o=addr=10.0.0.2,rw、device=:/export">
          <${ListInput} values=${options} onChange=${setOptions} placeholder="type=nfs" /><//>
        <${Field} label="标签" hint="KEY=VALUE"><${ListInput} values=${labels} onChange=${setLabels} /><//>
      </div>
      <div class="form-actions"><button class="btn btn-primary">创建</button><a class="btn" href=${href('/volumes')}>取消</a></div>
    </form>
  <//>`;
}
