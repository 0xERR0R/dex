package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "github.com/sirupsen/logrus"
)

func main() {
	prometheus.MustRegister(newDockerCollector())

	router := http.NewServeMux()
	router.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:         fmt.Sprintf(":%v", 8080),
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	done := make(chan bool)

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)

	go func() {
		<-quit
		log.Info("Server is shutting down...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("Could not gracefully shutdown the server: %v\n", err)
		}
		close(done)
	}()

	log.Info("Server is ready to handle requests at :", 8080)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not listen on %d: %v\n", 8080, err)
	}

	<-done
	log.Info("Server stopped")
}
