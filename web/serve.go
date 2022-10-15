package web

import (
	"net/http"

	"github.com/bredtape/prometheus_docker_sd/docker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Serve(addr string, metas <-chan []docker.Meta) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/containers", StartHandler(metas))
	http.ListenAndServe(addr, mux)
}
