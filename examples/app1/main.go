package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	adr := ":2000"
	if p := os.Getenv("PORT"); p != "" {
		adr = ":" + p
	}

	http.Handle("/metrics", promhttp.Handler())

	slog.Info("Start serving http", "address", adr)
	defer slog.Info("exiting")
	_ = http.ListenAndServe(adr, nil)
}
