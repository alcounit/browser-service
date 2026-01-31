# Browser Service

Browser Service is an HTTP API and event streaming layer for the **Selenosis** ecosystem.  
It provides a REST interface and real-time events on top of the `Browser` resources managed by **browser-controller**.

This service does **not** create Pods directly. Instead, it operates as a client-facing facade over Kubernetes `Browser` CRDs.

---

## Overview

- **Browser Service** exposes CRUD and event APIs for browser instances.
- **browser-controller** is responsible for reconciliation and Pod lifecycle.
- **Browser CRD** is the shared contract between the service and the controller.

The service watches `Browser` resources and streams lifecycle events to clients.

---

## Responsibilities

- Expose HTTP API for managing `Browser` resources
- Stream browser lifecycle events (ADDED / MODIFIED / DELETED)
- Provide read-only access to `Browser.status`
- Act as a thin abstraction over Kubernetes APIs

---

## Dependency on browser-controller

Browser Service **depends on browser-controller** for:

- `Browser` CRD definitions
- Generated clientsets, informers, and listers
- Actual browser Pod creation and lifecycle management

The service assumes that:
- CRDs are already installed
- browser-controller is running in the cluster

---

## API Overview

All APIs are namespaced.


### Endpoints

- `POST   /api/v1/namespaces/{namespace}/browsers`  
  Create a new `Browser`

- `GET    /api/v1/namespaces/{namespace}/browsers/{name}`  
  Get a single `Browser`

- `DELETE /api/v1/namespaces/{namespace}/browsers/{name}`  
  Delete a `Browser`

- `GET    /api/v1/namespaces/{namespace}/browsers`  
  List all Browsers in a namespace

- `GET    /api/v1/namespaces/{namespace}/events`  
  Stream browser events (server-sent events over HTTP)
  Query parameters (optional):
  - `name` — filter events by browser name.  
    When specified, only events related to the given browser are streamed.

---

## Browser Events

The service exposes a streaming endpoint backed by shared informers.

Event types:

- `ADDED`
- `MODIFIED`
- `DELETED`

Each event contains the full `Browser` object as payload.

---

## Runtime Model

- The service uses shared informers to watch `Browser` resources.
- Events are broadcast to connected clients using an in-memory fan-out broadcaster.
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

Variable	Description
- BINARY_NAME	Name of the produced binary (selenosis).
- REGISTRY	Docker registry prefix (default: localhost:5000).
- IMAGE_NAME	Full image name (<registry>/selenosis).
- VERSION	Image version/tag (default: develop).
- PLATFORM	Target platform (default: linux/amd64).
- CONTAINER_TOOL docker cmd

REGISTRY, VERSION is expected to be provided externally, which allows the same Makefile to be used locally and in CI.

## Deployment

Minimal configuration

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: browser-service
  namespace: default
```

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: browser-service
rules:
  - apiGroups: ["selenosis.io"]
    resources: ["browsers"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
```

``` yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: browser-service
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: browser-service
subjects:
- kind: ServiceAccount
  name: browser-service
  namespace: default
```

``` yaml
apiVersion: v1
kind: Service
metadata:
  name: browser-service
  labels:
    role: browser-service
spec:
  type: NodePort
  selector:
    role: browser-service
  ports:
  - name: http
    port: 8080
    targetPort: 8080

```

``` yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: browser-service
  labels:
    role: browser-service
spec:
  replicas: 1
  selector:
    matchLabels:
      role: browser-service
  template:
    metadata:
      labels:
        role: browser-service
    spec:
      serviceAccountName: browser-service
      containers:
      - name: service
        image: alcounit/browser-service:latest
        imagePullPolicy: IfNotPresent
        args:
          - "--listen=:8080"
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "500m"
            memory: "256Mi"
```
