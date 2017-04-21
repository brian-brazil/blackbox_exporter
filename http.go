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

package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/prometheus/common/log"
)

func matchRegularExpressions(reader io.Reader, config HTTPProbe) (bool, string) {
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		log.Errorf("Error reading HTTP body: %s", err)
		return false, "Error reading HTTP body"
	}
	for _, expression := range config.FailIfMatchesRegexp {
		re, err := regexp.Compile(expression)
		if err != nil {
			log.Errorf("Could not compile expression %q as regular expression: %s", expression, err)
			return false, "Could not compile regexp"
		}
		if re.Match(body) {
			return false, "Match failed"
		}
	}
	for _, expression := range config.FailIfNotMatchesRegexp {
		re, err := regexp.Compile(expression)
		if err != nil {
			log.Errorf("Could not compile expression %q as regular expression: %s", expression, err)
			return false, "Could not compile regexp"
		}
		if !re.Match(body) {
			return false, "Match failed"
		}
	}
	return true, ""
}

func probeHTTP(target string, w http.ResponseWriter, module Module) (success bool, probe_error string) {
	var isSSL, redirects int
	var dialProtocol, fallbackProtocol string

	config := module.HTTP

	if module.HTTP.Protocol == "" {
		module.HTTP.Protocol = "tcp"
	}

	if module.HTTP.Protocol == "tcp" && module.HTTP.PreferredIPProtocol == "" {
		module.HTTP.PreferredIPProtocol = "ip6"
	}
	if module.HTTP.PreferredIPProtocol == "ip6" {
		fallbackProtocol = "ip4"
	} else {
		fallbackProtocol = "ip6"
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}

	dialProtocol = module.HTTP.Protocol
	if module.HTTP.Protocol == "tcp" {
		targetURL, err := url.Parse(target)
		if err != nil {
			return false, "Could not parse target URL"
		}
		targetHost, _, err := net.SplitHostPort(targetURL.Host)
		// If split fails, assuming it's a hostname without port part
		if err != nil {
			targetHost = targetURL.Host
		}
		ip, err := net.ResolveIPAddr(module.HTTP.PreferredIPProtocol, targetHost)
		if err != nil {
			ip, err = net.ResolveIPAddr(fallbackProtocol, targetHost)
			if err != nil {
				return false, "Error resolving address"
			}
		}

		if ip.IP.To4() == nil {
			dialProtocol = "tcp6"
		} else {
			dialProtocol = "tcp4"
		}
	}

	if dialProtocol == "tcp6" {
		fmt.Fprintln(w, "probe_ip_protocol 6")
	} else {
		fmt.Fprintln(w, "probe_ip_protocol 4")
	}

	client := &http.Client{
		Timeout: module.Timeout,
	}

	tlsconfig, err := module.HTTP.TLSConfig.GenerateConfig()
	if err != nil {
		log.Errorf("Error generating TLS config: %s", err)
		return false, "Error generating TLS config"
	}
	dial := func(network, address string) (net.Conn, error) {
		return net.Dial(dialProtocol, address)
	}
	client.Transport = &http.Transport{
		TLSClientConfig:   tlsconfig,
		Dial:              dial,
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true,
	}

	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		redirects = len(via)
		if redirects > 10 || config.NoFollowRedirects {
			return errors.New("Don't follow redirects")
		}
		return nil
	}

	if config.Method == "" {
		config.Method = "GET"
	}

	request, err := http.NewRequest(config.Method, target, nil)
	if err != nil {
		log.Errorf("Error creating request for target %s: %s", target, err)
		return false, "Error creating request for target"
	}

	for key, value := range config.Headers {
		if strings.Title(key) == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(key, value)
	}

	// If a body is configured, add it to the request
	if config.Body != "" {
		request.Body = ioutil.NopCloser(strings.NewReader(config.Body))
	}

	resp, err := client.Do(request)
	// Err won't be nil if redirects were turned off. See https://github.com/golang/go/issues/3795
	if err != nil && resp == nil {
		log.Warnf("Error for HTTP request to %s: %s", target, err)
	} else {
		defer resp.Body.Close()
		if len(config.ValidStatusCodes) != 0 {
			for _, code := range config.ValidStatusCodes {
				if resp.StatusCode == code {
					success = true
					probe_error = ""
					break
				}
			}
		} else if 200 <= resp.StatusCode && resp.StatusCode < 300 {
			success = true
			probe_error = ""
		}

		if success && (len(config.FailIfMatchesRegexp) > 0 || len(config.FailIfNotMatchesRegexp) > 0) {
			success, probe_error = matchRegularExpressions(resp.Body, config)
		}
	}

	if resp == nil {
		resp = &http.Response{}
	}

	if resp.TLS != nil {
		isSSL = 1
		fmt.Fprintf(w, "probe_ssl_earliest_cert_expiry %f\n",
			float64(getEarliestCertExpiry(resp.TLS).UnixNano())/1e9)
		if config.FailIfSSL {
			success = false
			probe_error = "SSL failed"
		}
	} else if config.FailIfNotSSL {
		success = false
		probe_error = "SSL failed"
	}
	fmt.Fprintf(w, "probe_http_status_code %d\n", resp.StatusCode)
	fmt.Fprintf(w, "probe_http_content_length %d\n", resp.ContentLength)
	fmt.Fprintf(w, "probe_http_redirects %d\n", redirects)
	fmt.Fprintf(w, "probe_http_ssl %d\n", isSSL)
	return success, probe_error
}
