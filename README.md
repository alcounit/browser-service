# Browser Service

Browser Service is a small HTTP API that exposes CRUD operations for `Browser` custom resources managed by the Browser Controller. It sits in front of Kubernetes and provides a stable REST interface for systems that need to create, read, delete, and list browser sessions without talking to the Kubernetes API directly.

This service is typically used by higher-level components (for example, Selenosis) that spin up browser pods on demand.

## Overview
- Stateless HTTP server.
- Uses the generated Kubernetes clientset to create/delete `Browser` resources.
- Uses a shared informer and lister for cached reads and event streaming.
- Streams browser events as a JSON event stream.

## Architecture

### Entry point
`cmd/service/main.go` initializes:
- CLI flags (`--kubeconfig`, `--master`, `--listen`).
- Zerolog logger.
- Kubernetes config (in-cluster or kubeconfig).
- Browser Controller clientset.
- Shared informer factory and `Browser` lister.
- HTTP router and request-scoped logging.
- Health endpoint and graceful shutdown.

The server listens on `--listen` (default `:8080`).

### HTTP handlers
`service/service.go` defines `BrowserService` with:
- `clientset.Interface` for writes.
- `BrowserLister` for cached reads.
- Broadcaster for event streaming.

Handlers:
- `POST /api/v1/namespaces/{namespace}/browsers` creates a `Browser` CR.
- `GET /api/v1/namespaces/{namespace}/browsers/{name}` fetches a `Browser` from cache.
- `DELETE /api/v1/namespaces/{namespace}/browsers/{name}` deletes a `Browser`.
- `GET /api/v1/namespaces/{namespace}/browsers` lists all `Browser` resources in a namespace.
- `GET /api/v1/namespaces/{namespace}/events` streams browser events as JSON objects.
- `GET /healthz` returns `200 OK`.

## HTTP API

Base path:
```
/api/v1/namespaces/{namespace}
```

### Create browser
`POST /browsers`

Example request:
```json
{
  "apiVersion": "selenosis.io/v1",
  "kind": "Browser",
  "metadata": {"name": "931b1f38-0818-421c-9606-83e1c81a26fb"},
  "spec": {
    "browserName": "chrome",
    "browserVersion": "122.0"
  }
}
```

Validation:
- `namespace` is required.
- Request body must not be empty.
- `spec.browserName` and `spec.browserVersion` must be set.

### Get browser
`GET /browsers/{name}`

Returns `204 No Content` if not found.

### Delete browser
`DELETE /browsers/{name}`

Returns `204 No Content` even if the resource is already deleted.

### List browsers
`GET /browsers`

Returns a JSON array (empty array when no browsers exist).

### Events stream
`GET /events`

The endpoint responds with `Content-Type: text/event-stream` and flushes a JSON-encoded `BrowserEvent` on each update. The stream is a sequence of JSON objects (not SSE `data:` frames).

Event payload:
```json
{
  "eventType": "ADDED",
  "browser": {
    "metadata": {"name": "931b1f38-0818-421c-9606-83e1c81a26fb"},
    "spec": {"browserName": "chrome", "browserVersion": "122.0"}
  }
}
```

### Health
`GET /healthz` returns `ok` with status `200`.

## Errors
Errors are returned as JSON:
```json
{
  "error": "Bad Request",
  "message": "namespace must be provided",
  "details": "<optional error string>"
}
```

`IsNotFound` from Kubernetes is mapped to `204 No Content` in Get/Delete.

## Browser client library
The Go client lives in `pkg/client` and provides:
- Create/Get/Delete/List operations.
- Event stream helper via `Events(ctx, namespace)`.
- Structured errors via `APIError`.

Minimal example:
```go
cfg := client.ClientConfig{BaseURL: "http://browser-service:8080"}
c, _ := client.NewClient(cfg)

ctx := context.Background()

browser := &browserv1.Browser{
  Spec: browserv1.BrowserSpec{
    BrowserName:    "chrome",
    BrowserVersion: "122.0",
  },
}

created, _ := c.CreateBrowser(ctx, "default", browser)
_ = created
```

## Build and image workflow

The project is built and packaged entirely via Docker. Local Go installation is not required for producing the final artifact.

## Build variables

The build process is controlled via the following Makefile variables:

Variable	Description
- BINARY_NAME	Name of the produced binary (browser-service).
- DOCKER_REGISTRY	Docker registry prefix (passed via environment).
- IMAGE_NAME	Full image name (<registry>/browser-service).
- VERSION	Image version/tag (default: :v0.0.1).
- PLATFORM	Target platform (default: linux/amd64).

DOCKER_REGISTRY is expected to be provided externally, which allows the same Makefile to be used locally and in CI.

## Deployment

To be added....

## Notes
- The service is stateless and can be scaled horizontally.
- It depends on the Browser Controller to reconcile `Browser` CRs into actual pods.
