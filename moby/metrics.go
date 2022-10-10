package moby

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "prometheus_docker_sd"
)

var (
	metric_ignored_containers_not_in_network = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ignored_networks_count",
		Help:      "Number of ignored containers with 'prometheus_job' label, but are not in the target network"},
		[]string{"target_network"})
)
