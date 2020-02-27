## Prometheus examples
A basic Prometheus setup is described in [prometheus-quickstart.md](prometheus-quickstart.md).  This document provides examples for how a permanent Prometheus setup can be deployed.  It is assumed you have some familiarity with Linux and Systemd.  As these are all examples, they obviously need not be followed verbatim.  Samples are provided as-is with minimum necessary descriptions. You should refer to [official documentation](https://prometheus.io/docs/prometheus/latest/configuration/configuration/) to fully understand them. Please find the following samples in the `docs/samples` directory:
 - alerting.rules.yml
 - alertmanager.service
 - alertmanager.yml
 - blackbox_exporter.service
 - blackbox.yml
 - prometheus.service
 - prometheus.yml

### Voting node health
The `alerting.rules.yml` example can proactively detect potentially transient failures:
 - The node appears online, but `dcrd` has stalled (BlockHeightStalled)
 - Block height is correct but `dcrwallet` doesn't agree with the other voting nodes about the state of the ticket pool (LiveTicketParity)

On my testnet vsp, which uses extremely cheap virtual servers, the first alert frequently catches problems early.  The second scenario happens often, but resolves itself within a few minutes. So that alert is configured to wait for a much longer duration before firing.  Definitely consider tuning how long an alert waits (eg. start with `for: 5m` and increase until false positives stop) before it fires.  When the duration is tuned appropriately, these alerts can inform you of a problem before `dcrstakepool` itself collapses.  At a minimum, when you find your `dcrstakepool` instance offline, you can pull up the Prometheus `/alerts` page and drill down to see which specific nodes aren't healthy.
 
### Systemd service units
These [service units](https://www.freedesktop.org/wiki/Software/systemd/) assume you've set up Prometheus under various `/opt` subdirectories, with binaries stored in `/opt/bin`.  They use an unprivileged user account to run the daemon. For your convenience, `ExecReload` has been configured so that you can reload configuration on the fly, e.g. 	`systemctl reload prometheus`.  Assuming you went through the quickstart already, here is how you could set up the `prometheus` daemon :
```bash
 sudo mkdir -p /opt/bin
 sudo mkdir -p /opt/prometheus/data
 sudo cp -r prometheus-2.*.linux-amd64/{consoles,console_libraries,*.yml} /opt/prometheus/
 sudo cp prometheus-2.*.linux-amd64/{prometheus,promtool} /opt/bin/
 sudo useradd -Us /usr/bin/nologin -Md /opt/prometheus -c "Prometheus daemon" prometheus
 sudo chown prometheus:prometheus /opt/prometheus/data
 sudo cp prometheus.service /etc/systemd/system
 sudo systemctl daemon-reload
 sudo systemctl enable prometheus
 sudo systemctl start prometheus
```

### Mobile push notifications
The provided `alertmanager.yml` example sends alerts to the api of a service called [pushover.net](https://pushover.net/). For one time fee, you can use their api to send push notifications directly to your Android or iOS mobile device.  Alertmanager supports numerous notification methods, including a generic webhook api, PagerDuty (a significantly more expensive service), OpsGenie (free for up to 5 users), and more.

### Blackbox Exporter
[Blackbox exporter](https://github.com/prometheus/blackbox_exporter/) is a Prometheus daemon you can deploy to monitor http endpoints like `dcrstakepool` itself. See the relevant job named "health-checks" in `prometheus.yml`.  The service unit assumes configuration is in a directory `/opt/prometheus` owned by the `prometheus` user like in the example above.

### Observability with Grafana
Once you've set up alerts, you might find it valuable to visualize your metrics.  [Grafana](https://grafana.com/docs/) supports Prometheus as a datasource out of the box, and can be set up by simply adding a third party repository.

### Node Exporter
To monitor basic system statistics like CPU usage, RAM, disk IO, etc, there is [Node Exporter](https://github.com/prometheus/node_exporter). It's another daemon you can run on your nodes to export system-level metrics.  There's an [example dashboard](https://grafana.com/grafana/dashboards/1860) that visualizes all of the numerous collected metrics.

