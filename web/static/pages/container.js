import { html, useEffect, useRef, useState } from '../lib/html.js';
import { api, stream } from '../lib/api.js';
import { href, navigate, setQuery } from '../lib/router.js';
import { useResource, useDockerEvents } from '../lib/hooks.js';
import { Page, Resource, Status, KV, Tabs, LogView, Notice, Badge } from '../lib/ui.js';
import { ago, bytes, datetime, percent, shortId } from '../lib/format.js';
import { ContainerActions } from './containers.js';

export default function Container({ params, query }) {
  const container = useResource(signal => api.container(params.id, { signal }), [params.id]);
  useDockerEvents(['container'], e => { if (params.id.startsWith(e.id) || e.id.startsWith(params.id)) container.reload(); });
  const tab = query.tab || 'overview';
  const c = container.data;
  return html`<${Page} title=${c?.name || shortId(params.id)}
    subtitle=${c ? html`<${Status} state=${c.state} /> ${c.image}` : null}
    actions=${c ? html`<${ContainerActions} c=${c} onChange=${r => r === 'removed' ? navigate('/containers') : container.reload()} />
      <a class="btn" href=${href('/containers')}>返回</a>` : null}>
    <${Resource} resource=${container}>${c => html`
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['overview', '概览'], ['logs', '日志'], ['stats', '监控'], ['terminal', '终端']]} />
      ${tab === 'overview' ? html`<${Overview} c=${c} />` : null}
      ${tab === 'logs' ? html`<${Logs} id=${c.id} />` : null}
      ${tab === 'stats' ? (c.running ? html`<${Stats} id=${c.id} />` : html`<${Notice} tone="info">容器未运行<//>`) : null}
      ${tab === 'terminal' ? (c.running ? html`<${Terminal} id=${c.id} />` : html`<${Notice} tone="info">容器未运行，无法打开终端<//>`) : null}`}
    <//>
  <//>`;
}

function Overview({ c }) {
  return html`<div class="grid-2">
    <div class="card"><h2>基本信息</h2>
      <${KV} items=${[
        ['ID', html`<code>${shortId(c.id)}</code>`],
        ['镜像', html`<a href=${href(`/images/${c.image_id}`)}>${c.image}</a>`],
        ['状态', html`<${Status} state=${c.state} /> ${c.health ? html`<${Status} state=${c.health} />` : null}
          ${c.exit_code && !c.running ? html` <span class="muted">退出码 ${c.exit_code}</span>` : null}
          ${c.oom_killed ? html` <${Badge} tone="red">OOM<//>` : null}`],
        ['错误', c.error],
        ['创建', datetime(c.created)],
        [c.running ? '启动于' : '停止于', c.running ? `${datetime(c.started_at)}（${ago(c.started_at)}）` : datetime(c.finished_at)],
        ['重启策略', `${c.restart_policy}${c.restart_count ? `（已重启 ${c.restart_count} 次）` : ''}`],
        ['Compose', c.project ? html`<a href=${href(`/stacks/${c.project}`)}>${c.project}</a>` : null],
        ['命令', html`<code>${c.command.join(' ')}</code>`],
        ['工作目录', c.working_dir],
        ['用户', c.user],
        ['网络模式', c.network_mode],
        ['特权模式', c.privileged ? '是' : null],
      ]} />
    </div>
    <div>
      <div class="card"><h2>端口</h2>
        ${c.ports.length ? html`<table class="table"><tbody>${c.ports.map(p => html`<tr>
          <td><code>${p.container}</code></td><td>${p.host_port ? html`<code>${p.host_ip || '0.0.0.0'}:${p.host_port}</code>` : html`<span class="muted">未发布</span>`}</td>
        </tr>`)}</tbody></table>` : html`<span class="muted">无</span>`}
      </div>
      <div class="card"><h2>网络</h2>
        ${c.networks.length ? html`<table class="table"><tbody>${c.networks.map(n => html`<tr>
          <td>${n.network}</td><td><code>${n.ip || '—'}</code></td><td class="cell-sub">${n.mac}</td>
        </tr>`)}</tbody></table>` : html`<span class="muted">无</span>`}
      </div>
    </div>
    <div class="card"><h2>挂载</h2>
      ${c.mounts.length ? html`<table class="table"><tbody>${c.mounts.map(m => html`<tr>
        <td><code>${m.destination}</code></td>
        <td>${m.type === 'volume' ? html`卷 <code>${m.name}</code>` : html`<code>${m.source}</code>`}</td>
        <td>${m.rw ? '读写' : '只读'}</td>
      </tr>`)}</tbody></table>` : html`<span class="muted">无</span>`}
    </div>
    <div class="card"><h2>环境变量</h2>
      ${c.env.length ? html`<pre class="log" style="height:auto;max-height:320px">${c.env.join('\n')}</pre>` : html`<span class="muted">无</span>`}
    </div>
  </div>`;
}

