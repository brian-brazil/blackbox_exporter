// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prober

import (
	"context"
	"net"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus/blackbox_exporter/config"
)

// validRRs checks a slice of RRs received from the server against a DNSRRValidator.
func validRRs(rrs *[]dns.RR, v *config.DNSRRValidator, logger log.Logger) bool {
	// Fail the probe if there are no RRs of a given type, but a regexp match is required
	// (i.e. FailIfNotMatchesRegexp is set).
	if len(*rrs) == 0 && len(v.FailIfNotMatchesRegexp) > 0 {
		return false
	}
	for _, rr := range *rrs {
		level.Debug(logger).Log("msg", "Validating RR: %q", rr)
		for _, re := range v.FailIfMatchesRegexp {
			match, err := regexp.MatchString(re, rr.String())
			if err != nil {
				level.Error(logger).Log("msg", "Error matching regexp %q: %s", re, err)
				return false
			}
			if match {
				return false
			}
		}
		for _, re := range v.FailIfNotMatchesRegexp {
			match, err := regexp.MatchString(re, rr.String())
			if err != nil {
				level.Error(logger).Log("msg", "Error matching regexp %q: %s", re, err)
				return false
			}
			if !match {
				return false
			}
		}
	}
	return true
}

// validRcode checks rcode in the response against a list of valid rcodes.
func validRcode(rcode int, valid []string, logger log.Logger) bool {
	var validRcodes []int
	// If no list of valid rcodes is specified, only NOERROR is considered valid.
	if valid == nil {
		validRcodes = append(validRcodes, dns.StringToRcode["NOERROR"])
	} else {
		for _, rcode := range valid {
			rc, ok := dns.StringToRcode[rcode]
			if !ok {
				level.Error(logger).Log("msg", "Invalid rcode", "rcode", rcode, "known_rcode", dns.RcodeToString)
				return false
			}
			validRcodes = append(validRcodes, rc)
		}
	}
	for _, rc := range validRcodes {
		if rcode == rc {
			return true
		}
	}
	level.Error(logger).Log("msg", "Rcode is not one of the valid rcodes", "string_rcode", dns.RcodeToString[rcode], "rcode", rcode, "valid_rcodes", validRcodes)
	return false
}

func ProbeDNS(ctx context.Context, target string, module config.Module, registry *prometheus.Registry, logger log.Logger) bool {
	var numAnswer, numAuthority, numAdditional int
	var dialProtocol string
	probeDNSAnswerRRSGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_dns_answer_rrs",
		Help: "Returns number of entries in the answer resource record list",
	})
	probeDNSAuthorityRRSGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_dns_authority_rrs",
		Help: "Returns number of entries in the authority resource record list",
	})
	probeDNSAdditionalRRSGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_dns_additional_rrs",
		Help: "Returns number of entries in the additional resource record list",
	})
	registry.MustRegister(probeDNSAnswerRRSGauge)
	registry.MustRegister(probeDNSAuthorityRRSGauge)
	registry.MustRegister(probeDNSAdditionalRRSGauge)

	defer func() {
		// These metrics can be used to build additional alerting based on the number of replies.
		// They should be returned even in case of errors.
		probeDNSAnswerRRSGauge.Set(float64(numAnswer))
		probeDNSAuthorityRRSGauge.Set(float64(numAuthority))
		probeDNSAdditionalRRSGauge.Set(float64(numAdditional))
	}()

	var ip *net.IPAddr
	if module.DNS.TransportProtocol == "" {
		module.DNS.TransportProtocol = "udp"
	}
	if module.DNS.TransportProtocol == "udp" || module.DNS.TransportProtocol == "tcp" {
		targetAddr, port, err := net.SplitHostPort(target)
		if err != nil {
			// Target only contains host so fallback to default port and set targetAddr as target.
			port = "53"
			targetAddr = target
		}
		ip, err = chooseProtocol(module.DNS.PreferredIPProtocol, targetAddr, registry)
		if err != nil {
			level.Error(logger).Log("msg", "Error resolving address", "err", err)
			return false
		}
		target = net.JoinHostPort(ip.String(), port)
	} else {
		level.Error(logger).Log("msg", "Configuration error: Expected transport protocol udp or tcp", "protocol", module.DNS.TransportProtocol)
		return false
	}

	if ip.IP.To4() == nil {
		dialProtocol = module.DNS.TransportProtocol + "6"
	} else {
		dialProtocol = module.DNS.TransportProtocol + "4"
	}

	client := new(dns.Client)
	client.Net = dialProtocol
	qt := dns.TypeANY
	if module.DNS.QueryType != "" {
		var ok bool
		qt, ok = dns.StringToType[module.DNS.QueryType]
		if !ok {
			level.Error(logger).Log("msg", "Invalid query type", "Type seen", module.DNS.QueryType, "Existing types", dns.TypeToString)
			return false
		}
	}
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(module.DNS.QueryName), qt)

	timeoutDeadline, _ := ctx.Deadline()
	client.Timeout = timeoutDeadline.Sub(time.Now())
	response, _, err := client.Exchange(msg, target)
	if err != nil {
		level.Warn(logger).Log("msg", "Error while sending a DNS query", "err", err)
		return false
	}
	level.Debug(logger).Log("msg", "Got response: %#v", response)

	numAnswer, numAuthority, numAdditional = len(response.Answer), len(response.Ns), len(response.Extra)

	if !validRcode(response.Rcode, module.DNS.ValidRcodes, logger) {
		return false
	}
	if !validRRs(&response.Answer, &module.DNS.ValidateAnswer, logger) {
		level.Debug(logger).Log("msg", "Answer RRs validation failed")
		return false
	}
	if !validRRs(&response.Ns, &module.DNS.ValidateAuthority, logger) {
		level.Debug(logger).Log("msg", "Authority RRs validation failed")
		return false
	}
	if !validRRs(&response.Extra, &module.DNS.ValidateAdditional, logger) {
		level.Debug(logger).Log("msg", "Additional RRs validation failed")
		return false
	}
	return true
}
