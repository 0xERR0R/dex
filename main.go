package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	reg := prometheus.NewRegistry()

	var labels []string

	if rawLabels, isSet := os.LookupEnv("DEX_LABELS"); isSet {
		labels = strings.Split(rawLabels, ",")
	}

	reg.MustRegister(newDockerCollector(labels))

	router := http.NewServeMux()
	router.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry: reg,
	}))

	serverPort := 8080

	if strPort, isSet := os.LookupEnv("DEX_PORT"); isSet {
		if intPort, err := strconv.Atoi(strPort); err == nil {
			serverPort = intPort
		}
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%v", serverPort),
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	done := make(chan bool)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	go func() {
		<-quit
		slog.Info("Server is shutting down...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Could not gracefully shutdown the server", "error", err)
			os.Exit(1)
		}
		close(done)
	}()

	slog.Info("Server is ready to handle requests", "port", serverPort)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Could not listen", "port", serverPort, "error", err)
		os.Exit(1)
	}

	<-done
	slog.Info("Server stopped")
}