function Logs({ id }) {
  const [lines, setLines] = useState([]);
  const [follow, setFollow] = useState(true);
  const [tail, setTail] = useState('500');
  const [error, setError] = useState(null);
  useEffect(() => {
    setLines([]);
    setError(null);
    if (follow) {
      const buffer = [];
      let scheduled = false;
      // Batch bursts of lines into one render.
      const flush = () => { scheduled = false; const batch = buffer.splice(0); setLines(l => [...l, ...batch].slice(-5000)); };
      return stream(`containers/${encodeURIComponent(id)}/logs`, { follow: 1, tail }, {
        line: l => { buffer.push(l); if (!scheduled) { scheduled = true; requestAnimationFrame(flush); } },
        error: e => setError(new Error(e.message)),
        onerror: () => setError(new Error('日志流已断开')),
      });
    }
    const controller = new AbortController();
    api.containerLogs(id, { tail }, { signal: controller.signal }).then(setLines).catch(e => e.name !== 'AbortError' && setError(e));
    return () => controller.abort();
  }, [id, follow, tail]);
  return html`<div>
    <div class="toolbar">
      <label class="check"><input type="checkbox" checked=${follow} onChange=${e => setFollow(e.target.checked)} />实时跟随</label>
      <select class="input" style="max-width:160px" value=${tail} onChange=${e => setTail(e.target.value)}>
        ${['100', '500', '2000', 'all'].map(n => html`<option value=${n}>${n === 'all' ? '全部' : `最近 ${n} 行`}</option>`)}
      </select>
      <span class="spacer"></span>
      <button class="btn btn-sm" onClick=${() => setLines([])}>清屏</button>
    </div>
    <${Notice} error=${error} />
    <${LogView} lines=${lines} follow=${follow} />
  </div>`;
}

function Spark({ values, max }) {
  if (values.length < 2) return html`<svg class="spark"></svg>`;
  const top = max || Math.max(...values, 1);
  const points = values.map((v, i) => `${(i / (values.length - 1)) * 100},${48 - (v / top) * 44}`).join(' ');
  return html`<svg class="spark" viewBox="0 0 100 48" preserveAspectRatio="none" aria-hidden="true">
    <polyline points=${points} fill="none" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke" />
  </svg>`;
}

