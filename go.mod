module github.com/prometheus/blackbox_exporter

require (
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/go-kit/kit v0.10.0
	github.com/miekg/dns v1.1.40
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.18.0
	github.com/prometheus/exporter-toolkit v0.5.1
	golang.org/x/net v0.0.0-20210226172049-e18ecbb05110
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
)

// Source address support for http probes
replace github.com/prometheus/common => github.com/bobrik/common v0.19.1-0.20210317014803-5df092af4354

go 1.13
