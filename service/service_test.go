package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	fakeclientset "github.com/alcounit/browser-controller/pkg/clientset/fake"
	listers "github.com/alcounit/browser-controller/pkg/listers/browser/v1"
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

func TestCreateBrowserMissingNamespace(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces//browsers", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserEmptyBody(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces/default/browsers", nil, map[string]string{"namespace": "default"})
	req.Body = nil
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserInvalidJSON(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces/default/browsers", bytes.NewBufferString("{"), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserMissingSpecFields(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	body, _ := json.Marshal(&browserv1.Browser{})
	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces/default/browsers", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got["message"] != "requered field is empty" {
		t.Fatalf("unexpected message: %q", got["message"])
	}
}

func TestCreateBrowserClientError(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	cs.Fake.PrependReactor("create", "browsers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("create failed")
	})

	svc := NewBrowserService(cs, nil, nil)
	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"
	body, _ := json.Marshal(browser)

	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces/default/browsers", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserSuccess(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	svc := NewBrowserService(cs, nil, nil)
	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"
	body, _ := json.Marshal(browser)

	req := newRequestWithParams(http.MethodPost, "/api/v1/namespaces/default/browsers", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got browserv1.Browser
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Name != "br" {
		t.Fatalf("unexpected browser name: %s", got.Name)
	}
}

func TestGetBrowserMissingParams(t *testing.T) {
	svc := NewBrowserService(nil, nil, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestGetBrowserNotFound(t *testing.T) {
	lister := listers.NewBrowserLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	svc := NewBrowserService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestGetBrowserSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	lister := newListerWithBrowsers(browser)
	svc := NewBrowserService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestGetBrowserError(t *testing.T) {
	svc := NewBrowserService(nil, &fakeBrowserLister{getErr: errors.New("boom")}, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestDeleteBrowserMissingParams(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/api/v1/namespaces/default/browsers", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestDeleteBrowserNotFound(t *testing.T) {
	svc := NewBrowserService(fakeclientset.NewSimpleClientset(), nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestDeleteBrowserError(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	cs.Fake.PrependReactor("delete", "browsers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	svc := NewBrowserService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestDeleteBrowserSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	cs := fakeclientset.NewSimpleClientset(browser)
	svc := NewBrowserService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/api/v1/namespaces/default/browsers/br", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestListBrowsersMissingNamespace(t *testing.T) {
	svc := NewBrowserService(nil, nil, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces//browsers", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestListBrowsersEmpty(t *testing.T) {
	lister := newListerWithBrowsers()
	svc := NewBrowserService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got []*browserv1.Browser
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestListBrowsersNotFound(t *testing.T) {
	svc := NewBrowserService(nil, &fakeBrowserLister{
		listErr: apierr.NewNotFound(browserv1.Resource("browser"), "br"),
	}, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got []*browserv1.Browser
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestListBrowsersSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	lister := newListerWithBrowsers(browser)
	svc := NewBrowserService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestListBrowsersError(t *testing.T) {
	svc := NewBrowserService(nil, &fakeBrowserLister{listErr: errors.New("boom")}, nil)
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/browsers", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestBrowserEventsMissingNamespace(t *testing.T) {
	svc := NewBrowserService(nil, nil, broadcast.NewBroadcaster[event.BrowserEvent](1))
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces//events", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.BrowserEvents(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestBrowserEventsNoFlusher(t *testing.T) {
	svc := NewBrowserService(nil, nil, broadcast.NewBroadcaster[event.BrowserEvent](1))
	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/events", nil, map[string]string{"namespace": "default"})
	rw := &noFlusherResponseWriter{header: make(http.Header)}

	svc.BrowserEvents(rw, req)

	if rw.status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.status)
	}
}

func TestBrowserEventsSuccess(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserEvent](1)
	svc := NewBrowserService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/events", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &recordingResponseWriter{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.BrowserEvents(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserEvent{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br"}}})

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

func TestBrowserEventsNameFilter(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserEvent](2)
	svc := NewBrowserService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/events?name=target", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &recordingResponseWriter{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.BrowserEvents(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserEvent{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "other"}}})

	if waitFor(func() bool { return rw.Len() > 0 }, 150*time.Millisecond) {
		cancel()
		t.Fatal("unexpected event write for non-matching name")
	}

	b.Broadcast(event.BrowserEvent{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "target"}}})

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

func TestBrowserEventsChannelClosed(t *testing.T) {
	b := &closingBroadcaster{}
	svc := NewBrowserService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/events", nil, map[string]string{"namespace": "default"})
	rw := &recordingResponseWriter{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.BrowserEvents(rw, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to exit")
	}
}

func TestBrowserEventsEncodeError(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserEvent](1)
	svc := NewBrowserService(nil, nil, b)

	req := newRequestWithParams(http.MethodGet, "/api/v1/namespaces/default/events", nil, map[string]string{"namespace": "default"})
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rw := &errorWriteResponseWriter{header: make(http.Header)}
	done := make(chan struct{})

	go func() {
		svc.BrowserEvents(rw, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	b.Broadcast(event.BrowserEvent{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br"}}})

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to exit")
	}
}

func TestWriteErrorResponse(t *testing.T) {
	rw := httptest.NewRecorder()
	writeErrorResponse(rw, http.StatusBadRequest, "bad", errors.New("details"))

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got["details"] != "details" {
		t.Fatalf("expected details, got %q", got["details"])
	}
}

type fakeBrowserLister struct {
	listErr error
	getErr  error
	list    []*browserv1.Browser
}

func (f *fakeBrowserLister) List(selector labels.Selector) ([]*browserv1.Browser, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeBrowserLister) Browsers(namespace string) listers.BrowserNamespaceLister {
	return &fakeBrowserNamespaceLister{
		listErr: f.listErr,
		getErr:  f.getErr,
		list:    f.list,
	}
}

type fakeBrowserNamespaceLister struct {
	listErr error
	getErr  error
	list    []*browserv1.Browser
}

func (f *fakeBrowserNamespaceLister) List(selector labels.Selector) ([]*browserv1.Browser, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeBrowserNamespaceLister) Get(name string) (*browserv1.Browser, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, b := range f.list {
		if b.Name == name {
			return b, nil
		}
	}
	return nil, apierr.NewNotFound(browserv1.Resource("browser"), name)
}

type recordingResponseWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
	mu     sync.Mutex
}

func (r *recordingResponseWriter) Header() http.Header {
	return r.header
}

func (r *recordingResponseWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.buf.Write(p)
}

func (r *recordingResponseWriter) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = statusCode
}

func (r *recordingResponseWriter) Flush() {}

func (r *recordingResponseWriter) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}

func (r *recordingResponseWriter) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf.Bytes()...)
}

func (r *recordingResponseWriter) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

type noFlusherResponseWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (n *noFlusherResponseWriter) Header() http.Header {
	return n.header
}

func (n *noFlusherResponseWriter) Write(p []byte) (int, error) {
	if n.status == 0 {
		n.status = http.StatusOK
	}
	return n.buf.Write(p)
}

func (n *noFlusherResponseWriter) WriteHeader(statusCode int) {
	n.status = statusCode
}

type errorWriteResponseWriter struct {
	header http.Header
}

func (e *errorWriteResponseWriter) Header() http.Header {
	return e.header
}

func (e *errorWriteResponseWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func (e *errorWriteResponseWriter) WriteHeader(statusCode int) {}

func (e *errorWriteResponseWriter) Flush() {}

type closingBroadcaster struct{}

func (c *closingBroadcaster) Subscribe() chan event.BrowserEvent {
	ch := make(chan event.BrowserEvent)
	close(ch)
	return ch
}

func (c *closingBroadcaster) Unsubscribe(ch chan event.BrowserEvent) {}

func (c *closingBroadcaster) Broadcast(event.BrowserEvent) {}

func newRequestWithParams(method, path string, body io.Reader, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, body)
	rctx := chi.NewRouteContext()
	for key, val := range params {
		rctx.URLParams.Add(key, val)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func newListerWithBrowsers(browsers ...*browserv1.Browser) listers.BrowserLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, browser := range browsers {
		indexer.Add(browser)
	}
	return listers.NewBrowserLister(indexer)
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
