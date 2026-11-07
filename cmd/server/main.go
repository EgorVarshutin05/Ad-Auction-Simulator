package main

import (
	"ad-auction/internal/api"
	"ad-auction/internal/metrics"
	"ad-auction/internal/service"
	"ad-auction/internal/storage"
	"ad-auction/web"
	"context"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	databaseURL := env("DATABASE_URL", "postgres://auction:auction_dev_password@localhost:5433/auction_db?sslmode=disable")

	pgCtx, pgCancel := context.WithTimeout(context.Background(), 5*time.Second)

	postgresStore, err := storage.NewPostgres(pgCtx, databaseURL)
	pgCancel()

	if err != nil {
		log.Fatalf("PostgreSQL connection: %v", err)
	}
	defer postgresStore.Close()

	log.Println("PostgreSQL connected successfully")

	runtime.SetMutexProfileFraction(1)

	kind := env("AUCTION_TYPE", "second-price")
	if kind != "first-price" && kind != "second-price" {
		log.Fatal("AUCTION_TYPE must be first-price or second-price")
	}

	auctionService := &service.Service{
		History:     postgresStore,
		StatsStore:  postgresStore,
		AuctionType: kind,
	}

	handler := &api.Server{Service: auctionService}

	mux := http.NewServeMux()

	page, err := web.Handler()
	if err != nil {
		log.Fatalf("build web page: %v", err)
	}

	mux.Handle("/", page)

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(web.Files))))

	mux.HandleFunc("/bid", handler.Bid)
	mux.HandleFunc("/winner", handler.Winner)
	mux.HandleFunc("/stats", handler.Stats)
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              ":8080",
		Handler:           metrics.Middleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/debug/pprof/", pprof.Index)
	debugMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	debugServer := &http.Server{
		Addr:              "127.0.0.1:6060",
		Handler:           debugMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := debugServer.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Printf("pprof: %v", err)
		}
	}()

	go func() {
		log.Printf("auction server :8080 (%s)", kind)

		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	<-signals

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = server.Shutdown(shutdownCtx)
	_ = debugServer.Shutdown(shutdownCtx)
}
