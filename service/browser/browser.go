package browser

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-controller/pkg/clientset"
	listers "github.com/alcounit/browser-controller/pkg/listers/browser/v1"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/go-chi/chi/v5"
)

var (
	sseDataPrefix = []byte("data: ")
	sseDataSuffix = []byte("\n\n")
)

type Service struct {
	client      clientset.Interface
	lister      listers.BrowserLister
	broadcaster broadcast.Broadcaster[event.BrowserEvent]
}

func NewService(client clientset.Interface, lister listers.BrowserLister, eventBroadcaster broadcast.Broadcaster[event.BrowserEvent]) *Service {
	return &Service{
		client:      client,
		lister:      lister,
		broadcaster: eventBroadcaster,
	}
}

func (s *Service) Create(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeJSONError(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()
	if req.Body == nil {
		log.Error().Msg("empty request body")
		writeJSONError(rw, http.StatusBadRequest, "request body must not be empty", nil)
		return
	}

	var browser browserv1.Browser
	if err := json.NewDecoder(req.Body).Decode(&browser); err != nil {
		log.Err(err).Msg("failed to decode request body")
		writeJSONError(rw, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if browser.Spec.BrowserName == "" || browser.Spec.BrowserVersion == "" {
		log.Error().Msg("failed to create browser")
		writeJSONError(rw, http.StatusBadRequest, "required field is empty", nil)
		return
	}

	result, err := s.client.BrowserV1().Browsers(namespace).Create(req.Context(), &browser, metav1.CreateOptions{})
	if err != nil {
		log.Err(err).Msg("failed to create browser")
		writeJSONError(rw, http.StatusInternalServerError, "failed to create browser", err)
		return
	}

	log.Info().Str("browserName", result.Name).Msg("browser created successfully")
	writeJSON(rw, result)
}

func (s *Service) Get(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeJSONError(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Str("browserName", name).Logger()
	result, err := s.lister.Browsers(namespace).Get(name)
	if err != nil {
		if apierr.IsNotFound(err) {
			log.Warn().Msg("browser not found")
			rw.WriteHeader(http.StatusNoContent)
			return
		}

		log.Err(err).Msg("failed to get browser")
		writeJSONError(rw, http.StatusInternalServerError, "failed to retrieve browser", err)
		return
	}

	log.Info().Str("phase", string(result.Status.Phase)).Msg("browser retrieved successfully")
	writeJSON(rw, result)
}

func (s *Service) Delete(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeJSONError(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Str("name", name).Logger()

	if err := s.client.BrowserV1().Browsers(namespace).Delete(req.Context(), name, metav1.DeleteOptions{}); err != nil {
		if apierr.IsNotFound(err) {
			rw.WriteHeader(http.StatusNoContent)
			log.Warn().Msg("browser not found, nothing to delete")
			return
		}

		log.Err(err).Msg("failed to delete browser")
		writeJSONError(rw, http.StatusInternalServerError, "failed to delete browser", err)
		return
	}

	log.Info().Msg("browser deleted successfully")
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Service) List(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeJSONError(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()
	browsers, err := s.lister.Browsers(namespace).List(labels.Everything())
	if err != nil {
		if apierr.IsNotFound(err) {
			log.Warn().Msg("no browsers found in namespace")
			writeJSON(rw, []*browserv1.Browser{})
			return
		}

		log.Err(err).Msg("failed to list browsers")
		writeJSONError(rw, http.StatusInternalServerError, "failed to list browsers", err)
		return
	}

	if browsers == nil {
		browsers = []*browserv1.Browser{}
	}

	log.Info().Int("count", len(browsers)).Msg("browsers listed successfully")
	writeJSON(rw, browsers)
}

func (s *Service) Events(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeJSONError(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()

	nameFilter := req.URL.Query().Get("name")
	if nameFilter != "" {
		log = log.With().Str("name", nameFilter).Logger()
	}

	flusher, ok := rw.(http.Flusher)
	if !ok {
		log.Error().Msg("response writer does not support http.Flusher")
		writeJSONError(rw, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	var predicate func(event.BrowserEvent) bool
	if nameFilter != "" {
		predicate = func(evt event.BrowserEvent) bool {
			return evt.Browser != nil && evt.Browser.GetName() == nameFilter
		}
	}

	ch := s.broadcaster.Subscribe(predicate)
	defer s.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-req.Context().Done():
			log.Info().Msg("browser events stream closed by client")
			return
		case evt, ok := <-ch:
			if !ok {
				log.Info().Msg("browser events channel closed")
				flusher.Flush()
				return
			}

			if evt.Browser == nil {
				continue
			}

			data, err := json.Marshal(evt)
			if err != nil {
				log.Err(err).Msg("failed to encode browser event")
				return
			}
			rw.Write(sseDataPrefix)
			rw.Write(data)
			rw.Write(sseDataSuffix)
			flusher.Flush()
		}
	}
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details"`
}

func writeJSON(rw http.ResponseWriter, v any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeJSONError(rw, http.StatusInternalServerError, "failed to encode response", err)
		return err
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	buf.WriteTo(rw)
	return nil
}

func writeJSONError(rw http.ResponseWriter, status int, msg string, err error) {
	details := ""
	if err != nil {
		details = err.Error()
	}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(errorResponse{
		Error:   http.StatusText(status),
		Message: msg,
		Details: details,
	})
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	rw.WriteHeader(status)
	buf.WriteTo(rw)
}
