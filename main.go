package main

import (
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"flag" // Import the flag package

	"github.com/PuerkitoBio/goquery"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/publicsuffix"
	"gopkg.in/yaml.v3"
)

// Config holds the values read from the configuration file.
type Config struct {
	UPSURL   string `yaml:"ups_url"`
	USERNAME string `yaml:"username"`
	PASSWORD string `yaml:"password"`
}

var config Config

// Define your application constants.
const (
	LOGINURL     = "/j_security_check"
	LOGONPAGEURL = "/logon"
	STATUSURL    = "/status"
	LISTENPORT   = ":8000"
)

// upsCollector implements the prometheus.Collector interface and holds client state.
type upsCollector struct {
	mu                       sync.Mutex
	httpClient               *http.Client
	isLoggedIn               bool

	deviceStatusDesc         *prometheus.Desc
	loadPercentDesc          *prometheus.Desc
	runtimeRemainingDesc     *prometheus.Desc
	internalTempDesc         *prometheus.Desc
	loadPowerVADesc          *prometheus.Desc
	loadCurrentADesc         *prometheus.Desc
	inputVoltageVACDesc      *prometheus.Desc
	outputVoltageVACDesc     *prometheus.Desc
	inputFrequencyHZDesc     *prometheus.Desc
	outputFrequencyHZDesc    *prometheus.Desc
	batteryChargePercentDesc *prometheus.Desc
	batteryVoltageVDCDesc    *prometheus.Desc
	outletStatusDesc         *prometheus.Desc
}

// newUPSCollector returns a new instance of upsCollector with an initialized HTTP client.
func newUPSCollector(client *http.Client) *upsCollector {
	return &upsCollector{
		httpClient: client,
		isLoggedIn: false,

		deviceStatusDesc:         prometheus.NewDesc("ups_device_status_up", "Device status (1=Online, 0=Other).", nil, nil),
		loadPercentDesc:          prometheus.NewDesc("ups_load_percent", "Current UPS load in percent.", nil, nil),
		runtimeRemainingDesc:     prometheus.NewDesc("ups_runtime_remaining_minutes", "Estimated runtime remaining in minutes.", nil, nil),
		internalTempDesc:         prometheus.NewDesc("ups_internal_temperature_celsius", "Internal temperature in Celsius.", nil, nil),
		loadPowerVADesc:          prometheus.NewDesc("ups_load_power_percent_va", "Load power in VA percent.", nil, nil),
		loadCurrentADesc:         prometheus.NewDesc("ups_load_current_amps", "Load current in Amps.", nil, nil),
		inputVoltageVACDesc:      prometheus.NewDesc("ups_input_voltage_vac", "Input voltage in VAC.", nil, nil),
		outputVoltageVACDesc:     prometheus.NewDesc("ups_output_voltage_vac", "Output voltage in VAC.", nil, nil),
		inputFrequencyHZDesc:     prometheus.NewDesc("ups_input_frequency_hz", "Input frequency in Hz.", nil, nil),
		outputFrequencyHZDesc:    prometheus.NewDesc("ups_output_frequency_hz", "Output frequency in Hz.", nil, nil),
		batteryChargePercentDesc: prometheus.NewDesc("ups_battery_charge_percent", "Battery charge in percent.", nil, nil),
		batteryVoltageVDCDesc:    prometheus.NewDesc("ups_battery_voltage_vdc", "Battery voltage in VDC.", nil, nil),
		outletStatusDesc:         prometheus.NewDesc("ups_outlet_status", "UPS outlet status (1=On, 0=Off).", nil, nil),
	}
}

// Describe sends the descriptors of all metrics to the provided channel.
func (c *upsCollector) Describe(ch chan<- *prometheus.Desc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch <- c.deviceStatusDesc
	ch <- c.loadPercentDesc
	ch <- c.runtimeRemainingDesc
	ch <- c.internalTempDesc
	ch <- c.loadPowerVADesc
	ch <- c.loadCurrentADesc
	ch <- c.inputVoltageVACDesc
	ch <- c.outputVoltageVACDesc
	ch <- c.inputFrequencyHZDesc
	ch <- c.outputFrequencyHZDesc
	ch <- c.batteryChargePercentDesc
	ch <- c.batteryVoltageVDCDesc
	ch <- c.outletStatusDesc
}

