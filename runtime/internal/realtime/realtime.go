package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/gateway"
	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
	"github.com/duxweb/dux-runtime/runtime/internal/status"
)

type Service struct {
	config *config.Config
	master *phpmaster.Client
	state  *status.State
	gw     *gateway.Service
}

func New(cfg *config.Config, master *phpmaster.Client, state *status.State) *Service {
	return &Service{
		config: cfg,
		master: master,
		state:  state,
		gw:     gateway.New(cfg, master, state),
	}
}

func (s *Service) Run(ctx context.Context) error {
	if !s.config.RealtimeEnabled {
		<-ctx.Done()
		return nil
	}
	go func() {
		if err := s.gw.RunAdmin(ctx, s.config.GatewaySocketPath); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("gateway admin: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/auth", s.handleAuth)
	mux.HandleFunc("/ws", s.gw.HandleWS)

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
	response := map[string]any{
		"ok":      true,
		"service": "dux-runtime-realtime",
	}
	if s.state != nil {
		response["runtime"] = s.state.Snapshot()
	}
	_ = json.NewEncoder(writer).Encode(response)
}

func (s *Service) handleMetrics(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	if s.state == nil {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok": false,
		})
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"ok":      true,
		"service": "dux-runtime-realtime",
		"runtime": s.state.Snapshot(),
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
