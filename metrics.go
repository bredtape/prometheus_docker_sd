package main

import (
	"github.com/bredtape/prometheus_docker_sd/docker"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	APP = "prometheus_docker_sd"
)

var (
	labelKeys = []string{"external_url", "target_network"}

	metric_attempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: APP,
		Name:      "discovery_attempts_total",
		Help:      "Number of attempts to discover containers and write result"}, labelKeys)

	metric_errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: APP,
		Name:      "discovery_attempts_errors_total",
		Help:      "Number of attempts to discover containers and write result, that resulted in some error"}, labelKeys)

	metric_count = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_count",
		Help:      "Number of containers discovered"},
		labelKeys)

	metric_ignored = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_ignored_count",
		Help:      "Number of containers discovered that were ignored"},
		labelKeys)

	metric_ignored_containers_not_in_network = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_not_in_target_network_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but not in the target network"},
		labelKeys)

	metric_ignored_no_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_no_exposed_ports_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but with no exposed TCP ports"},
		labelKeys)

	metric_multiple_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_multiple_ports_not_explicit_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, with multiple exposed TCP ports, but the prometheus_scrape_port is not defined"},
		labelKeys)
)

func updateMetrics(externalUrl, targetNetwork string, xs []docker.Meta) {
	var ignored, notInNetwork, noPorts, notExplicit float64
	for _, x := range xs {
		if !x.HasJob {
			ignored++
			continue
		}

		if !x.IsInTargetNetwork {
			notInNetwork++
			continue
		}

		if !x.HasTCPPorts {
			noPorts++
			continue
		}

		if !x.HasExplicitPort {
			notExplicit++
		}
	}

	metric_count.WithLabelValues(externalUrl, targetNetwork).Set(float64(len(xs)))
	metric_ignored.WithLabelValues(externalUrl, targetNetwork).Set(ignored)
	metric_ignored_containers_not_in_network.WithLabelValues(externalUrl, targetNetwork).Set(notInNetwork)
	metric_ignored_no_ports.WithLabelValues(externalUrl, targetNetwork).Set(noPorts)
	metric_multiple_ports.WithLabelValues(externalUrl, targetNetwork).Set(notExplicit)
}