// relogin handles the full login sequence to re-establish a session.
func (c *upsCollector) relogin() error {
	logonPageURL := config.UPSURL + LOGONPAGEURL
	loginURL := config.UPSURL + LOGINURL
	
	// Step 1: GET the login page to retrieve the form tokens
	res, err := c.httpClient.Get(logonPageURL)
	if err != nil {
		c.isLoggedIn = false
		return err
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		c.isLoggedIn = false
		return err
	}

	formToken, _ := doc.Find("input[name=\"formtoken\"]").Attr("value")
	formTokenID, _ := doc.Find("input[name=\"formtokenid\"]").Attr("value")

	// Step 2: POST to the login URL with credentials and form tokens.
	formData := strings.NewReader("j_username=" + config.USERNAME + "&j_password=" + config.PASSWORD + "&login=Log On" + "&formtoken=" + formToken + "&formtokenid=" + formTokenID)
	
	// The client will follow the redirect.
	res, err = c.httpClient.Post(loginURL, "application/x-www-form-urlencoded", formData)
	if err != nil {
		c.isLoggedIn = false
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		c.isLoggedIn = false
		return http.ErrUseLastResponse
	}

	c.isLoggedIn = true
	log.Printf("Re-login successful.")
	return nil
}

// Collect reads the data and sends the collected metrics to the provided channel.
func (c *upsCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	statusURL := config.UPSURL + STATUSURL

	// Scrape with a maximum of 2 attempts (initial + relogin)
	for i := 0; i < 2; i++ {
		if !c.isLoggedIn {
			if err := c.relogin(); err != nil {
				log.Printf("Re-login failed: %v", err)
				c.sendZeroMetrics(ch)
				return
			}
		}

		res, err := c.httpClient.Get(statusURL)
		if err != nil {
			log.Printf("Scrape attempt %d failed: %v", i+1, err)
			c.isLoggedIn = false // Force re-login on next attempt
			continue
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("Scrape attempt %d failed with status code: %d", i+1, res.StatusCode)
			c.isLoggedIn = false // Force re-login on next attempt
			continue
		}

		// Scrape successful, process the HTML
		doc, err := goquery.NewDocumentFromReader(res.Body)
		if err != nil {
			log.Printf("Error parsing status page: %v", err)
			c.sendZeroMetrics(ch)
			return
		}

		// Extract data and update metrics
		c.collectMetric(ch, c.deviceStatusDesc, doc, "#value_DeviceStatus", "", 1.0, 0.0)
		c.collectMetric(ch, c.loadPercentDesc, doc, "#value_RealPowerPct", "", 0.0, 0.0)
		c.collectMetric(ch, c.runtimeRemainingDesc, doc, "#value_RuntimeRemaining", "", 0.0, 0.0)
		c.collectMetric(ch, c.internalTempDesc, doc, "#value_InternalTemp", "°C", 0.0, 0.0)
		c.collectMetric(ch, c.loadPowerVADesc, doc, "#value_ApparentPowerPct", "", 0.0, 0.0)
		c.collectMetric(ch, c.loadCurrentADesc, doc, "#value_LoadCurrent", "", 0.0, 0.0)
		c.collectMetric(ch, c.inputVoltageVACDesc, doc, "#value_InputVoltage", "", 0.0, 0.0)
		c.collectMetric(ch, c.outputVoltageVACDesc, doc, "#value_OutputVoltage", "", 0.0, 0.0)
		c.collectMetric(ch, c.inputFrequencyHZDesc, doc, "#value_InputFrequency", "", 0.0, 0.0)
		c.collectMetric(ch, c.outputFrequencyHZDesc, doc, "#value_OutputFrequency", "", 0.0, 0.0)
		c.collectMetric(ch, c.batteryChargePercentDesc, doc, "#value_BatteryCharge", "", 0.0, 0.0)
		c.collectMetric(ch, c.batteryVoltageVDCDesc, doc, "#value_VoltageDC", "", 0.0, 0.0)
		c.collectMetric(ch, c.outletStatusDesc, doc, "#status0", "On", 1.0, 0.0)

		log.Printf("Scrape successful at %s", time.Now().Format(time.RFC850))
		return
	}

	// All attempts failed, so send zero values
	log.Printf("All scrape attempts failed. Sending zero values.")
	c.sendZeroMetrics(ch)
}

