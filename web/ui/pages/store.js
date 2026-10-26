import { html, useEffect, useState } from '../lib/html.js';
import { useResource } from '../lib/hooks.js';
import { Page, Resource, Badge } from '../lib/ui.js';
import { datetime } from '../lib/format.js';
import { usePanel } from '../lib/panel.js';
import { startJob } from '../docker.js';

export function AppIcon({ id, large }) {
  const { api } = usePanel();
  const [failed, setFailed] = useState(false);
  return failed
    ? html`<span class=${`app-icon ${large ? 'app-icon-lg' : ''}`}></span>`
    : html`<img class=${`app-icon ${large ? 'app-icon-lg' : ''}`} src=${api.iconURL(id)} alt="" loading="lazy" onError=${() => setFailed(true)} />`;
}

export default function Store({ query }) {
  const { api, href, setQuery } = usePanel();
  const category = query.category || '';
  // Debounce the search box so typing does not fire a request per key.
  const [search, setSearch] = useState(query.q || '');
  useEffect(() => {
    const t = setTimeout(() => setQuery({ q: search }), 250);
    return () => clearTimeout(t);
  }, [search]);
  const catalog = useResource(signal => api.catalog({ search: query.q, category }, { signal }), [query.q, category]);
  const categories = useResource(signal => api.categories({ signal }));
  const sync = () => startJob(api, () => api.syncCatalog(), { onDone: () => { catalog.reload(); categories.reload(); } }).catch(() => {});
  const loadedAt = catalog.data?.loaded_at;
  const synced = loadedAt && !loadedAt.startsWith('0001');
  return html`<${Page} title="App store" subtitle=${synced ? `Catalog updated ${datetime(loadedAt)}` : 'The catalog has not been synced yet'}
    actions=${html`<button class="button" onClick=${sync}>Sync catalog</button>`}>
    <div class="toolbar">
      <input class="input" type="search" placeholder="Search apps" value=${search} onInput=${e => setSearch(e.target.value)} />
    </div>
    <div class="chips">
      <button class=${`chip ${!category ? 'active' : ''}`} onClick=${() => setQuery({ category: '' })}>All</button>
      ${(categories.data || []).map(c => html`<button class=${`chip ${category === c.name ? 'active' : ''}`}
        onClick=${() => setQuery({ category: c.name })}>${c.name} <span class="muted">${c.count}</span></button>`)}
    </div>
    <${Resource} resource=${{ ...catalog, data: catalog.data?.apps }}
      empty=${synced ? 'No matching apps' : html`The catalog is empty; click Sync catalog to download it`}>${apps => html`
      <div class="app-grid">${apps.map(app => html`
        <a class="card app-card" key=${app.id} href=${href(`/store/${app.id}`)}>
          <div class="app-head">
            <${AppIcon} id=${app.id} />
            <div style="min-width:0">
              <h3>${app.name}</h3>
              <div class="cell-sub">${app.version} · ${app.categories.join(', ')}</div>
            </div>
          </div>
          <p>${app.short_desc}</p>
          <div>${app.installed ? html`<${Badge} tone="green">Installed<//>` : null}</div>
        </a>`)}
      </div>`}
    <//>
  <//>`;
}
