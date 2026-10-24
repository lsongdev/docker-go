// Minimal hash router: "#/containers/abc?tab=logs". Hash routing keeps the
// UI mountable under any path without server-side rewrites.
import { useEffect, useState } from './html.js';

function current() {
  const raw = location.hash.replace(/^#/, '') || '/';
  const [path, search = ''] = raw.split('?');
  return { path: path || '/', query: Object.fromEntries(new URLSearchParams(search)) };
}

export function navigate(to, { replace = false } = {}) {
  const hash = '#' + to;
  if (replace) history.replaceState(null, '', hash);
  else history.pushState(null, '', hash);
  dispatchEvent(new HashChangeEvent('hashchange'));
}

export function href(path, query) {
  const search = new URLSearchParams(Object.entries(query || {}).filter(([, v]) => v !== '' && v != null)).toString();
  return '#' + path + (search ? '?' + search : '');
}

// setQuery updates the current route's query string in place, so list
// filters are shareable and survive reloads.
export function setQuery(patch) {
  const { path, query } = current();
  const search = new URLSearchParams(Object.entries({ ...query, ...patch }).filter(([, v]) => v !== '' && v != null)).toString();
  navigate(path + (search ? '?' + search : ''), { replace: true });
}

// compile turns "/containers/:id" into a matcher returning params.
function compile(pattern) {
  const names = [];
  const re = new RegExp('^' + pattern.replace(/:(\w+)/g, (_, name) => { names.push(name); return '([^/]+)'; }) + '/?$');
  return path => {
    const m = path.match(re);
    return m && Object.fromEntries(names.map((n, i) => [n, decodeURIComponent(m[i + 1])]));
  };
}

export function useRoute(routes) {
  const [location, setLocation] = useState(current);
  useEffect(() => {
    const onChange = () => setLocation(current());
    addEventListener('hashchange', onChange);
    addEventListener('popstate', onChange);
    return () => { removeEventListener('hashchange', onChange); removeEventListener('popstate', onChange); };
  }, []);
  for (const route of routes) {
    route.match ||= compile(route.path);
    const params = route.match(location.path);
    if (params) return { ...location, route, params };
  }
  return { ...location, route: null, params: {} };
}
