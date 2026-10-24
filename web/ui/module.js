// Docker module entry point (panel module contract, version 1). The host
// is a panel shell, or standalone.js on its own.
import { createMount } from './lib/mount.js';
import { dockerAPI } from './docker.js';

const page = load => ({ load });

const routes = [
  { path: '/', ...page(() => import('./pages/dashboard.js')) },
  { path: '/containers', ...page(() => import('./pages/containers.js')) },
  { path: '/containers/new', ...page(() => import('./pages/container-new.js')) },
  { path: '/containers/:id', ...page(() => import('./pages/container.js')) },
  { path: '/images', ...page(() => import('./pages/images.js')) },
  { path: '/images/build', ...page(() => import('./pages/image-build.js')) },
  { path: '/images/:id', ...page(() => import('./pages/image.js')) },
  { path: '/volumes', ...page(() => import('./pages/volumes.js')) },
  { path: '/volumes/new', ...page(() => import('./pages/volume-new.js')) },
  { path: '/networks', ...page(() => import('./pages/networks.js')) },
  { path: '/networks/new', ...page(() => import('./pages/network-new.js')) },
  { path: '/networks/:id', ...page(() => import('./pages/network.js')) },
  { path: '/stacks', ...page(() => import('./pages/stacks.js')) },
  { path: '/stacks/new', ...page(() => import('./pages/stack-new.js')) },
  { path: '/stacks/:name', ...page(() => import('./pages/stack.js')) },
  { path: '/store', ...page(() => import('./pages/store.js')) },
  { path: '/store/:id', ...page(() => import('./pages/store-app.js')) },
  { path: '/apps', ...page(() => import('./pages/apps.js')) },
  { path: '/apps/:id', ...page(() => import('./pages/app.js')) },
  { path: '/jobs', ...page(() => import('./pages/jobs.js')) },
];

export const mount = createMount({ routes, api: dockerAPI });
