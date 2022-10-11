## Prometheus Docker Discovery for single host (no swarm)

# Vanilla

Prometheus have built-in discovery of Docker containers with docker_sd_configs, see https://prometheus.io/docs/prometheus/latest/configuration/configuration/#docker_sd_config.

See examples/vanilla for a working example.

# This flavour

The trouble with docker_sd_configs is when a container have multiple exposed ports, there will be multiple targets present for the same container. This application solves that scenario.

See examples/discovery for a working example.

2 arguments must be specified:

- target-network-name: The Network that the containers must be member of to be considered. Defaults to metrics-net.
- instance-prefix: Prefix added to the output labels as \<instance-prefix\>/\<container-name\>:\<port\>.

To be scraped a container must be in the configured 'target-network' (command line arg) and have a label 'prometheus_job'.

| Container label            | Description                                                                                 |
| -------------------------- | ------------------------------------------------------------------------------------------- |
| prometheus_job             | Add target. Set job name to value                                                           |
| prometheus\_\<key\>        | Add \<key\>' to output labels                                                               |
| prometheus_scrape_port     | Overrules address scrape port. Should be set when the container have multiple exposed ports |
| prometheus_scrape_interval | Set the scrape interval                                                                     |
| prometheus_scrape_timeout  | Set the scrape timeout                                                                      |
| prometheus_scrape_path     | Override metrics path                                                                       |
| prometheus_scrape_scheme   | Override scheme                                                                             |

Metrics should indicate if a container has the 'prometheus_job' label set, but not included in targets.
