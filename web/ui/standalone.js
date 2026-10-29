// Runs the module on its own: a minimal host that reads manifest.json for
// the sidebar and drives mount() from the URL hash, exactly as a panel
// shell would.
import { html, render } from './lib/html.js';
import { mount } from './module.js';
import { Sidebar } from './node_modules/@lsongdev/scripts/react/components/index.js';
import { createHashNavigation } from './node_modules/@lsongdev/scripts/navigation/hash.js';

const navigation = createHashNavigation({ event: 'panel:routechange', stateKey: 'panelNavigation' });

const manifest = await (await fetch('manifest.json')).json();
document.title = manifest.title;

function location() {
  const raw = window.location.hash.replace(/^#/, '') || '/';
  const [path, search = ''] = raw.split('?');
  return { path: path || '/', query: Object.fromEntries(new URLSearchParams(search)) };
}

const href = (path, query) => {
  const s = new URLSearchParams(Object.entries(query || {}).filter(([, v]) => v !== '' && v != null)).toString();
  return '#' + path + (s ? '?' + s : '');
};

const navigate = navigation.navigate;

function context() {
  return {
    id: manifest.id,
    apiBase: new URL('api/', document.baseURI).href,
    assetsBase: new URL('./', document.baseURI).href,
    ...location(),
    navigate, href,
    beforeLeave: navigation.beforeLeave,
    link: () => null, // no other modules standalone
    lang: document.documentElement.lang,
  };
}

function Navigation({ path }) {
  // The longest matching entry is active; "/" only matches exactly.
  const active = (manifest.nav || [])
    .filter(i => path === i.path || (i.path !== '/' && path.startsWith(i.path + '/')))
    .sort((a, b) => b.path.length - a.path.length)[0];
  const groups = [];
  for (const item of manifest.nav || []) {
    let group = groups.find(g => g.title === (item.group || null));
    if (!group) groups.push(group = { title: item.group || null, items: [] });
    group.items.push({ href: href(item.path), title: item.title, icon: item.icon, active: item === active });
  }
  return html`<${Sidebar} brand=${manifest.title} mark="◆" groups=${groups} />`;
}

const sidebar = document.getElementById('sidebar');
const instance = mount(document.getElementById('module'), context());
const update = () => {
  const ctx = context();
  render(html`<${Navigation} path=${ctx.path} />`, sidebar);
  instance.update(ctx);
};
addEventListener('panel:routechange', update);
update();
