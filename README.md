# docker-go

A self-hosted Docker management panel: containers, images, volumes, networks,
Compose stacks, and an app store that installs apps from a git catalog
(default: [lsongdev/ohmyserver-apps](https://github.com/lsongdev/ohmyserver-apps)).

One static Go binary. The UI is Preact + HTM loaded as native ES modules, with
no build step. It needs no database, and its only Go dependency is
`coder/websocket`.

docker-go is also a **panel module**: it follows the module contract
(panelkit's `CONTRACT.md`), so a panel shell can mount it beside other
modules without docker-go depending on that shell.

```sh
go run ./cmd/docker-go -listen 127.0.0.1:8080 -data ./data
```

The daemon is found the same way the docker CLI finds it: `DOCKER_HOST`, then
`DOCKER_CONTEXT`, then the current docker context, then
`/var/run/docker.sock`. `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH` and
`DOCKER_API_VERSION` are honoured. The panel also needs the `docker compose`
plugin (or `docker-compose`) and `git` on the host.

| Flag       | Default       | Purpose |
|------------|---------------|---------|
| `-listen`  | `127.0.0.1:8080` | HTTP address |
| `-data`    | `./data`      | Holds `stacks/`, `apps/`, `catalog/` and `storage/` |
| `-catalog` | ohmyserver-apps | App catalog git URL; empty uses `<data>/catalog` as is |
| `-host`    | from `-listen` | Address users reach installed apps at |
| `-storage` | `<data>/storage` | Shared media root exposed to apps (`ROOT_FOLDER_HOST`) |
| `-tz`      | system        | `TZ` passed to apps |

> The panel controls the Docker daemon, which is root-equivalent. Set
> `DOCKER_GO_PASSWORD` to require HTTP basic authentication (any user name);
> without it, keep the panel on localhost.

## Architecture

Each layer depends only on the layers below it:

```
web/       L4  Module assembly (web.New) + browser UI (ES modules, mount contract)
api/       L3  HTTP: JSON envelope, SSE streams, WebSocket terminal, its own DTOs
apps/      L2  App lifecycle: plan → apply, built on compose + catalog + engine
catalog/   L2  App catalog: git sync, manifest parsing, install-form validation
jobs/      L2  Background operations with log fan-out
compose/   L1  docker compose CLI wrapper + on-disk stack store
engine/    L1  Docker Engine API client (net/http over unix socket / TCP+TLS)
cmd/docker-go  Wiring, flags, graceful shutdown
```

**engine** talks to the Engine API directly instead of using the Docker SDK.
It negotiates the API version (supports 1.44–1.56), demultiplexes
stdout/stderr streams, decodes progress streams, hijacks connections for
exec, and builds tar build contexts with `.dockerignore` support. Wire types
keep the Engine's field names; nothing above `api/` sees them.

**compose** delegates to the official CLI rather than reimplementing
Compose. Managed stacks live in `<data>/stacks/<name>/` (`compose.yaml` +
`.env`). Edits are validated with `compose config` and rolled back if
invalid. Projects started elsewhere are listed read-only. Cancelling a
command interrupts its whole process group.

**catalog** keeps the catalog in memory (a few hundred manifests). No
database is needed; it is reloaded from the checkout on each sync.

**apps** separates validation from execution. `PlanInstall`/`PlanUpdate`
validate synchronously (form fields, architecture, host-port conflicts), so
the API can answer 422 with per-field errors. `Apply` then runs as a job. A
failed or cancelled fresh install is rolled back. Each installed app is a
self-contained directory:

```
<data>/apps/<id>/compose.yaml           the catalog template
                 runtime.compose.yaml   resources templates expect from the host
                 .env                   every variable (plain `docker compose` works here)
                 app.json               install record: version, port, settings
                 data/                  seed files + persistent data (APP_DATA_DIR)
```

Apps can also be published at their own domain through an optional
`apps.Exposer` (for example a TLS-terminating reverse proxy). Without one the
domain setting is hidden and apps are reached at host:port.

Install records go through `apps.Store`. The default `FileStore` keeps them
as `app.json` beside each app's compose files. A host that centralises state
(for example in its own database) can pass another implementation in
`web.Options.Store`.

**web** wires every layer together (`web.New(ctx, web.Options)`).
`Docker.Handler` and `Docker.Assets` satisfy the module contract;
`Docker.Standalone` serves both on their own. The standalone command and a
panel use the same assembly.

**jobs** runs installs, pulls, builds and compose actions in the background.
At most one job per resource. Logs fan out to any number of SSE observers, so
the UI can close the dialog and reattach later.

## App catalog contract

Templates follow the runtipi layout: `manifest.json` + `docker-compose.yml`
(plus an optional `docker-compose.<arch>.yml`, logo, README and seed dirs).
The panel provides these variables:

`APP_ID`, `APP_PORT`, `APP_DATA_DIR`, `APP_HOST`, `APP_DOMAIN` (host:port),
`APP_PROTOCOL`, `APP_EXPOSED`, `LOCAL_DOMAIN`, `ROOT_FOLDER_HOST`, `TZ`,
`DNS_IP`, `INTERNAL_IP`, `NETWORK_INTERFACE`, plus each form field's
`env_variable`.

Host-provided values take precedence over form values. `random` fields are
generated once and preserved across updates and reconfiguration. Templates
may attach services to `default_network` without declaring it; the panel
declares it in `runtime.compose.yaml`.

## HTTP API

All routes are relative to the mount point (`/api/` standalone,
`/m/docker/api/` in a panel). A daemon that cannot be reached answers 503
`docker_unavailable`. Success is `{"data": …}`; failure is
`{"error": {"code", "message", "fields"}}`. List fields are always arrays.
Write requests must be same-origin JSON with no unknown fields.

| Area | Routes |
|------|--------|
| System | `GET /system`, `GET /events` (SSE) |
| Containers | `GET/POST /containers`, `GET/DELETE /containers/{id}`, `POST /containers/{id}/{start,stop,restart,kill,pause,unpause,rename}`, `GET /containers/{id}/logs` (`follow=1` → SSE), `GET /containers/{id}/stats` (SSE), `GET /containers/{id}/terminal` (WebSocket), `POST /containers/prune` |
| Images | `GET /images`, `GET/DELETE /images/{id}`, `POST /images/pull`, `POST /images/build`, `POST /images/prune` |
| Volumes | `GET/POST /volumes`, `GET/DELETE /volumes/{name}`, `POST /volumes/prune` |
| Networks | `GET/POST /networks`, `GET/DELETE /networks/{id}`, `POST /networks/prune` |
| Stacks | `GET/POST /stacks`, `GET/PUT/DELETE /stacks/{name}`, `POST /stacks/{name}/{up,down,start,stop,restart,pull}` |
| Catalog | `GET /catalog?search=&category=`, `GET /catalog/categories`, `GET /catalog/{id}`, `GET /catalog/{id}/icon`, `POST /catalog/sync` |
| Apps | `GET/POST /apps`, `GET/PATCH/DELETE /apps/{id}`, `POST /apps/{id}/{start,stop,restart}` |
| Jobs | `GET /jobs`, `GET /jobs/{id}`, `GET /jobs/{id}/events` (SSE), `POST /jobs/{id}/cancel` |

Long operations answer `202` with a job; follow it at `/jobs/{id}/events`
(`snapshot`, then `line`…, then `done`).

The terminal speaks raw bytes in binary frames. The client sends
`{"type":"resize","cols":N,"rows":N}` as text frames. The socket closes with
the shell's exit code as the reason.

## Development

```sh
go test ./...                                 # unit + HTTP contract tests (fake daemon)
go test -race ./...
go test -tags integration ./engine            # against a real daemon
```

`engine/enginetest` provides a fake daemon for tests in any layer.

The UI is self-contained: `web/ui/manifest.json` describes it,
`module.js` exports `mount(el, ctx)`, and `index.html` + `standalone.js` host
it on its own. Shared components are in `web/ui/lib`; Preact, htm and
xterm.js are vendored in `web/ui/vendor`. The UI works offline and under a
strict Content-Security-Policy, and its styles follow a panel's `--panel-*`
theme variables when mounted.
