// HTTP client for the panel API. Paths are resolved against the page URL so
// the UI works wherever it is mounted; the API lives at "api/" beside it.

const base = new URL('api/', document.baseURI);

export function apiURL(path, query) {
  const url = new URL(path.replace(/^\//, ''), base);
  for (const [key, value] of Object.entries(query || {})) {
    if (value !== undefined && value !== null && value !== '') url.searchParams.set(key, value);
  }
  return url;
}

export class ApiError extends Error {
  constructor(status, { code = 'error', message = 'Request failed', fields } = {}) {
    super(message);
    this.status = status;
    this.code = code;
    this.fields = fields || {};
  }
}

async function request(method, path, { body, query, signal } = {}) {
  const init = { method, signal, headers: { Accept: 'application/json' } };
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let response;
  try {
    response = await fetch(apiURL(path, query), init);
  } catch (err) {
    if (err.name === 'AbortError') throw err;
    throw new ApiError(0, { code: 'network', message: '无法连接到面板服务' });
  }
  if (response.status === 204) return null;
  const payload = await response.json().catch(() => null);
  if (!response.ok) throw new ApiError(response.status, payload?.error || { message: response.statusText });
  return payload?.data ?? null;
}

const get = (path, opts) => request('GET', path, opts);
const post = (path, body, opts) => request('POST', path, { ...opts, body });
const put = (path, body) => request('PUT', path, { body });
const patch = (path, body) => request('PATCH', path, { body });
const del = (path, query) => request('DELETE', path, { query });
const enc = encodeURIComponent;

// stream subscribes to a server-sent event endpoint. handlers maps event
// names to callbacks. It returns a function that closes the stream. Streams
// that end on their own ("end", "done") are closed rather than reconnected.
export function stream(path, query, handlers) {
  const source = new EventSource(apiURL(path, query));
  for (const [event, fn] of Object.entries(handlers)) {
    if (event === 'onerror') continue;
    source.addEventListener(event, e => {
      const data = e.data ? JSON.parse(e.data) : null;
      if (event === 'end' || event === 'done') source.close();
      fn(data);
    });
  }
  source.addEventListener('error', e => {
    // A server "error" event carries data; a transport error does not.
    if (e.data) {
      source.close();
      handlers.error?.(JSON.parse(e.data));
    } else if (source.readyState === EventSource.CLOSED) {
      handlers.onerror?.();
    }
  });
  return () => source.close();
}

export const api = {
  system: opts => get('system', opts),

  containers: opts => get('containers', opts),
  container: (id, opts) => get(`containers/${enc(id)}`, opts),
  createContainer: spec => post('containers', spec),
  containerAction: (id, action, body) => post(`containers/${enc(id)}/${action}`, body),
  removeContainer: (id, { force = false, volumes = false } = {}) => del(`containers/${enc(id)}`, { force, volumes }),
  containerLogs: (id, query, opts) => get(`containers/${enc(id)}/logs`, { ...opts, query }),
  pruneContainers: () => post('containers/prune'),
  terminalURL: (id, query) => {
    const url = apiURL(`containers/${enc(id)}/terminal`, query);
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    return url;
  },

  images: opts => get('images', opts),
  image: (id, opts) => get(`images/${enc(id)}`, opts),
  pullImage: (image, platform) => post('images/pull', { image, platform }),
  buildImage: spec => post('images/build', spec),
  removeImage: (id, force = false) => del(`images/${enc(id)}`, { force }),
  pruneImages: all => request('POST', 'images/prune', { query: { all } }),

  volumes: opts => get('volumes', opts),
  volume: (name, opts) => get(`volumes/${enc(name)}`, opts),
  createVolume: spec => post('volumes', spec),
  removeVolume: (name, force = false) => del(`volumes/${enc(name)}`, { force }),
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
  deleteStack: (name, volumes = false) => del(`stacks/${enc(name)}`, { volumes }),
  stackAction: (name, action) => post(`stacks/${enc(name)}/${action}`),

  catalog: (query, opts) => get('catalog', { ...opts, query }),
  categories: opts => get('catalog/categories', opts),
  catalogApp: (id, opts) => get(`catalog/${enc(id)}`, opts),
  iconURL: id => apiURL(`catalog/${enc(id)}/icon`).href,
  syncCatalog: () => post('catalog/sync'),

  apps: opts => get('apps', opts),
  app: (id, opts) => get(`apps/${enc(id)}`, opts),
  installApp: (id, request) => post('apps', { id, ...request }),
  updateApp: (id, request = {}) => patch(`apps/${enc(id)}`, request),
  uninstallApp: (id, keepData = false) => del(`apps/${enc(id)}`, { keep_data: keepData }),
  appAction: (id, action) => post(`apps/${enc(id)}/${action}`),

  jobs: opts => get('jobs', opts),
  job: (id, opts) => get(`jobs/${enc(id)}`, opts),
  cancelJob: id => post(`jobs/${enc(id)}/cancel`),
};
