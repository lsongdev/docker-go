import { html, useMemo, useState } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, Modal, Field, toast, toastError, confirmDanger } from '../lib/ui.js';
import { ago, bytes, shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents, PruneButton, startJob } from '../docker.js';

function PullDialog({ onClose, onPulled }) {
  const { api } = usePanel();
  const [image, setImage] = useState('');
  const [platform, setPlatform] = useState('');
  const submit = async e => {
    e.preventDefault();
    try {
      await startJob(api, () => api.pullImage(image.trim(), platform), { onDone: onPulled });
      onClose();
    } catch {}
  };
  return html`<${Modal} title="拉取镜像" onClose=${onClose}>
    <form class="form" onSubmit=${submit}>
      <${Field} label="镜像" required hint="不写标签时拉取 latest">
        <input class="input" autofocus required value=${image} onInput=${e => setImage(e.target.value)} placeholder="nginx:alpine" /><//>
      <${Field} label="平台" hint="可选，例如 linux/amd64；留空使用宿主机平台">
        <input class="input" value=${platform} onInput=${e => setPlatform(e.target.value)} /><//>
      <div class="form-actions"><button class="btn btn-primary">拉取</button><button type="button" class="btn" onClick=${onClose}>取消</button></div>
    </form>
  <//>`;
}

export default function Images({ query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const images = useResource(signal => api.images({ signal }));
  useDockerEvents(['image'], images.reload);
  const [pulling, setPulling] = useState(false);
  const [busy, run] = useBusy(toastError);
  const search = query.q || '';
  const filtered = useMemo(() => (images.data || []).filter(i =>
    !search || `${i.tags.join(' ')} ${i.id}`.toLowerCase().includes(search.toLowerCase())), [images.data, search]);
  const remove = img => {
    const label = img.tags[0] || shortId(img.id);
    if (!confirmDanger(`删除镜像 ${label}？${img.containers.length ? `\n它正被 ${img.containers.length} 个容器使用，将强制删除。` : ''}`)) return;
    run(img.id, async () => {
      await api.removeImage(img.id, img.containers.length > 0);
      toast(`已删除 ${label}`, 'success');
      images.reload();
    });
  };
  const total = (images.data || []).reduce((n, i) => n + i.size, 0);
  return html`<${Page} title="镜像" subtitle=${images.data ? `${images.data.length} 个镜像，共 ${bytes(total)}` : null} actions=${html`
    <${PruneButton} title="清理镜像" description="删除悬空镜像（没有标签且未被使用的镜像层）。"
      allLabel="同时删除所有未被容器使用的镜像" prune=${api.pruneImages} onDone=${images.reload} />
    <button class="btn" onClick=${() => navigate('/images/build')}>构建</button>
    <button class="btn btn-primary" onClick=${() => setPulling(true)}>拉取镜像</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="搜索标签或 ID" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
    </div>
    <${Resource} resource=${{ ...images, data: images.data && filtered }} empty="没有镜像">${list => html`
      <div class="table-wrap"><table class="table">
        <thead><tr><th>标签</th><th>ID</th><th>大小</th><th>使用</th><th>创建</th><th class="actions">操作</th></tr></thead>
        <tbody>${list.map(img => html`<tr key=${img.id}>
          <td><a class="cell-main" href=${href(`/images/${img.id}`)}>${img.tags[0] || html`<span class="muted">&lt;none&gt;</span>`}</a>
            ${img.tags.length > 1 ? html`<div class="cell-sub">${img.tags.slice(1).join(', ')}</div>` : null}</td>
          <td><code>${shortId(img.id)}</code></td>
          <td class="nowrap">${bytes(img.size)}</td>
          <td>${img.containers.length ? html`<${Badge} tone="blue" title=${img.containers.join('\n')}>${img.containers.length} 个容器<//>`
            : img.dangling ? html`<${Badge}>悬空<//>` : html`<span class="muted">未使用</span>`}</td>
          <td class="nowrap">${ago(img.created)}</td>
          <td class="actions"><span class="btn-group">
            <a class="btn btn-sm" href=${href('/containers/new', { image: img.tags[0] || img.id })}>运行</a>
            <button class="btn btn-sm btn-danger" disabled=${busy[img.id]} onClick=${() => remove(img)}>删除</button>
          </span></td>
        </tr>`)}</tbody>
      </table></div>`}
    <//>
    ${pulling ? html`<${PullDialog} onClose=${() => setPulling(false)} onPulled=${images.reload} />` : null}
  <//>`;
}
