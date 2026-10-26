import { html, useMemo, useState } from '../lib/html.js';
import { useResource, useBusy } from '../lib/hooks.js';
import { Page, Resource, Badge, Modal, Field, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
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
  return html`<${Modal} title="Pull image" onClose=${onClose}>
    <form class="form" onSubmit=${submit}>
      <${Field} label="Image" required hint="Without a tag, latest is pulled">
        <input class="input" autofocus required value=${image} onInput=${e => setImage(e.target.value)} placeholder="nginx:alpine" /><//>
      <${Field} label="Platform" hint="Optional, such as linux/amd64; empty for the host's">
        <input class="input" value=${platform} onInput=${e => setPlatform(e.target.value)} /><//>
      <div class="form-actions"><button class="button button-primary">Pull</button><button type="button" class="button" onClick=${onClose}>Cancel</button></div>
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
    if (!confirmDanger(`Delete the image ${label}?${img.containers.length ? `\n${img.containers.length} containers use it; it is deleted forcibly.` : ''}`)) return;
    run(img.id, async () => {
      await api.removeImage(img.id, img.containers.length > 0);
      toast(`Deleted ${label}`, 'success');
      images.reload();
    });
  };
  const total = (images.data || []).reduce((n, i) => n + i.size, 0);
  return html`<${Page} title="Images" subtitle=${images.data ? `${images.data.length} images, ${bytes(total)} in all` : null} actions=${html`
    <${PruneButton} title="Prune images" description="Delete dangling images (untagged layers no image uses)."
      allLabel="Also delete every image no container uses" prune=${api.pruneImages} onDone=${images.reload} />
    <button class="button" onClick=${() => navigate('/images/build')}>Build</button>
    <button class="button button-primary" onClick=${() => setPulling(true)}>Pull image</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="Search tags or IDs" value=${search} onInput=${e => setQuery({ q: e.target.value })} />
    </div>
    <${Resource} resource=${{ ...images, data: images.data && filtered }} empty="No images">${list => html`
      <${Table}>
        <thead><tr><th>Tags</th><th>ID</th><th>Size</th><th>Used by</th><th>Created</th><th class="actions">Actions</th></tr></thead>
        <tbody>${list.map(img => html`<tr key=${img.id}>
          <td><a class="cell-main" href=${href(`/images/${img.id}`)}>${img.tags[0] || html`<span class="muted">&lt;none&gt;</span>`}</a>
            ${img.tags.length > 1 ? html`<div class="cell-sub">${img.tags.slice(1).join(', ')}</div>` : null}</td>
          <td><code>${shortId(img.id)}</code></td>
          <td class="nowrap">${bytes(img.size)}</td>
          <td>${img.containers.length ? html`<${Badge} tone="blue" title=${img.containers.join('\n')}>${img.containers.length} containers<//>`
            : img.dangling ? html`<${Badge}>Dangling<//>` : html`<span class="muted">Unused</span>`}</td>
          <td class="nowrap">${ago(img.created)}</td>
          <td class="actions"><span class="button-row">
            <a class="button button-small" href=${href('/containers/new', { image: img.tags[0] || img.id })}>Run</a>
            <button class="button button-small button-danger" disabled=${busy[img.id]} onClick=${() => remove(img)}>Delete</button>
          </span></td>
        </tr>`)}</tbody>
      <//>`}
    <//>
    ${pulling ? html`<${PullDialog} onClose=${() => setPulling(false)} onPulled=${images.reload} />` : null}
  <//>`;
}