// Helper function to safely extract and set metric values.
func (c *upsCollector) collectMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, doc *goquery.Document, selector string, strip string, trueVal, falseVal float64) {
	s := doc.Find(selector)
	if s.Length() > 0 {
		text := strings.TrimSpace(s.Text())
		
		// For the internal temperature, we need to handle the more complex string format.
		if selector == "#value_InternalTemp" {
			parts := strings.Split(text, "/")
			if len(parts) > 0 {
				text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[0]), "°C"))
			} else {
				text = "0"
			}
		} else if strip != "" {
			text = strings.TrimSuffix(text, strip)
			text = strings.TrimSpace(text)
		}

		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, val)
		} else {
			// Handle non-numeric text values like "On" or "On Line"
			if strings.Contains(s.Text(), "On Line") || strings.Contains(s.Text(), "On") {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, trueVal)
			} else {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, falseVal)
			}
		}
	} else {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, falseVal)
	}
}

// sendZeroMetrics sends 0 for all metrics on failure.
func (c *upsCollector) sendZeroMetrics(ch chan<- prometheus.Metric) {
	metrics := []*prometheus.Desc{
		c.deviceStatusDesc, c.loadPercentDesc, c.runtimeRemainingDesc, c.internalTempDesc,
		c.loadPowerVADesc, c.loadCurrentADesc, c.inputVoltageVACDesc,
		c.outputVoltageVACDesc, c.inputFrequencyHZDesc, c.outputFrequencyHZDesc,
		c.batteryChargePercentDesc, c.batteryVoltageVDCDesc, c.outletStatusDesc,
	}
	for _, desc := range metrics {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 0)
	}
}

func main() {
	// Define the default config path and a flag to override it.
	defaultConfigPath := "/etc/apc-exporter/config.yaml"
	configPath := flag.String("config", "", "Path to the configuration file")
	flag.Parse()

	// Determine which config path to use.
	var finalConfigPath string
	if *configPath != "" {
		finalConfigPath = *configPath
	} else {
		finalConfigPath = defaultConfigPath
	}

	// Read configuration from file
	configFile, err := os.Open(finalConfigPath)
	if err != nil {
		log.Fatalf("Failed to open config file at %s: %v", finalConfigPath, err)
	}
	defer configFile.Close()

	if err := yaml.NewDecoder(configFile).Decode(&config); err != nil {
		log.Fatalf("Failed to decode config file: %v", err)
	}

	// Create the cookie jar and HTTP client once for the application's lifecycle.
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		log.Fatalf("Error creating cookie jar: %v", err)
	}
	httpClient := &http.Client{Jar: jar}

	// Create and register the custom collector, passing the shared HTTP client.
	collector := newUPSCollector(httpClient)
	prometheus.MustRegister(collector)

	log.Printf("Starting Prometheus exporter on port %s...", LISTENPORT)
	
	// Create a channel to listen for OS signals.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the HTTP server in a separate goroutine.
	go func() {
		if err := http.ListenAndServe(LISTENPORT, promhttp.Handler()); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not start server: %v", err)
		}
	}()

	// Wait for an OS signal to terminate the program.
	<-sigChan
	log.Println("Shutting down gracefully...")

	// Close the idle connections to ensure resources are released.
	httpClient.CloseIdleConnections()

	log.Println("Server gracefully stopped.")
}
