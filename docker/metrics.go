package docker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	APP = "prometheus_docker_sd"
)

var (
	metric_count = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_total",
		Help:      "Number of containers discovered"},
		[]string{})

	metric_ignored_containers_not_in_network = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_not_in_target_network_total",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but not in the target network"},
		[]string{"target_network"})

	metric_ignored_no_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_no_exposed_ports",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but with no exposed TCP ports"},
		[]string{"target_network"})

	metric_multiple_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_multiple_ports_not_explicit_total",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, with multiple exposed TCP ports, but the prometheus_scrape_port is not defined"},
		[]string{"target_network"})
)
