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
	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"

	"github.com/alcounit/browser-controller/pkg/clientset"
	"github.com/alcounit/browser-controller/pkg/informers/externalversions"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/event"
	browsersvc "github.com/alcounit/browser-service/service/browser"
	browserconfigsvc "github.com/alcounit/browser-service/service/browserconfig"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	broadcasterBufSize = 256
	broadcastQueueSize = 1024
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

	factory := externalversions.NewSharedInformerFactory(cs, 1*time.Minute)

	browserInformer := factory.Browser().V1().Browsers().Informer()
	browserBroadcaster := broadcast.NewBroadcaster[event.BrowserEvent](broadcasterBufSize)
	browserLister := factory.Browser().V1().Browsers().Lister()

	configInformer := factory.Browserconfig().V1().BrowserConfigs().Informer()
	configBroadcaster := broadcast.NewBroadcaster[event.BrowserConfigEvent](broadcasterBufSize)
	configLister := factory.Browserconfig().V1().BrowserConfigs().Lister()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	browserQueue := startBroadcastPipeline(ctx, browserBroadcaster)
	browserInformer.AddEventHandler(browserEventHandler(browserQueue))

	configQueue := startBroadcastPipeline(ctx, configBroadcaster)
	configInformer.AddEventHandler(browserConfigEventHandler(configQueue))

	factory.Start(ctx.Done())

	if ok := cache.WaitForCacheSync(ctx.Done(), browserInformer.HasSynced, configInformer.HasSynced); !ok {
		log.Error().Msg("failed to sync caches")
		os.Exit(1)
	}

	brwSvc := browsersvc.NewService(cs, browserLister, browserBroadcaster)
	cfgSvc := browserconfigsvc.NewService(cs, configLister, configBroadcaster)

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
		r.Route("/browsers", func(r chi.Router) {
			r.Post("/", brwSvc.Create)
			r.Get("/{name}", brwSvc.Get)
			r.Delete("/{name}", brwSvc.Delete)
			r.Get("/", brwSvc.List)
			r.Get("/events", brwSvc.Events)
		})

		r.Route("/browserconfigs", func(r chi.Router) {
			r.Post("/", cfgSvc.Create)
			r.Get("/{name}", cfgSvc.Get)
			r.Delete("/{name}", cfgSvc.Delete)
			r.Get("/", cfgSvc.List)
			r.Get("/events", cfgSvc.Events)
		})
	})

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10, // 64 KB; default 1 MB is excessive
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

	cancel()
	factory.Shutdown()
	log.Info().Msg("Informers stopped")
}

func buildConfig(masterURL, kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	}
	return rest.InClusterConfig()
}

func startBroadcastPipeline[T any](ctx context.Context, b broadcast.Broadcaster[T]) chan<- T {
	queue := make(chan T, broadcastQueueSize)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-queue:
				if !ok {
					return
				}
				b.Broadcast(evt)
			}
		}
	}()
	return queue
}

func browserEventHandler(queue chan<- event.BrowserEvent) cache.ResourceEventHandler {
	send := func(evt event.BrowserEvent) {
		select {
		case queue <- evt:
		default:
		}
	}

	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			browser, ok := obj.(*browserv1.Browser)
			if !ok {
				return
			}

			send(event.BrowserEvent{
				EventType: event.EventTypeAdded,
				Browser:   browser.DeepCopy(),
			})
		},
		UpdateFunc: func(oldObj, newObj any) {
			old, ok := oldObj.(*browserv1.Browser)
			if !ok {
				return
			}

			new, ok := newObj.(*browserv1.Browser)
			if !ok {
				return
			}

			if old.ResourceVersion == new.ResourceVersion {
				return
			}

			send(event.BrowserEvent{
				EventType: event.EventTypeModified,
				Browser:   new.DeepCopy(),
			})
		},
		DeleteFunc: func(obj any) {
			browser, ok := obj.(*browserv1.Browser)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}

				browser, ok = tombstone.Obj.(*browserv1.Browser)
				if !ok {
					return
				}
			}
			send(event.BrowserEvent{
				EventType: event.EventTypeDeleted,
				Browser:   browser.DeepCopy(),
			})
		},
	}
}

func browserConfigEventHandler(queue chan<- event.BrowserConfigEvent) cache.ResourceEventHandler {
	send := func(evt event.BrowserConfigEvent) {
		select {
		case queue <- evt:
		default:
		}
	}

	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			cfg, ok := obj.(*browserconfigv1.BrowserConfig)
			if !ok {
				return
			}

			send(event.BrowserConfigEvent{
				EventType:     event.EventTypeAdded,
				BrowserConfig: cfg.DeepCopy(),
			})
		},
		UpdateFunc: func(oldObj, newObj any) {
			old, ok := oldObj.(*browserconfigv1.BrowserConfig)
			if !ok {
				return
			}

			new, ok := newObj.(*browserconfigv1.BrowserConfig)
			if !ok {
				return
			}

			if old.ResourceVersion == new.ResourceVersion {
				return
			}

			send(event.BrowserConfigEvent{
				EventType:     event.EventTypeModified,
				BrowserConfig: new.DeepCopy(),
			})
		},
		DeleteFunc: func(obj any) {
			cfg, ok := obj.(*browserconfigv1.BrowserConfig)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}

				cfg, ok = tombstone.Obj.(*browserconfigv1.BrowserConfig)
				if !ok {
					return
				}
			}
			send(event.BrowserConfigEvent{
				EventType:     event.EventTypeDeleted,
				BrowserConfig: cfg.DeepCopy(),
			})
		},
	}
}
