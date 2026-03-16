package browserconfig

import (
	"encoding/json"
	"fmt"
	"net/http"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	"github.com/alcounit/browser-controller/pkg/clientset"
	browserconfiglisters "github.com/alcounit/browser-controller/pkg/listers/browserconfig/v1"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/go-chi/chi/v5"
)

type Service struct {
	client      clientset.Interface
	lister      browserconfiglisters.BrowserConfigLister
	broadcaster broadcast.Broadcaster[event.BrowserConfigEvent]
}

func NewService(client clientset.Interface, lister browserconfiglisters.BrowserConfigLister, eventBroadcaster broadcast.Broadcaster[event.BrowserConfigEvent]) *Service {
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
		writeErrorResponse(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()

	if req.Body == nil {
		log.Error().Msg("empty request body")
		writeErrorResponse(rw, http.StatusBadRequest, "request body must not be empty", nil)
		return
	}

	var cfg browserconfigv1.BrowserConfig
	if err := json.NewDecoder(req.Body).Decode(&cfg); err != nil {
		log.Err(err).Msg("failed to decode request body")
		writeErrorResponse(rw, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if len(cfg.Spec.Browsers) == 0 {
		log.Error().Msg("failed to create browser config")
		writeErrorResponse(rw, http.StatusBadRequest, "required field is empty", nil)
		return
	}

	result, err := s.client.BrowserconfigV1().BrowserConfigs(namespace).Create(req.Context(), &cfg, metav1.CreateOptions{})
	if err != nil {
		log.Err(err).Msg("failed to create browser config")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to create browser config", err)
		return
	}

	log.Info().Str("browserConfigName", result.Name).Msg("browser config created successfully")

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(result)
}

func (s *Service) Get(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Str("browserConfigName", name).Logger()

	result, err := s.lister.BrowserConfigs(namespace).Get(name)
	if err != nil {
		if apierr.IsNotFound(err) {
			log.Warn().Msg("browser config not found")
			rw.WriteHeader(http.StatusNoContent)
			return
		}

		log.Err(err).Msg("failed to get browser config")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to retrieve browser config", err)
		return
	}

	log.Info().Msg("browser config retrieved successfully")

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(result)
}

func (s *Service) Delete(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace, name := chi.URLParam(req, "namespace"), chi.URLParam(req, "name")
	if namespace == "" || name == "" {
		log.Error().Msg("missing required url params: namespace or name")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace and name must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Str("name", name).Logger()

	if err := s.client.BrowserconfigV1().BrowserConfigs(namespace).Delete(req.Context(), name, metav1.DeleteOptions{}); err != nil {
		if apierr.IsNotFound(err) {
			rw.WriteHeader(http.StatusNoContent)
			log.Warn().Msg("browser config not found, nothing to delete")
			return
		}

		log.Err(err).Msg("failed to delete browser config")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to delete browser config", err)
		return
	}

	log.Info().Msg("browser config deleted successfully")
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Service) List(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	namespace := chi.URLParam(req, "namespace")
	if namespace == "" {
		log.Error().Msg("missing required url param: namespace")
		writeErrorResponse(rw, http.StatusBadRequest, "namespace must be provided", nil)
		return
	}

	log = log.With().Str("namespace", namespace).Logger()

	configs, err := s.lister.BrowserConfigs(namespace).List(labels.Everything())
	if err != nil {
		if apierr.IsNotFound(err) {
			log.Warn().Msg("no browser configs found in namespace")
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode([]*browserconfigv1.BrowserConfig{})
			return
		}

		log.Err(err).Msg("failed to list browser configs")
		writeErrorResponse(rw, http.StatusInternalServerError, "failed to list browser configs", err)
		return
	}

	if configs == nil {
		configs = []*browserconfigv1.BrowserConfig{}
	}

	log.Info().Int("count", len(configs)).Msg("browser configs listed successfully")

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(configs)
}

func (s *Service) Events(rw http.ResponseWriter, req *http.Request) {
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
	flusher.Flush()

	var predicate func(event.BrowserConfigEvent) bool
	if nameFilter != "" {
		predicate = func(evt event.BrowserConfigEvent) bool {
			return evt.BrowserConfig != nil && evt.BrowserConfig.GetName() == nameFilter
		}
	}

	ch := s.broadcaster.Subscribe(predicate)
	defer s.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-req.Context().Done():
			log.Info().Msg("browser config events stream closed by client")
			return
		case evt, ok := <-ch:
			if !ok {
				log.Info().Msg("browser config events channel closed")
				flusher.Flush()
				return
			}

			if evt.BrowserConfig == nil {
				continue
			}

			data, err := json.Marshal(evt)
			if err != nil {
				log.Err(err).Msg("failed to encode browser config event")
				return
			}
			fmt.Fprintf(rw, "data: %s\n\n", data)
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
