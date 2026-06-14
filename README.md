[![GitHub release](https://img.shields.io/github/v/release/alcounit/browser-service)](https://github.com/alcounit/browser-service/releases) [![Go Reference](https://pkg.go.dev/badge/github.com/alcounit/browser-service.svg)](https://pkg.go.dev/github.com/alcounit/browser-service) [![Docker Pulls](https://img.shields.io/docker/pulls/alcounit/browser-service.svg)](https://hub.docker.com/r/alcounit/browser-service) [![codecov](https://codecov.io/gh/alcounit/browser-service/branch/main/graph/badge.svg)](https://codecov.io/gh/alcounit/browser-service) [![Go Report Card](https://goreportcard.com/badge/github.com/alcounit/browser-service)](https://goreportcard.com/report/github.com/alcounit/browser-service)

# Browser Service

Browser Service is an HTTP API and event streaming layer for the **Selenosis** ecosystem.
It provides a REST interface and real-time events on top of the `Browser` and `BrowserConfig` resources managed by **browser-controller**.

This service does **not** create Pods directly. Instead, it operates as a client-facing facade over Kubernetes CRDs.

---

## Overview

- **Browser Service** exposes CRUD and event APIs for browser instances and their configurations.
- **browser-controller** is responsible for reconciliation and Pod lifecycle.
- **Browser** and **BrowserConfig** CRDs are the shared contract between the service and the controller.

The service watches both `Browser` and `BrowserConfig` resources and streams lifecycle events to clients.

---

## Responsibilities

- Expose HTTP API for managing `Browser` and `BrowserConfig` resources
- Stream lifecycle events (ADDED / MODIFIED / DELETED) for both resource types
- Provide read-only access to `Browser.status`
- Act as a thin abstraction over Kubernetes APIs

---

## Dependency on browser-controller

Browser Service **depends on browser-controller** for:

- `Browser` and `BrowserConfig` CRD definitions
- Generated clientsets, informers, and listers
- Actual browser Pod creation and lifecycle management

The service assumes that:
- CRDs are already installed
- browser-controller is running in the cluster

---

## API Overview

All APIs are namespaced.

### Browser endpoints

- `POST   /api/v1/namespaces/{namespace}/browsers`
  Create a new `Browser`

- `GET    /api/v1/namespaces/{namespace}/browsers/{name}`
  Get a single `Browser`

- `DELETE /api/v1/namespaces/{namespace}/browsers/{name}`
  Delete a `Browser`

- `GET    /api/v1/namespaces/{namespace}/browsers`
  List all Browsers in a namespace

- `GET    /api/v1/namespaces/{namespace}/browsers/events`
  Stream browser lifecycle events (server-sent events over HTTP)

  Query parameters (optional):
  - `name` — filter events by browser name.
    When specified, only events related to the given browser are streamed.

### BrowserConfig endpoints

- `POST   /api/v1/namespaces/{namespace}/browserconfigs`
  Create a new `BrowserConfig`

- `GET    /api/v1/namespaces/{namespace}/browserconfigs/{name}`
  Get a single `BrowserConfig`

- `DELETE /api/v1/namespaces/{namespace}/browserconfigs/{name}`
  Delete a `BrowserConfig`

- `GET    /api/v1/namespaces/{namespace}/browserconfigs`
  List all BrowserConfigs in a namespace

- `GET    /api/v1/namespaces/{namespace}/browserconfigs/events`
  Stream BrowserConfig lifecycle events (server-sent events over HTTP)

  Query parameters (optional):
  - `name` — filter events by BrowserConfig name.
    When specified, only events related to the given BrowserConfig are streamed.

---

## Events

Both resource types expose a streaming endpoint backed by shared informers.

Event types:

- `ADDED`
- `MODIFIED`
- `DELETED`

`Browser` events include the full `Browser` object as payload, including `status.phase` and `status.podIP`.
`BrowserConfig` events include the full `BrowserConfig` object as payload.

---

## Runtime Model

- The service uses shared informers to watch `Browser` and `BrowserConfig` resources.
- Events are broadcast to connected clients using an in-memory fan-out broadcaster.
- Subscribers can optionally provide a predicate to receive only events matching a specific resource name.
- No state is persisted outside of Kubernetes.
- All writes are forwarded directly to the Kubernetes API server.

---

## Health Endpoint

```http
GET /healthz
```

Returns `200 OK` when the service is running.

---

## Build and image workflow

The project is built and packaged entirely via Docker. Local Go installation is not required for producing the final artifact.

## Build variables

The build process is controlled via the following Makefile variables:

| Variable         | Description                                                  |
|------------------|--------------------------------------------------------------|
| `BINARY_NAME`    | Name of the produced binary (fixed: `browser-service`)       |
| `REGISTRY`       | Docker registry prefix (default: `localhost:5000`)           |
| `IMAGE_NAME`     | Full image name, derived as `$(REGISTRY)/$(BINARY_NAME)`     |
| `VERSION`        | Image version/tag (default: `develop`)                       |
| `EXTRA_TAGS`     | Additional `-t` tags passed to `docker-push` (default: none) |
| `PLATFORM`       | Target platform (default: `linux/amd64`)                     |
| `CONTAINER_TOOL` | Container build tool (default: `docker`)                     |

`REGISTRY` and `VERSION` are expected to be provided externally, which allows the same Makefile to be used locally and in CI.

## Deployment

Helm chart [selenosis-deploy](https://github.com/alcounit/selenosis-deploy)
