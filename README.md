# ⚡ APC UPS Prometheus Exporter

A lightweight [Prometheus](https://prometheus.io/) exporter written in [Go](https://go.dev/) that scrapes metrics from an APC UPS web interface and exposes them in a Prometheus-compatible format.  

---

## ✨ Features

- **Configurable**: Credentials and endpoints are provided via a YAML config file.
- **Session Management**: Automatically re-authenticates when sessions expire.
- **Graceful Shutdown**: Clean exit on `SIGINT` / `SIGTERM`.
- **Customizable**: Metrics use the Prometheus Collector pattern for easy extension.

---

## 📦 Prerequisites

- [Go](https://go.dev/dl/) **v1.18+**
- An **Apc PowerChute Serial Shutdown** web interface access.
- [Prometheus](https://prometheus.io/) for scraping metrics.

---

## ⚙️ Installation

Clone and build the exporter:

```bash
git clone https://github.com/veter2005/apc-exporter.git
cd apc-exporter
go build -o apc-exporter main.go
```

---

## 🛠 Configuration

The exporter reads configuration from a YAML file.  
By default, it looks at:

```
/etc/apc-exporter/config.yaml
```

You can override the path with the `-config` flag.

### Example `config.yaml`

```yaml
ups_url: "https://your-ups-hostname.com"
username: "your-admin-username"
password: "your-secret-password"
```

---

## 🚀 Usage

### Run with a custom config file
```bash
./apc-exporter -config=/path/to/my/config.yaml
```

### Run with the default config path
```bash
sudo cp config.yaml /etc/apc-exporter/config.yaml
./apc-exporter
```

The exporter starts an HTTP server on **port 8000** by default and exposes metrics at:

```
http://localhost:8000/metrics
```

---

## 📡 Prometheus Integration

Add the following to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'apc_ups'
    scrape_interval: 30s
    static_configs:
      - targets: ['localhost:8000']
```

---

## 📊 Exposed Metrics

All metrics are **Gauges**.  

| Metric Name                     | Description                                    |
|---------------------------------|------------------------------------------------|
| `ups_device_status_up`          | Device status (`1=Online`, `0=Other`)          |
| `ups_load_percent`              | Current UPS load (%)                           |
| `ups_runtime_remaining_minutes` | Estimated runtime remaining (minutes)          |
| `ups_internal_temperature_celsius` | Internal temperature (°C)                 |
| `ups_load_power_percent_va`     | Load power in % of VA capacity                 |
| `ups_load_current_amps`         | Load current (Amps)                            |
| `ups_input_voltage_vac`         | Input voltage (VAC)                            |
| `ups_output_voltage_vac`        | Output voltage (VAC)                           |
| `ups_input_frequency_hz`        | Input frequency (Hz)                           |
| `ups_output_frequency_hz`       | Output frequency (Hz)                          |
| `ups_battery_charge_percent`    | Battery charge (%)                             |
| `ups_battery_voltage_vdc`       | Battery voltage (VDC)                          |
| `ups_outlet_status`             | UPS outlet status (`1=On`, `0=Off`)            |

---

## 🛡️ Graceful Shutdown

The exporter handles `SIGINT` and `SIGTERM` for clean termination, ensuring HTTP and UPS sessions are properly closed.

---

## 📜 License

This project is licensed under the [GPL-3.0 license](LICENSE).

---

## 🤝 Contributing

Pull requests and feature suggestions are welcome!  
If you encounter issues, please open a GitHub issue with logs and details.

---

