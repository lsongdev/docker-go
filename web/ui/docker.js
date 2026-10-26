// Docker-specific UI support: the typed API, live daemon events, background
// job dialogs and shared resource actions. Generic building blocks are in
// lib/.
import { html, useEffect, useRef, useState } from './lib/html.js';
import { usePanel } from './lib/panel.js';
import { ConfirmButton, LogView, Modal, Notice, Status, openDialog, toast, toastError } from './lib/ui.js';
import { bytes } from './lib/format.js';

const enc = encodeURIComponent;

// dockerAPI wraps the module's API client (created by the shell for wherever
// the module is mounted).
export function dockerAPI(client) {
  const { get, post, put, patch, del, request } = client;
  return {
    client,
    stream: client.stream,

    system: opts => get('system', opts),

    containers: opts => get('containers', opts),
    container: (id, opts) => get(`containers/${enc(id)}`, opts),
    createContainer: spec => post('containers', spec),
    containerAction: (id, action, body) => post(`containers/${enc(id)}/${action}`, body),
    removeContainer: (id, { force = false, volumes = false } = {}) => del(`containers/${enc(id)}`, { query: { force, volumes } }),
    containerLogs: (id, query, opts) => get(`containers/${enc(id)}/logs`, { ...opts, query }),
    pruneContainers: () => post('containers/prune'),
    terminalURL: (id, query) => client.socketURL(`containers/${enc(id)}/terminal`, query),

    images: opts => get('images', opts),
    image: (id, opts) => get(`images/${enc(id)}`, opts),
    pullImage: (image, platform) => post('images/pull', { image, platform }),
    buildImage: spec => post('images/build', spec),
    removeImage: (id, force = false) => del(`images/${enc(id)}`, { query: { force } }),
    pruneImages: all => request('POST', 'images/prune', { query: { all } }),

    volumes: opts => get('volumes', opts),
    volume: (name, opts) => get(`volumes/${enc(name)}`, opts),
    createVolume: spec => post('volumes', spec),
    removeVolume: (name, force = false) => del(`volumes/${enc(name)}`, { query: { force } }),
    pruneVolumes: all => request('POST', 'volumes/prune', { query: { all } }),

    networks: opts => get('networks', opts),
    network: (id, opts) => get(`networks/${enc(id)}`, opts),
    createNetwork: spec => post('networks', spec),
    removeNetwork: id => del(`networks/${enc(id)}`),
    pruneNetworks: () => post('networks/prune'),

    stacks: opts => get('stacks', opts),
    stack: (name, opts) => get(`stacks/${enc(name)}`, opts),
    createStack: spec => post('stacks', spec),
    updateStack: (name, spec) => put(`stacks/${enc(name)}`, spec),
    deleteStack: (name, volumes = false) => del(`stacks/${enc(name)}`, { query: { volumes } }),
    stackAction: (name, action) => post(`stacks/${enc(name)}/${action}`),

    catalog: (query, opts) => get('catalog', { ...opts, query }),
    categories: opts => get('catalog/categories', opts),
    catalogApp: (id, opts) => get(`catalog/${enc(id)}`, opts),
    iconURL: id => client.url(`catalog/${enc(id)}/icon`).href,
    syncCatalog: () => post('catalog/sync'),

    apps: opts => get('apps', opts),
    app: (id, opts) => get(`apps/${enc(id)}`, opts),
    installApp: (id, req) => post('apps', { id, ...req }),
    updateApp: (id, req = {}) => patch(`apps/${enc(id)}`, req),
    uninstallApp: (id, keepData = false) => del(`apps/${enc(id)}`, { query: { keep_data: keepData } }),
    appAction: (id, action) => post(`apps/${enc(id)}/${action}`),

    jobs: opts => get('jobs', opts),
    job: (id, opts) => get(`jobs/${enc(id)}`, opts),
    cancelJob: id => post(`jobs/${enc(id)}/cancel`),
  };
}

// ---- Daemon events --------------------------------------------------------

// Every subscriber shares one event stream per API.
const subscriptions = new Map(); // api → { listeners, close }

