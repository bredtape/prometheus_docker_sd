package web

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bredtape/prometheus_docker_sd/docker"
)

func Serve(addr string, metas <-chan []docker.Meta) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/containers", StartHandler(metas))
	mux.Handle("/static/", cacheForever(http.StripPrefix("/static", http.FileServer(http.Dir("./web/static")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/containers", http.StatusSeeOther)
	})
	http.ListenAndServe(addr, mux)
}

func cacheForever(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
