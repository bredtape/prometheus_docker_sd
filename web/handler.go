package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"sync"

	"github.com/bredtape/prometheus_docker_sd/docker"
)

//go:embed *.html
var templates embed.FS

type handler struct {
	rw      sync.Mutex
	updates <-chan []docker.Meta
	view    View
}

func StartHandler(updates <-chan []docker.Meta) *handler {
	h := &handler{updates: updates}
	go h.update()
	return h
}

func (h *handler) update() {
	for update := range h.updates {
		view := convert(update)
		h.rw.Lock()
		h.view = view
		h.rw.Unlock()
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.rw.Lock()
	defer h.rw.Unlock()

	t, err := template.ParseFS(templates, "template.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "failed to parse template: %v", err)
		return
	}

	err = t.Execute(w, h.view)
	if err != nil {
		slog.Error("failed to execute template: %v", err)
	}
}

type View struct {
	Total    int
	WithJob  int
	OKs      int
	Errors   int
	Warnings int
	Items    []Item
}

type Item struct {
	Name              string
	Address           string
	Labels            []string
	HasJob            bool
	IsExported        bool
	IsInTargetNetwork bool
	HasTCPPorts       bool // at least 1 TCP port
	HasExplicitPort   bool // explicit or single port
}

func convert(xs []docker.Meta) View {
	view := View{
		Total: len(xs),
		Items: make([]Item, 0, len(xs))}

	for _, x := range xs {
		if x.HasJob {
			view.WithJob++

			if x.IsExported() {
				if x.HasExplicitPort {
					view.OKs++
				} else {
					view.Warnings++
				}
			} else {
				view.Errors++
			}
		}

		view.Items = append(view.Items,
			Item{
				Name:              x.Name,
				Address:           x.Address,
				Labels:            convertLabels(x.Labels),
				HasJob:            x.HasJob,
				IsExported:        x.IsExported(),
				IsInTargetNetwork: x.IsInTargetNetwork,
				HasTCPPorts:       x.HasTCPPorts,
				HasExplicitPort:   x.HasExplicitPort})
	}
	return view
}

func convertLabels(m map[string]string) []string {
	result := make([]string, 0, len(m))

	for k, v := range m {
		result = append(result, fmt.Sprintf(`%s="%s"`, k, v))
	}

	sort.Strings(result)
	return result
}
