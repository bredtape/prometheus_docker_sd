## Prometheus Docker Discovery for single host (no swarm)

# Vanilla

Prometheus have built-in discovery of Docker containers with docker_sd_configs, see https://prometheus.io/docs/prometheus/latest/configuration/configuration/#docker_sd_config.

See examples/vanilla for a working example.

# This flavour

The trouble with docker_sd_configs is when a container have multiple exposed ports, there will be multiple targets present for the same container. This application solves that scenario.

See examples/discovery for a working example.

For a container to be exported:

- add container label 'prometheus_job' with the job name you want as value.
- the container must be in the configured target network (defaults to 'metrics-net').
- the container must have at least 1 expose port. Public ports are ignored. This may already be set in the Docker image.

| Container label            | Description                                                                                                                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| prometheus_job             | Add target. Set job name to value. Containers without this label will be ignored.                                                                                                     |
| prometheus\_\<key\>        | Add \<key\>' to output labels                                                                                                                                                         |
| prometheus_scrape_port     | Overrules address scrape port. Should be set when the container have multiple exposed ports, or external                                                                              |
| prometheus_scrape_interval | Override the scrape interval. Optional.                                                                                                                                               |
| prometheus_scrape_timeout  | Override the scrape timeout. Optional.                                                                                                                                                |
| prometheus_scrape_path     | Override metrics path. Optional.                                                                                                                                                      |
| prometheus_scrape_scheme   | Override scheme. Optional.                                                                                                                                                            |
| prometheus_scrape_external | Scrape external host:post, instead of the internal network. True/false. Optional. This is useful for https targets where the certicate matches the external url, but not the internal |

Metrics should indicate if a container has the 'prometheus_job' label set, but not included in targets.
