package browserconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	fakeclientset "github.com/alcounit/browser-controller/pkg/clientset/fake"
	browserconfiglisters "github.com/alcounit/browser-controller/pkg/listers/browserconfig/v1"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/go-chi/chi/v5"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestCreateMissingNamespace(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateEmptyBody(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/", nil, map[string]string{"namespace": "default"})
	req.Body = nil
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/", bytes.NewBufferString("{"), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateMissingSpecFields(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	body, _ := json.Marshal(&browserconfigv1.BrowserConfig{})
	req := newRequestWithParams(http.MethodPost, "/", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got["message"] != "required field is empty" {
		t.Fatalf("unexpected message: %q", got["message"])
	}
}

func TestCreateClientError(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	cs.Fake.PrependReactor("create", "browserconfigs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("create failed")
	})

	svc := NewService(cs, nil, nil)
	body, _ := json.Marshal(newTestConfig("cfg"))
	req := newRequestWithParams(http.MethodPost, "/", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateSuccess(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	svc := NewService(cs, nil, nil)
	body, _ := json.Marshal(newTestConfig("cfg"))
	req := newRequestWithParams(http.MethodPost, "/", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got browserconfigv1.BrowserConfig
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Name != "cfg" {
		t.Fatalf("unexpected name: %s", got.Name)
	}
}

func TestGetMissingParams(t *testing.T) {
	svc := NewService(nil, nil, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	lister := browserconfiglisters.NewBrowserConfigLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	svc := NewService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rw.Code)
	}
	if ct := rw.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json body, got %q", ct)
	}
}

func TestGetSuccess(t *testing.T) {
	cfg := &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"}}
	svc := NewService(nil, newConfigListerWith(cfg), nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestGetError(t *testing.T) {
	svc := NewService(nil, &fakeLister{getErr: errors.New("boom")}, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestDeleteMissingParams(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestDeleteNotFound(t *testing.T) {
	svc := NewService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestDeleteError(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	cs.Fake.PrependReactor("delete", "browserconfigs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	svc := NewService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestDeleteSuccess(t *testing.T) {
	cfg := &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"}}
	cs := fakeclientset.NewSimpleClientset(cfg)
	svc := NewService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "cfg"})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestListMissingNamespace(t *testing.T) {
	svc := NewService(nil, nil, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestListEmpty(t *testing.T) {
	svc := NewService(nil, newConfigListerWith(), nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	body := strings.TrimSpace(rw.Body.String())
	if body == "null" {
		t.Fatal("expected empty JSON array [], got null")
	}

	var got []*browserconfigv1.BrowserConfig
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestListNotFound(t *testing.T) {
	svc := NewService(nil, &fakeLister{
		listErr: apierr.NewNotFound(browserconfigv1.Resource("browserconfig"), "cfg"),
	}, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestListSuccess(t *testing.T) {
	cfg := &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"}}
	svc := NewService(nil, newConfigListerWith(cfg), nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestListError(t *testing.T) {
	svc := NewService(nil, &fakeLister{listErr: errors.New("boom")}, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestEventsMissingNamespace(t *testing.T) {
	svc := NewService(nil, nil, broadcast.NewBroadcaster[event.BrowserConfigEvent](1))
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.Events(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestEventsNoFlusher(t *testing.T) {
	svc := NewService(nil, nil, broadcast.NewBroadcaster[event.BrowserConfigEvent](1))
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := &noFlusherRW{header: make(http.Header)}

	svc.Events(rw, req)

	if rw.status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.status)
	}
}

func TestEventsSuccess(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserConfigEvent](1)
	svc := NewService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &recordingRW{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.Events(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserConfigEvent{
		EventType:     event.EventTypeAdded,
		BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}},
	})

	if !waitFor(func() bool { return rw.Len() > 0 }, 2*time.Second) {
		cancel()
		t.Fatal("timed out waiting for event write")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to exit")
	}

	if !bytes.Contains(rw.Bytes(), []byte(`"eventType"`)) {
		t.Fatalf("expected event JSON, got %s", rw.String())
	}
}

func TestEventsChannelClosed(t *testing.T) {
	svc := NewService(nil, nil, &closingBroadcaster{})

	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := &recordingRW{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.Events(rw, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to exit")
	}
}

func TestEventsNameFilter(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserConfigEvent](2)
	svc := NewService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/?name=target", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &recordingRW{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.Events(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserConfigEvent{EventType: event.EventTypeAdded, BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "other"}}})

	if waitFor(func() bool { return rw.Len() > 0 }, 150*time.Millisecond) {
		cancel()
		t.Fatal("unexpected event write for non-matching name")
	}

	b.Broadcast(event.BrowserConfigEvent{EventType: event.EventTypeAdded, BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "target"}}})

	if !waitFor(func() bool { return rw.Len() > 0 }, 2*time.Second) {
		cancel()
		t.Fatal("timed out waiting for matching event")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to exit")
	}
}

func TestWriteErrorResponse(t *testing.T) {
	rw := httptest.NewRecorder()
	writeErrorResponse(rw, http.StatusBadRequest, "bad request", errors.New("detail"))

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
	if ct := rw.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if cl := rw.Header().Get("Content-Length"); cl == "" {
		t.Fatal("expected Content-Length to be set")
	}

	var got map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if got["message"] != "bad request" {
		t.Fatalf("unexpected message: %q", got["message"])
	}
	if got["details"] != "detail" {
		t.Fatalf("unexpected details: %q", got["details"])
	}
}

func TestWriteErrorResponseNoError(t *testing.T) {
	rw := httptest.NewRecorder()
	writeErrorResponse(rw, http.StatusInternalServerError, "internal", nil)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if got["details"] != "" {
		t.Fatalf("expected empty details, got %q", got["details"])
	}
}

func TestWriteJSONSetsContentLength(t *testing.T) {
	cfg := &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}}
	rw := httptest.NewRecorder()

	if err := writeJSON(rw, cfg); err != nil {
		t.Fatalf("writeJSON returned error: %v", err)
	}
	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
	if cl := rw.Header().Get("Content-Length"); cl == "" {
		t.Fatal("expected Content-Length to be set")
	}

	var got browserconfigv1.BrowserConfig
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Name != "cfg" {
		t.Fatalf("unexpected name: %q", got.Name)
	}
}

func TestEventsNilBrowserConfigSkipped(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserConfigEvent](2)
	svc := NewService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &recordingRW{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.Events(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserConfigEvent{EventType: event.EventTypeAdded, BrowserConfig: nil})

	if waitFor(func() bool { return rw.Len() > 0 }, 150*time.Millisecond) {
		cancel()
		t.Fatal("expected nil browserconfig event to be skipped")
	}

	b.Broadcast(event.BrowserConfigEvent{EventType: event.EventTypeAdded, BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}}})

	if !waitFor(func() bool { return rw.Len() > 0 }, 2*time.Second) {
		cancel()
		t.Fatal("timed out waiting for valid event")
	}

	cancel()
	<-done
}

// fakes

type fakeLister struct {
	listErr error
	getErr  error
	list    []*browserconfigv1.BrowserConfig
}

func (f *fakeLister) List(selector labels.Selector) ([]*browserconfigv1.BrowserConfig, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeLister) BrowserConfigs(namespace string) browserconfiglisters.BrowserConfigNamespaceLister {
	return &fakeNamespaceLister{listErr: f.listErr, getErr: f.getErr, list: f.list}
}

type fakeNamespaceLister struct {
	listErr error
	getErr  error
	list    []*browserconfigv1.BrowserConfig
}

func (f *fakeNamespaceLister) List(selector labels.Selector) ([]*browserconfigv1.BrowserConfig, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeNamespaceLister) Get(name string) (*browserconfigv1.BrowserConfig, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, c := range f.list {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, apierr.NewNotFound(browserconfigv1.Resource("browserconfig"), name)
}

type closingBroadcaster struct{}

func (c *closingBroadcaster) Subscribe(_ ...func(event.BrowserConfigEvent) bool) chan event.BrowserConfigEvent {
	ch := make(chan event.BrowserConfigEvent)
	close(ch)
	return ch
}
func (c *closingBroadcaster) Unsubscribe(chan event.BrowserConfigEvent) {}
func (c *closingBroadcaster) Broadcast(event.BrowserConfigEvent)        {}

type recordingRW struct {
	header http.Header
	buf    bytes.Buffer
	status int
	mu     sync.Mutex
}

func (r *recordingRW) Header() http.Header { return r.header }
func (r *recordingRW) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.buf.Write(p)
}
func (r *recordingRW) WriteHeader(s int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = s
}
func (r *recordingRW) Flush() {}
func (r *recordingRW) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}
func (r *recordingRW) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf.Bytes()...)
}
func (r *recordingRW) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

type noFlusherRW struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (n *noFlusherRW) Header() http.Header         { return n.header }
func (n *noFlusherRW) Write(p []byte) (int, error) { return n.buf.Write(p) }
func (n *noFlusherRW) WriteHeader(s int)           { n.status = s }

func newRequestWithParams(method, path string, body io.Reader, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, body)
	rctx := chi.NewRouteContext()
	for key, val := range params {
		rctx.URLParams.Add(key, val)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func newConfigListerWith(cfgs ...*browserconfigv1.BrowserConfig) browserconfiglisters.BrowserConfigLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, c := range cfgs {
		indexer.Add(c)
	}
	return browserconfiglisters.NewBrowserConfigLister(indexer)
}

func newTestConfig(name string) *browserconfigv1.BrowserConfig {
	return &browserconfigv1.BrowserConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: browserconfigv1.BrowserConfigSpec{
			Browsers: map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec{
				"chrome": {"120": {Image: "chrome:120"}},
			},
		},
	}
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
