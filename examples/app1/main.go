package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

func main() {
	adr := ":2000"
	if p, _ := strconv.Atoi(os.Getenv("PORT")); p > 0 {
		adr = fmt.Sprintf(":%d", p)
	}
	http.Handle("/metrics", promhttp.Handler())

	logrus.Infof("Serving at %s", adr)
	_ = http.ListenAndServe(adr, nil)
}
