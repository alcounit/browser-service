package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"

	"github.com/alcounit/browser-controller/pkg/clientset"
	"github.com/alcounit/browser-controller/pkg/informers/externalversions"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-service/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var kubeconfig string
	var masterURL string
	var addr string

	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (if running outside cluster)")
	flag.StringVar(&masterURL, "master", "", "Kubernetes API server address")
	flag.StringVar(&addr, "listen", ":8080", "Address to listen on")
	flag.Parse()

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := buildConfig(masterURL, kubeconfig)
	if err != nil {
		log.Err(err).Msg("error building kubeconfig")
		os.Exit(1)
	}

	cs, err := clientset.NewForConfig(cfg)
	if err != nil {
		log.Err(err).Msg("error building browser clientset")
		os.Exit(1)
	}

	factory := externalversions.NewSharedInformerFactory(cs, 10*time.Second)
	informer := factory.Selenosis().V1().Browsers().Informer()
	broadcaster := broadcast.NewBroadcaster[event.BrowserEvent](16)

	informer.AddEventHandler(browserEventHandler(broadcaster))

	lister := factory.Selenosis().V1().Browsers().Lister()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory.Start(ctx.Done())

	if ok := cache.WaitForCacheSync(ctx.Done(), informer.HasSynced); !ok {
		log.Error().Msg("failed to sync caches")
		os.Exit(1)
	}

	svc := service.NewBrowserService(cs, lister, broadcaster)

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		fn := func(rw http.ResponseWriter, req *http.Request) {
			logger := log.With().
				Str("method", req.Method).
				Str("path", req.URL.Path).
				Str("reqID", uuid.NewString()).
				Logger()

			ctx := req.Context()
			ctx = logctx.IntoContext(ctx, logger)

			next.ServeHTTP(rw, req.WithContext(ctx))
		}
		return http.HandlerFunc(fn)
	})

	router.Route("/api/v1/namespaces/{namespace}", func(r chi.Router) {
		r.Post("/browsers", svc.CreateBrowser)
		r.Get("/browsers/{name}", svc.GetBrowser)
		r.Delete("/browsers/{name}", svc.DeleteBrowser)
		r.Get("/browsers", svc.ListBrowsers)
		r.Get("/events", svc.BrowserEvents)
	})

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Info().Msgf("HTTP server listening %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Err(err).Msg("HTTP server error")
			os.Exit(1)
		}
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh
	log.Info().Msg("Shutting down HTTP server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Err(err).Msg("HTTP server shutdown error")
		os.Exit(1)
	}
}

func buildConfig(masterURL, kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	}
	return rest.InClusterConfig()
}

func browserEventHandler(broadcaster broadcast.Broadcaster[event.BrowserEvent]) cache.ResourceEventHandler {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			browser := obj.(*browserv1.Browser)
			broadcaster.Broadcast(event.BrowserEvent{
				EventType: event.EventTypeAdded,
				Browser:   browser.DeepCopy(),
			})
		},
		UpdateFunc: func(oldObj, newObj any) {
			browser := newObj.(*browserv1.Browser)
			broadcaster.Broadcast(event.BrowserEvent{
				EventType: event.EventTypeModified,
				Browser:   browser.DeepCopy(),
			})
		},
		DeleteFunc: func(obj any) {
			browser := obj.(*browserv1.Browser)
			broadcaster.Broadcast(event.BrowserEvent{
				EventType: event.EventTypeDeleted,
				Browser:   browser.DeepCopy(),
			})
		},
	}
}
