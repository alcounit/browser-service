# browser-service

**REST + Server-Sent-Events facade over the [Selenosis](https://github.com/alcounit/selenosis) `Browser` and `BrowserConfig` resources.**
It gives clients a plain HTTP API and a real-time event stream on top of the Kubernetes
CRDs — without ever creating Pods itself.

[![GitHub release](https://img.shields.io/github/v/release/alcounit/browser-service)](https://github.com/alcounit/browser-service/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/alcounit/browser-service.svg)](https://pkg.go.dev/github.com/alcounit/browser-service)
[![Docker Pulls](https://img.shields.io/docker/pulls/alcounit/browser-service.svg)](https://hub.docker.com/r/alcounit/browser-service)
[![codecov](https://codecov.io/gh/alcounit/browser-service/branch/main/graph/badge.svg)](https://codecov.io/gh/alcounit/browser-service)
[![Go Report Card](https://goreportcard.com/badge/github.com/alcounit/browser-service)](https://goreportcard.com/report/github.com/alcounit/browser-service)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)

---

## What it does

- **CRUD over CRDs.** Create, get, list, and delete `Browser` and `BrowserConfig`
  resources through a namespaced HTTP API.
- **Real-time events.** Streams `ADDED` / `MODIFIED` / `DELETED` lifecycle events for both
  resource types over Server-Sent Events, optionally filtered by name.
- **Read-only status.** Surfaces `Browser.status` (phase, pod IP, failure message) so
  clients can wait on readiness or react to failures.
- **A thin, stateless facade.** It does **not** create Pods or act as a controller — all
  writes go straight to the Kubernetes API server; no state is kept outside Kubernetes.

It's the boundary between protocol semantics and Kubernetes resources: the selenosis hub
calls it to create sessions and watch for readiness, and browser-ui consumes its event
stream for the live session list.

---

## How it fits

| Component | Role |
| --- | --- |
| **[selenosis](https://github.com/alcounit/selenosis)** | Stateless Selenium / Playwright / MCP hub. Calls this service to create sessions and watch readiness. |
| **[seleniferous](https://github.com/alcounit/seleniferous)** | Sidecar proxy inside each browser pod. |
| **[browser-controller](https://github.com/alcounit/browser-controller)** | Operator that reconciles `Browser` / `BrowserConfig` CRDs into pods. Owns the CRD types this service uses. |
| **[browser-service](https://github.com/alcounit/browser-service)** (this repo) | REST + SSE facade over `Browser` and `BrowserConfig` resources. |
| **[browser-ui](https://github.com/alcounit/browser-ui)** | Dashboard; consumes this service's event stream + VNC. |
| **[selenosis-deploy](https://github.com/alcounit/selenosis-deploy)** | Helm chart that deploys the whole stack. **Start here.** |

> Depends on **browser-controller** for the `Browser` / `BrowserConfig` CRD definitions and
> generated clientsets/informers/listers. CRDs must be installed and the controller running.

---

## API

All endpoints are namespaced under `/api/v1/namespaces/{namespace}`.

### Browsers

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/browsers` | Create a `Browser`. |
| `GET` | `/browsers/{name}` | Get a single `Browser`. |
| `DELETE` | `/browsers/{name}` | Delete a `Browser`. |
| `GET` | `/browsers` | List browsers in the namespace. |
| `GET` | `/browsers/events` | Stream browser lifecycle events (SSE). Optional `?name=` to filter. |

### BrowserConfigs

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/browserconfigs` | Create a `BrowserConfig`. |
| `GET` | `/browserconfigs/{name}` | Get a single `BrowserConfig`. |
| `DELETE` | `/browserconfigs/{name}` | Delete a `BrowserConfig`. |
| `GET` | `/browserconfigs` | List browserconfigs in the namespace. |
| `GET` | `/browserconfigs/events` | Stream BrowserConfig lifecycle events (SSE). Optional `?name=` to filter. |

### Health

`GET /healthz` → `200 OK` when the service is running.

---

## Events & runtime model

Event types are `ADDED`, `MODIFIED`, and `DELETED`. `Browser` events carry the full
`Browser` object (incl. `status.phase` / `status.podIP` / failure message);
`BrowserConfig` events carry the full `BrowserConfig`.

<details>
<summary><b>How events are produced and fanned out</b></summary>

- The service uses **shared informers** to watch `Browser` and `BrowserConfig` resources.
- Events are delivered to connected clients through an **in-memory fan-out broadcaster**.
- Subscribers may pass a **predicate** to receive only events for a specific resource name.
- Nothing is persisted outside Kubernetes; all writes forward directly to the API server.

</details>

---

## Build

The project builds and packages entirely via Docker — a local Go install is not required.

<details>
<summary><b>Makefile build variables</b></summary>

| Variable | Description |
| --- | --- |
| `BINARY_NAME` | Name of the produced binary (fixed: `browser-service`). |
| `REGISTRY` | Docker registry prefix (default: `localhost:5000`). |
| `IMAGE_NAME` | Full image name, derived as `$(REGISTRY)/$(BINARY_NAME)`. |
| `VERSION` | Image version/tag (default: `develop`). |
| `EXTRA_TAGS` | Additional `-t` tags passed to `docker-push` (default: none). |
| `PLATFORM` | Target platform (default: `linux/amd64`). |
| `CONTAINER_TOOL` | Container build tool (default: `docker`). |

`REGISTRY` and `VERSION` are expected to be supplied externally so the same Makefile works
locally and in CI.

</details>

---

## Deployment

Deployed as part of the full stack via the
[selenosis-deploy](https://github.com/alcounit/selenosis-deploy) Helm chart.

---

## License

[Apache-2.0](./LICENSE)
