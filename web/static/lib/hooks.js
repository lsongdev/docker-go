import { useCallback, useEffect, useRef, useState } from './html.js';
import { stream } from './api.js';

// useResource loads data with fn(signal) and reloads when deps change.
// Reloads keep showing the previous data instead of flashing a spinner.
export function useResource(fn, deps = []) {
  const [state, setState] = useState({ data: undefined, error: null, loading: true });
  const [version, setVersion] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setState(s => ({ ...s, loading: true }));
    fn(controller.signal)
      .then(data => setState({ data, error: null, loading: false }))
      .catch(error => { if (error.name !== 'AbortError') setState(s => ({ ...s, error, loading: false })); });
    return () => controller.abort();
  }, [...deps, version]);
  const reload = useCallback(() => setVersion(v => v + 1), []);
  return { ...state, reload };
}

// Docker events are shared by every subscriber over a single stream.
const listeners = new Set();
let closeEvents = null;

function ensureEvents() {
  if (closeEvents) return;
  closeEvents = stream('events', null, {
    event: e => listeners.forEach(fn => fn(e)),
    onerror: () => { closeEvents = null; setTimeout(() => listeners.size && ensureEvents(), 3000); },
    error: () => { closeEvents = null; setTimeout(() => listeners.size && ensureEvents(), 3000); },
  });
}

// useDockerEvents calls onChange (debounced) whenever the daemon reports an
// event of one of the given types, keeping lists live without polling.
export function useDockerEvents(types, onChange, delay = 400) {
  const callback = useRef(onChange);
  callback.current = onChange;
  useEffect(() => {
    let timer;
    const listener = e => {
      if (!types.includes(e.type)) return;
      clearTimeout(timer);
      timer = setTimeout(() => callback.current(e), delay);
    };
    listeners.add(listener);
    ensureEvents();
    return () => {
      clearTimeout(timer);
      listeners.delete(listener);
      if (!listeners.size && closeEvents) { closeEvents(); closeEvents = null; }
    };
  }, [types.join(',')]);
}

// useBusy tracks in-flight actions by key and reports their errors.
export function useBusy(onError) {
  const [busy, setBusy] = useState({});
  const run = useCallback(async (key, fn) => {
    setBusy(b => ({ ...b, [key]: true }));
    try {
      return await fn();
    } catch (err) {
      onError?.(err);
    } finally {
      setBusy(b => { const next = { ...b }; delete next[key]; return next; });
    }
  }, [onError]);
  return [busy, run];
}