function subscribe(api, listener) {
  let sub = subscriptions.get(api);
  if (!sub) {
    sub = { listeners: new Set(), close: null };
    subscriptions.set(api, sub);
  }
  sub.listeners.add(listener);
  const connect = () => {
    if (sub.close || !sub.listeners.size) return;
    const retry = () => { sub.close = null; setTimeout(connect, 3000); };
    sub.close = api.stream('events', null, { event: e => sub.listeners.forEach(fn => fn(e)), onerror: retry, error: retry });
  };
  connect();
  return () => {
    sub.listeners.delete(listener);
    if (!sub.listeners.size && sub.close) { sub.close(); sub.close = null; }
  };
}

// useDockerEvents calls onChange (debounced) whenever the daemon reports an
// event of one of the given types, keeping lists live without polling.
export function useDockerEvents(types, onChange, delay = 400) {
  const { api } = usePanel();
  const callback = useRef(onChange);
  callback.current = onChange;
  useEffect(() => {
    let timer;
    const off = subscribe(api, e => {
      if (!types.includes(e.type)) return;
      clearTimeout(timer);
      timer = setTimeout(() => callback.current(e), delay);
    });
    return () => { clearTimeout(timer); off(); };
  }, [api, types.join(',')]);
}

// ---- Jobs -----------------------------------------------------------------

const jobWatchers = new Set();

// onJobsChanged lets views refresh when jobs start or finish.
export function onJobsChanged(fn) {
  jobWatchers.add(fn);
  return () => jobWatchers.delete(fn);
}

// startJob calls fn (which starts a job and resolves to its snapshot) and
// shows the job's live log. onDone(job) runs when the job finishes.
export async function startJob(api, fn, { onDone } = {}) {
  try {
    const job = await fn();
    openJob(api, job.id, onDone);
    jobWatchers.forEach(w => w());
    return job;
  } catch (err) {
    toastError(err);
    throw err;
  }
}

export function openJob(api, id, onDone) {
  openDialog(close => html`<${JobDialog} api=${api} id=${id} onDone=${onDone} onClose=${close} />`);
}

function JobDialog({ api, id, onDone, onClose }) {
  const [job, setJob] = useState(null);
  const [lines, setLines] = useState([]);
  const [error, setError] = useState(null);
  useEffect(() => api.stream(`jobs/${enc(id)}/events`, null, {
    snapshot: s => { setJob(s.job); setLines(s.lines); },
    line: line => setLines(l => [...l, line]),
    done: final => {
      setJob(final);
      jobWatchers.forEach(w => w());
      if (final.status === 'succeeded') toast(`${final.title}: done`, 'success');
      onDone?.(final);
    },
    onerror: () => setError(new Error('Lost the connection to the server')),
  }), [id]);
  const running = job?.status === 'running';
  const cancel = () => api.cancelJob(id).catch(toastError);
  return html`<${Modal} title=${job?.title || 'Job'} onClose=${onClose} wide
    footer=${html`
      ${job ? html`<${Status} state=${job.status} />` : null}
      ${job?.error ? html`<span class="job-error">${job.error}</span>` : null}
      <span class="spacer"></span>
      ${running ? html`<button class="button" onClick=${cancel}>Cancel job</button>` : null}
      <button class="button button-primary" onClick=${onClose}>${running ? 'Run in background' : 'Close'}</button>`}>
    <${Notice} error=${error} />
    <${LogView} lines=${lines} empty=${running ? 'Waiting for output…' : 'No output'} />
  <//>`;
}

// PruneButton confirms and runs a prune; prune(all) returns a report
// {deleted, space_reclaimed}. allLabel offers the broader variant.
export function PruneButton({ title, description, allLabel, prune, onDone }) {
  const run = async all => {
    const r = await prune(all);
    const space = r?.space_reclaimed ? `, ${bytes(r.space_reclaimed)} reclaimed` : '';
    toast(`Removed ${r?.deleted?.length || 0}${space}`, 'success');
    onDone?.();
  };
  return html`<${ConfirmButton} label="Prune" title=${title} description=${description} checkbox=${allLabel}
    confirmLabel="Prune" danger=${false} onConfirm=${run} />`;
}
