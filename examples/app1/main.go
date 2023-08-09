package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	adr := ":2000"
	if p, _ := strconv.Atoi(os.Getenv("PORT")); p > 0 {
		adr = fmt.Sprintf(":%d", p)
	}
	http.Handle("/metrics", promhttp.Handler())

	slog.Info("Start serving http", "address", adr)
	_ = http.ListenAndServe(adr, nil)
}