function Stats({ id }) {
  const [samples, setSamples] = useState([]);
  const [error, setError] = useState(null);
  useEffect(() => stream(`containers/${encodeURIComponent(id)}/stats`, null, {
    stats: s => setSamples(list => [...list.slice(-59), s]),
    error: e => setError(new Error(e.message)),
  }), [id]);
  const last = samples[samples.length - 1];
  const prev = samples[samples.length - 2];
  const rate = (key) => last && prev ? Math.max(0, (last[key] - prev[key]) / ((new Date(last.time) - new Date(prev.time)) / 1000 || 1)) : 0;
  if (error) return html`<${Notice} error=${error} />`;
  if (!last) return html`<div class="loading"><span class="spinner"></span>采集中…</div>`;
  return html`<div class="grid">
    <div class="card stat"><span class="stat-label">CPU</span><span class="stat-value">${percent(last.cpu_percent)}</span>
      <${Spark} values=${samples.map(s => s.cpu_percent)} /></div>
    <div class="card stat"><span class="stat-label">内存</span><span class="stat-value">${bytes(last.memory_usage)}</span>
      <span class="stat-sub">${percent(last.memory_percent)} / ${bytes(last.memory_limit)}</span>
      <div class="meter"><span style=${`width:${Math.min(100, last.memory_percent)}%`}></span></div></div>
    <div class="card stat"><span class="stat-label">网络</span><span class="stat-value">↓ ${bytes(rate('network_rx'))}/s</span>
      <span class="stat-sub">↑ ${bytes(rate('network_tx'))}/s · 累计 ${bytes(last.network_rx)} / ${bytes(last.network_tx)}</span></div>
    <div class="card stat"><span class="stat-label">磁盘 IO</span><span class="stat-value">${bytes(last.block_read)}</span>
      <span class="stat-sub">读取 · 写入 ${bytes(last.block_write)}</span></div>
    <div class="card stat"><span class="stat-label">进程</span><span class="stat-value">${last.pids}</span></div>
  </div>`;
}

function Terminal({ id }) {
  const ref = useRef();
  const [status, setStatus] = useState('connecting');
  const [session, setSession] = useState(0);
  useEffect(() => {
    let disposed = false;
    let cleanup = () => {};
    // xterm is large, so it is loaded only when a terminal is opened.
    if (!document.querySelector('link[data-xterm]')) {
      const link = Object.assign(document.createElement('link'), { rel: 'stylesheet', href: 'vendor/xterm.css' });
      link.dataset.xterm = '';
      document.head.append(link);
    }
    Promise.all([import('../vendor/xterm.mjs'), import('../vendor/addon-fit.mjs')]).then(([{ Terminal }, { FitAddon }]) => {
      if (disposed) return;
      const term = new Terminal({ cursorBlink: true, fontFamily: 'ui-monospace, Menlo, Consolas, monospace', fontSize: 13, theme: { background: '#000000' } });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(ref.current);
      fit.fit();
      setStatus('connecting');
      const ws = new WebSocket(api.terminalURL(id, { cols: term.cols, rows: term.rows }));
      ws.binaryType = 'arraybuffer';
      const encoder = new TextEncoder();
      ws.onopen = () => { setStatus('open'); term.focus(); };
      ws.onmessage = e => term.write(new Uint8Array(e.data));
      ws.onclose = e => { setStatus('closed'); term.write(`\r\n\x1b[90m[${e.reason || '连接已关闭'}]\x1b[0m\r\n`); };
      const input = term.onData(data => ws.readyState === 1 && ws.send(encoder.encode(data)));
      const resize = term.onResize(({ cols, rows }) => ws.readyState === 1 && ws.send(JSON.stringify({ type: 'resize', cols, rows })));
      const observer = new ResizeObserver(() => fit.fit());
      observer.observe(ref.current);
      cleanup = () => { observer.disconnect(); input.dispose(); resize.dispose(); ws.close(); term.dispose(); };
    }).catch(() => setStatus('error'));
    return () => { disposed = true; cleanup(); };
  }, [id, session]);
  return html`<div>
    <div class="toolbar">
      <${Status} state=${status === 'open' ? 'running' : status === 'connecting' ? 'starting' : 'stopped'} />
      <span class="muted">${{ open: '已连接', connecting: '连接中…', closed: '会话已结束', error: '终端加载失败' }[status]}</span>
      <span class="spacer"></span>
      ${status === 'closed' ? html`<button class="btn btn-sm" onClick=${() => setSession(s => s + 1)}>重新连接</button>` : null}
    </div>
    <div class="terminal" ref=${ref}></div>
  </div>`;
}
