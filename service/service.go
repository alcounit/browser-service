package service

import (
	"encoding/json"

	"net/http"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"

	"github.com/alcounit/browser-controller/pkg/clientset"
	listers "github.com/alcounit/browser-controller/pkg/listers/browser/v1"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BrowserService struct {
	client      clientset.Interface
	lister      listers.BrowserLister
	broadcaster broadcast.Broadcaster[event.BrowserEvent]
}

func NewBrowserService(client clientset.Interface, lister listers.BrowserLister, eventBroadcaster broadcast.Broadcaster[event.BrowserEvent]) *BrowserService {
	return &BrowserService{
		client:      client,
		lister:      lister,
		broadcaster: eventBroadcaster,
	}
}

func (s *BrowserService) CreateBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()
	if req.Body == nil {
		log.Error().Msg("empty request body")
		writeErrorResponse(rw, http.StatusBadRequest, "request body must not be empty", nil)
		return
	}

	var browser browserv1.Browser
	if err := json.NewDecoder(req.Body).Decode(&browser); err != nil {
		log.Err(err).Msg("failed to decode request body")
		writeErrorResponse(rw, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if browser.Spec.BrowserName == "" || browser.Spec.BrowserVersion == "" {
		log.Error().Msg("failed to create browser")
		writeErrorResponse(rw, http.StatusInternalServerError, "requered field is empty", nil)
		return
	}

	result, err := s.client.SelenosisV1().Browsers(namespace).Create(req.Context(), &browser, metav1.CreateOptions{})
	if err != nil {
		log.Err(err).Msg("failed to create browser")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to create browser", err)
		return
	}

	log.Info().Str("browserName", result.Name).Msg("browser created successfully")

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(result)

}

func (s *BrowserService) GetBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
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
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to retrieve browser", err)
		return
	}

	log.Info().Str("phase", string(result.Status.Phase)).Msg("browser retrieved successfully")

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(result)
}

func (s *BrowserService) DeleteBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Str("name", name).Logger()

	if err := s.client.SelenosisV1().Browsers(namespace).Delete(req.Context(), name, metav1.DeleteOptions{}); err != nil {
		if apierr.IsNotFound(err) {
			rw.WriteHeader(http.StatusNoContent)
			log.Warn().Msg("browser not found, nothing to delete")
			return
		}

		log.Err(err).Msg("failed to delete browser")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to delete browser", err)
		return
	}

	log.Info().Msg("browser deleted successfully")

	rw.WriteHeader(http.StatusNoContent)
}

func (s *BrowserService) ListBrowsers(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()
	browsers, err := s.lister.Browsers(namespace).List(labels.Everything())

	if err != nil {
		if apierr.IsNotFound(err) {
			log.Warn().Msg("no browsers found in namespace")
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode([]*browserv1.Browser{})
			return
		}

		log.Err(err).Msg("failed to list browsers")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to list browsers", err)
		return
	}

	log.Info().Int("count", len(browsers)).Msg("browsers listed successfully")

	rw.Header().Set("Content-Type", "application/json")
	if len(browsers) < 1 {
		json.NewEncoder(rw).Encode([]*browserv1.Browser{})
		return
	}

	json.NewEncoder(rw).Encode(browsers)
}

func (s *BrowserService) BrowserEvents(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace must be provided", nil)
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
		writeErrorResponse(rw, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no")

	ch := s.broadcaster.Subscribe()
	defer s.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-req.Context().Done():
			log.Info().Msg("browser events stream closed by client")
			return
		case event, ok := <-ch:
			if !ok {
				log.Info().Msg("browser events channel closed")
				flusher.Flush()
				return
			}

			if event.Browser == nil {
				continue
			}

			if nameFilter != "" {
				if event.Browser.GetName() != nameFilter {
					continue
				}
			}

			rw.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(rw).Encode(event)
			if err != nil {
				log.Err(err).Msg("failed to encode browser event")
				flusher.Flush()
				continue
			}
			flusher.Flush()
		}
	}
}

func writeErrorResponse(rw http.ResponseWriter, status int, msg string, err error) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	json.NewEncoder(rw).Encode(map[string]string{
		"error":   http.StatusText(status),
		"message": msg,
		"details": func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
	})
}
