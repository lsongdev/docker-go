import { html, useEffect, useRef, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Status, KV, Tabs, LogView, Notice, Badge, Table } from '../lib/ui.js';
import { ago, bytes, datetime, percent, shortId } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { useDockerEvents } from '../docker.js';
import { ContainerActions } from './containers.js';

export default function Container({ params, query }) {
  const { api, href, navigate, setQuery } = usePanel();
  const container = useResource(signal => api.container(params.id, { signal }), [params.id]);
  useDockerEvents(['container'], e => { if (params.id.startsWith(e.id) || e.id.startsWith(params.id)) container.reload(); });
  const tab = query.tab || 'overview';
  const c = container.data;
  return html`<${Page} title=${c?.name || shortId(params.id)}
    subtitle=${c ? html`<${Status} state=${c.state} /> ${c.image}` : null}
    back=${{ href: href('/containers'), label: 'Containers' }}
    actions=${c ? html`<${ContainerActions} c=${c} onChange=${r => r === 'removed' ? navigate('/containers') : container.reload()} />` : null}>
    <${Resource} resource=${container}>${c => html`
      <${Tabs} active=${tab} onChange=${t => setQuery({ tab: t })} tabs=${[['overview', 'Overview'], ['logs', 'Logs'], ['stats', 'Stats'], ['terminal', 'Terminal']]} />
      ${tab === 'overview' ? html`<${Overview} c=${c} />` : null}
      ${tab === 'logs' ? html`<${Logs} id=${c.id} />` : null}
      ${tab === 'stats' ? (c.running ? html`<${Stats} id=${c.id} />` : html`<${Notice} tone="info">The container is not running<//>`) : null}
      ${tab === 'terminal' ? (c.running ? html`<${Terminal} id=${c.id} />` : html`<${Notice} tone="info">The container is not running, so no terminal can open<//>`) : null}`}
    <//>
  <//>`;
}

