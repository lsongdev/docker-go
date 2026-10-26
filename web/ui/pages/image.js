import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, KV, toast, toastError, confirmDanger, Table } from '../lib/ui.js';
import { bytes, datetime, shortId, ago } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';

export default function Image({ params }) {
  const { api, href, navigate } = usePanel();
  const image = useResource(signal => api.image(params.id, { signal }), [params.id]);
  const remove = async () => {
    const img = image.data;
    if (!confirmDanger(`Delete the image ${img.tags[0] || shortId(img.id)}?`)) return;
    try {
      await api.removeImage(img.id, img.containers.length > 0);
      toast('Image deleted', 'success');
      navigate('/images');
    } catch (err) { toastError(err); }
  };
  const img = image.data;
  return html`<${Page} title=${img?.tags[0] || shortId(params.id)} subtitle=${img ? html`<code>${img.id}</code>` : null}
    actions=${img ? html`
      <a class="button button-primary" href=${href('/containers/new', { image: img.tags[0] || img.id })}>Run container</a>
      <button class="button button-danger" onClick=${remove}>Delete</button>
      <a class="button" href=${href('/images')}>Back</a>` : null}>
    <${Resource} resource=${image}>${img => html`
      <div class="grid-2">
        <div class="card"><h2>Details</h2>
          <${KV} items=${[
            ['Tags', img.tags.join(', ')],
            ['Digest', img.digests.length ? html`<code>${img.digests[0]}</code>` : null],
            ['Size', bytes(img.size)],
            ['Layers', img.layers],
            ['Platform', `${img.os}/${img.architecture}`],
            ['Created', datetime(img.created)],
            ['Author', img.author],
            ['Used by', img.containers.join(', ')],
          ]} />
        </div>
        <div class="card"><h2>Defaults</h2>
          <${KV} items=${[
            ['Entrypoint', img.entrypoint?.length ? html`<code>${img.entrypoint.join(' ')}</code>` : null],
            ['Command', img.cmd?.length ? html`<code>${img.cmd.join(' ')}</code>` : null],
            ['Working directory', img.working_dir],
            ['User', img.user],
            ['Exposed ports', img.exposed_ports.join(', ')],
            ['Volumes', img.volumes.join(', ')],
          ]} />
          ${img.env.length ? html`<pre class="log" style="height:auto;max-height:220px;margin-top:12px">${img.env.join('\n')}</pre>` : null}
        </div>
      </div>
      <div class="section"><h2>History</h2>
        <${Table}>
          <thead><tr><th>Instruction</th><th>Size</th><th>Created</th></tr></thead>
          <tbody>${img.history.map(h => html`<tr>
            <td><code style="overflow-wrap:anywhere">${h.created_by.replace(/^\/bin\/sh -c (#\(nop\) )?/, '')}</code></td>
            <td class="nowrap">${h.size ? bytes(h.size) : '—'}</td>
            <td class="nowrap">${ago(h.created)}</td>
          </tr>`)}</tbody>
        <//>
      </div>`}
    <//>
  <//>`;
}
