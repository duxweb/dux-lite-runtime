package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
)

type Service struct {
	config *config.Config
	master *phpmaster.Client
}

func New(cfg *config.Config, master *phpmaster.Client) *Service {
	return &Service{
		config: cfg,
		master: master,
	}
}

func (s *Service) Run(ctx context.Context) error {
	if !s.config.RealtimeEnabled {
		<-ctx.Done()
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/auth", s.handleAuth)

	server := &http.Server{
		Addr:    s.config.RealtimeListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	log.Printf("realtime: listening on %s", s.config.RealtimeListenAddr)
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Service) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"ok":      true,
		"service": "dux-runtime-realtime",
	})
}

func (s *Service) handleAuth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var payload phpmaster.WsAuthRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := s.master.WsAuth(request.Context(), payload)
	if err != nil {
		if errors.Is(err, phpmaster.ErrUnavailable) {
			http.Error(writer, err.Error(), http.StatusNotImplemented)
			return
		}
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}