function Overview({ c }) {
  const { href } = usePanel();
  return html`<div class="grid-2">
    <div class="card"><h2>Details</h2>
      <${KV} items=${[
        ['ID', html`<code>${shortId(c.id)}</code>`],
        ['Image', html`<a href=${href(`/images/${c.image_id}`)}>${c.image}</a>`],
        ['Status', html`<${Status} state=${c.state} /> ${c.health ? html`<${Status} state=${c.health} />` : null}
          ${c.exit_code && !c.running ? html` <span class="muted">exit code ${c.exit_code}</span>` : null}
          ${c.oom_killed ? html` <${Badge} tone="red">OOM<//>` : null}`],
        ['Error', c.error],
        ['Created', datetime(c.created)],
        [c.running ? 'Started' : 'Stopped', c.running ? `${datetime(c.started_at)} (${ago(c.started_at)})` : datetime(c.finished_at)],
        ['Restart policy', `${c.restart_policy}${c.restart_count ? ` (restarted ${c.restart_count} times)` : ''}`],
        ['Compose', c.project ? html`<a href=${href(`/stacks/${c.project}`)}>${c.project}</a>` : null],
        ['Command', html`<code>${c.command.join(' ')}</code>`],
        ['Working directory', c.working_dir],
        ['User', c.user],
        ['Network mode', c.network_mode],
        ['Privileged', c.privileged ? 'Yes' : null],
      ]} />
    </div>
    <div>
      <div class="card"><h2>Ports</h2>
        ${c.ports.length ? html`<${Table} framed=${false}><tbody>${c.ports.map(p => html`<tr>
          <td><code>${p.container}</code></td><td>${p.host_port ? html`<code>${p.host_ip || '0.0.0.0'}:${p.host_port}</code>` : html`<span class="muted">Not published</span>`}</td>
        </tr>`)}</tbody><//>` : html`<span class="muted">None</span>`}
      </div>
      <div class="card"><h2>Networks</h2>
        ${c.networks.length ? html`<${Table} framed=${false}><tbody>${c.networks.map(n => html`<tr>
          <td>${n.network}</td><td><code>${n.ip || '—'}</code></td><td class="cell-sub">${n.mac}</td>
        </tr>`)}</tbody><//>` : html`<span class="muted">None</span>`}
      </div>
    </div>
    <div class="card"><h2>Mounts</h2>
      ${c.mounts.length ? html`<${Table} framed=${false}><tbody>${c.mounts.map(m => html`<tr>
        <td><code>${m.destination}</code></td>
        <td>${m.type === 'volume' ? html`volume <code>${m.name}</code>` : html`<code>${m.source}</code>`}</td>
        <td>${m.rw ? 'read-write' : 'read-only'}</td>
      </tr>`)}</tbody><//>` : html`<span class="muted">None</span>`}
    </div>
    <div class="card"><h2>Environment</h2>
      ${c.env.length ? html`<pre class="log" style="height:auto;max-height:320px">${c.env.join('\n')}</pre>` : html`<span class="muted">None</span>`}
    </div>
  </div>`;
}

function Logs({ id }) {
  const { api } = usePanel();
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
      return api.stream(`containers/${encodeURIComponent(id)}/logs`, { follow: 1, tail }, {
        line: l => { buffer.push(l); if (!scheduled) { scheduled = true; requestAnimationFrame(flush); } },
        error: e => setError(new Error(e.message)),
        onerror: () => setError(new Error('The log stream was cut off')),
      });
    }
    const controller = new AbortController();
    api.containerLogs(id, { tail }, { signal: controller.signal }).then(setLines).catch(e => e.name !== 'AbortError' && setError(e));
    return () => controller.abort();
  }, [id, follow, tail]);
  return html`<div>
    <div class="toolbar">
      <label class="field-check"><input type="checkbox" checked=${follow} onChange=${e => setFollow(e.target.checked)} />Follow</label>
      <select class="input" style="max-width:160px" value=${tail} onChange=${e => setTail(e.target.value)}>
        ${['100', '500', '2000', 'all'].map(n => html`<option value=${n}>${n === 'all' ? 'All' : `Last ${n} lines`}</option>`)}
      </select>
      <span class="spacer"></span>
      <button class="button button-small" onClick=${() => setLines([])}>Clear</button>
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
  const { api } = usePanel();
  const [samples, setSamples] = useState([]);
  const [error, setError] = useState(null);
  useEffect(() => api.stream(`containers/${encodeURIComponent(id)}/stats`, null, {
    stats: s => setSamples(list => [...list.slice(-59), s]),
    error: e => setError(new Error(e.message)),
  }), [id]);
  const last = samples[samples.length - 1];
  const prev = samples[samples.length - 2];
  const rate = (key) => last && prev ? Math.max(0, (last[key] - prev[key]) / ((new Date(last.time) - new Date(prev.time)) / 1000 || 1)) : 0;
  if (error) return html`<${Notice} error=${error} />`;
  if (!last) return html`<div class="loading"><span class="spinner"></span>Collecting…</div>`;
  return html`<div class="grid">
    <div class="card stat"><span class="stat-label">CPU</span><span class="stat-value">${percent(last.cpu_percent)}</span>
      <${Spark} values=${samples.map(s => s.cpu_percent)} /></div>
    <div class="card stat"><span class="stat-label">Memory</span><span class="stat-value">${bytes(last.memory_usage)}</span>
      <span class="stat-sub">${percent(last.memory_percent)} / ${bytes(last.memory_limit)}</span>
      <div class="meter"><span style=${`width:${Math.min(100, last.memory_percent)}%`}></span></div></div>
    <div class="card stat"><span class="stat-label">Network</span><span class="stat-value">↓ ${bytes(rate('network_rx'))}/s</span>
      <span class="stat-sub">↑ ${bytes(rate('network_tx'))}/s · total ${bytes(last.network_rx)} / ${bytes(last.network_tx)}</span></div>
    <div class="card stat"><span class="stat-label">Disk I/O</span><span class="stat-value">${bytes(last.block_read)}</span>
      <span class="stat-sub">read · ${bytes(last.block_write)} written</span></div>
    <div class="card stat"><span class="stat-label">Processes</span><span class="stat-value">${last.pids}</span></div>
  </div>`;
}

function Terminal({ id }) {
  const { api } = usePanel();
  const ref = useRef();
  const [status, setStatus] = useState('connecting');
  const [session, setSession] = useState(0);
  useEffect(() => {
    let disposed = false;
    let cleanup = () => {};
    // xterm is large, so it is loaded only when a terminal is opened.
    // Inside a panel the module lives in a shadow root, which document
    // styles do not reach: add the stylesheet where the terminal renders.
    const root = ref.current.getRootNode();
    const target = root instanceof ShadowRoot ? root : document.head;
    if (!root.querySelector('link[data-xterm]')) {
      const link = Object.assign(document.createElement('link'), { rel: 'stylesheet', href: new URL('../node_modules/@xterm/xterm/css/xterm.css', import.meta.url).href });
      link.dataset.xterm = '';
      target.append(link);
    }
    Promise.all([import('../node_modules/@xterm/xterm/lib/xterm.mjs'), import('../node_modules/@xterm/addon-fit/lib/addon-fit.mjs')]).then(([{ Terminal }, { FitAddon }]) => {
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
      ws.onclose = e => { setStatus('closed'); term.write(`\r\n\x1b[90m[${e.reason || 'Connection closed'}]\x1b[0m\r\n`); };
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
      <span class="muted">${{ open: 'Connected', connecting: 'Connecting…', closed: 'Session ended', error: 'The terminal failed to load' }[status]}</span>
      <span class="spacer"></span>
      ${status === 'closed' ? html`<button class="button button-small" onClick=${() => setSession(s => s + 1)}>Reconnect</button>` : null}
    </div>
    <div class="terminal" ref=${ref}></div>
  </div>`;
}
