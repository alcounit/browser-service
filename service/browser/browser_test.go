package browser

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
	body, _ := json.Marshal(&browserv1.Browser{})
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
	cs.Fake.PrependReactor("create", "browsers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("create failed")
	})

	svc := NewService(cs, nil, nil)
	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"
	body, _ := json.Marshal(browser)

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
	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"
	body, _ := json.Marshal(browser)

	req := newRequestWithParams(http.MethodPost, "/", bytes.NewBuffer(body), map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.Create(rw, req)

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
	lister := listers.NewBrowserLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	svc := NewService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestGetSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	lister := newListerWith(browser)
	svc := NewService(nil, lister, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.Get(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestGetError(t *testing.T) {
	svc := NewService(nil, &fakeLister{getErr: errors.New("boom")}, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default", "name": "br"})
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
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
}

func TestDeleteError(t *testing.T) {
	cs := fakeclientset.NewSimpleClientset()
	cs.Fake.PrependReactor("delete", "browsers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	svc := NewService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "br"})
	rw := httptest.NewRecorder()

	svc.Delete(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestDeleteSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	cs := fakeclientset.NewSimpleClientset(browser)
	svc := NewService(cs, nil, nil)
	req := newRequestWithParams(http.MethodDelete, "/", nil, map[string]string{"namespace": "default", "name": "br"})
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
	svc := NewService(nil, newListerWith(), nil)
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

	var got []*browserv1.Browser
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestListNotFound(t *testing.T) {
	svc := NewService(nil, &fakeLister{
		listErr: apierr.NewNotFound(browserv1.Resource("browser"), "br"),
	}, nil)
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := httptest.NewRecorder()

	svc.List(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestListSuccess(t *testing.T) {
	browser := &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "br", Namespace: "default"}}
	svc := NewService(nil, newListerWith(browser), nil)
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
	svc := NewService(nil, nil, broadcast.NewBroadcaster[event.BrowserEvent](1))
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{})
	rw := httptest.NewRecorder()

	svc.Events(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestEventsNoFlusher(t *testing.T) {
	svc := NewService(nil, nil, broadcast.NewBroadcaster[event.BrowserEvent](1))
	req := newRequestWithParams(http.MethodGet, "/", nil, map[string]string{"namespace": "default"})
	rw := &noFlusherRW{header: make(http.Header)}

	svc.Events(rw, req)

	if rw.status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.status)
	}
}

func TestEventsSuccess(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserEvent](1)
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

func TestEventsNameFilter(t *testing.T) {
	b := broadcast.NewBroadcaster[event.BrowserEvent](2)
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

// fakes

type fakeLister struct {
	listErr error
	getErr  error
	list    []*browserv1.Browser
}

func (f *fakeLister) List(selector labels.Selector) ([]*browserv1.Browser, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeLister) Browsers(namespace string) listers.BrowserNamespaceLister {
	return &fakeNamespaceLister{listErr: f.listErr, getErr: f.getErr, list: f.list}
}

type fakeNamespaceLister struct {
	listErr error
	getErr  error
	list    []*browserv1.Browser
}

func (f *fakeNamespaceLister) List(selector labels.Selector) ([]*browserv1.Browser, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeNamespaceLister) Get(name string) (*browserv1.Browser, error) {
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

type closingBroadcaster struct{}

func (c *closingBroadcaster) Subscribe(_ ...func(event.BrowserEvent) bool) chan event.BrowserEvent {
	ch := make(chan event.BrowserEvent)
	close(ch)
	return ch
}
func (c *closingBroadcaster) Unsubscribe(chan event.BrowserEvent) {}
func (c *closingBroadcaster) Broadcast(event.BrowserEvent)        {}

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

func newListerWith(browsers ...*browserv1.Browser) listers.BrowserLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, b := range browsers {
		indexer.Add(b)
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
