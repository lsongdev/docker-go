import { html } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, KV, toast, toastError, confirmDanger } from '../lib/ui.js';
import { bytes, datetime, shortId, ago } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';

export default function Image({ params }) {
  const { api, href, navigate } = usePanel();
  const image = useResource(signal => api.image(params.id, { signal }), [params.id]);
  const remove = async () => {
    const img = image.data;
    if (!confirmDanger(`删除镜像 ${img.tags[0] || shortId(img.id)}？`)) return;
    try {
      await api.removeImage(img.id, img.containers.length > 0);
      toast('镜像已删除', 'success');
      navigate('/images');
    } catch (err) { toastError(err); }
  };
  const img = image.data;
  return html`<${Page} title=${img?.tags[0] || shortId(params.id)} subtitle=${img ? html`<code>${img.id}</code>` : null}
    actions=${img ? html`
      <a class="btn btn-primary" href=${href('/containers/new', { image: img.tags[0] || img.id })}>运行容器</a>
      <button class="btn btn-danger" onClick=${remove}>删除</button>
      <a class="btn" href=${href('/images')}>返回</a>` : null}>
    <${Resource} resource=${image}>${img => html`
      <div class="grid-2">
        <div class="card"><h2>基本信息</h2>
          <${KV} items=${[
            ['标签', img.tags.join(', ')],
            ['摘要', img.digests.length ? html`<code>${img.digests[0]}</code>` : null],
            ['大小', bytes(img.size)],
            ['层数', img.layers],
            ['平台', `${img.os}/${img.architecture}`],
            ['创建', datetime(img.created)],
            ['作者', img.author],
            ['使用中', img.containers.join(', ')],
          ]} />
        </div>
        <div class="card"><h2>默认配置</h2>
          <${KV} items=${[
            ['入口点', img.entrypoint?.length ? html`<code>${img.entrypoint.join(' ')}</code>` : null],
            ['命令', img.cmd?.length ? html`<code>${img.cmd.join(' ')}</code>` : null],
            ['工作目录', img.working_dir],
            ['用户', img.user],
            ['暴露端口', img.exposed_ports.join(', ')],
            ['卷', img.volumes.join(', ')],
          ]} />
          ${img.env.length ? html`<pre class="log" style="height:auto;max-height:220px;margin-top:12px">${img.env.join('\n')}</pre>` : null}
        </div>
      </div>
      <div class="section"><h2>构建历史</h2>
        <div class="table-wrap"><table class="table">
          <thead><tr><th>指令</th><th>大小</th><th>时间</th></tr></thead>
          <tbody>${img.history.map(h => html`<tr>
            <td><code style="overflow-wrap:anywhere">${h.created_by.replace(/^\/bin\/sh -c (#\(nop\) )?/, '')}</code></td>
            <td class="nowrap">${h.size ? bytes(h.size) : '—'}</td>
            <td class="nowrap">${ago(h.created)}</td>
          </tr>`)}</tbody>
        </table></div>
      </div>`}
    <//>
  <//>`;
}
