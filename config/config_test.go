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

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

func yamlToJson(t *testing.T, path string) error {
	t.Helper()
	data := make(map[string]any)
	fileReader, err := os.Open(fmt.Sprintf("%s.yml", path))
	if err != nil {
		return fmt.Errorf("error reading config file: %s", err)
	}
	defer fileReader.Close()

	decoder := yaml.NewDecoder(fileReader)
	if err := decoder.Decode(&data); err != nil {
		return err
	}

	jsonData, err := json.Marshal(&data)
	if err != nil {
		return err
	}

	file, err := os.Create(fmt.Sprintf("%s.json", path))
	if err != nil {
		return err
	}
	defer file.Close()
	t.Cleanup(func() { os.Remove(fmt.Sprintf("%s.json", path)) })

	if _, err = file.Write(jsonData); err != nil {
		return err
	}
	return nil
}

func TestLoadConfig(t *testing.T) {
	diff := make([]*SafeConfig, 2)
	path := "testdata/blackbox-good"
	require.NoError(t, yamlToJson(t, path))

	for idx, format := range []string{"yml", "json"} {
		file := fmt.Sprintf("%s.%s", path, format)
		t.Run(file, func(t *testing.T) {
			sc := NewSafeConfig(prometheus.NewRegistry())

			err := sc.ReloadConfig(file, nil)
			if err != nil {
				t.Errorf("Error loading config %v: %v", file, err)
			}
			diff[idx] = sc
		})
	}
	require.EqualExportedValues(t, diff[0], diff[1])
}

// Testing the Marshal and Unmarshal functions of the Regexp type
func TestRegexpMarshal(t *testing.T) {
	var regexp struct {
		Test Regexp `yaml:"test" json:"test"`
	}
	t.Run("JSON", func(t *testing.T) {
		data := []byte(`{"test":"(\\w+.+)"}`)
		err := json.Unmarshal(data, &regexp)
		require.NoError(t, err)
		marshaled, err := json.Marshal(&regexp)
		require.NoError(t, err)
		require.Equal(t, string(data), string(marshaled))

		_, err = json.Marshal(Regexp{})
		require.NoError(t, err)
	})

	t.Run("YAML", func(t *testing.T) {
		data := []byte("test: (\\w+.+)\n")
		err := yaml.Unmarshal(data, &regexp)
		require.NoError(t, err)
		marshaled, err := yaml.Marshal(&regexp)
		require.NoError(t, err)
		require.Equal(t, string(data), string(marshaled))

		_, err = yaml.Marshal(Regexp{})
		require.NoError(t, err)
	})
}

// Testing the capability of Marsheling the config without errors
func TestConfigMarshal(t *testing.T) {
	path := "testdata/blackbox-good"
	require.NoError(t, yamlToJson(t, path))

	for _, format := range []string{"yml", "json"} {
		file := fmt.Sprintf("%s.%s", path, format)
		t.Run(file, func(t *testing.T) {
			sc := NewSafeConfig(prometheus.NewRegistry())
			err := sc.ReloadConfig(file, nil)
			if err != nil {
				t.Errorf("Error loading config %v: %v", file, err)
			}

			if strings.HasSuffix(file, ".json") {
				_, err := json.Marshal(sc.C)
				require.NoError(t, err)
			} else {
				_, err := yaml.Marshal(sc.C)
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadBadConfigs(t *testing.T) {
	sc := NewSafeConfig(prometheus.NewRegistry())
	tests := []struct {
		input  string
		want   string
		format []string
	}{
		{
			input:  "testdata/blackbox-bad",
			want:   "error parsing config file: yaml: unmarshal errors:\n  line 50: field invalid_extra_field not found in type config.plain",
			format: []string{"yml"},
		},
		{
			input:  "testdata/blackbox-bad",
			want:   "error parsing config file: json: unknown field \"invalid_extra_field\"",
			format: []string{"json"},
		},
		{
			input:  "testdata/blackbox-bad2",
			want:   "error parsing config file: at most one of bearer_token & bearer_token_file must be configured",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-dns-module",
			want:   "error parsing config file: query name must be set for DNS module",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-dns-class",
			want:   "error parsing config file: query class 'X' is not valid",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-dns-type",
			want:   "error parsing config file: query type 'X' is not valid",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-header-match",
			want:   "error parsing config file: regexp must be set for HTTP header matchers",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-body-match-regexp",
			want:   `error parsing config file: "Could not compile regular expression" regexp=":["`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-body-not-match-regexp",
			want:   `error parsing config file: "Could not compile regular expression" regexp=":["`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-header-match-regexp",
			want:   `error parsing config file: "Could not compile regular expression" regexp=":["`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-compression-mismatch",
			want:   `error parsing config file: invalid configuration "Accept-Encoding: deflate", "compression: gzip"`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-compression-mismatch-special-case",
			want:   `error parsing config file: invalid configuration "accEpt-enCoding: deflate", "compression: gzip"`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-request-compression-reject-all-encodings",
			want:   `error parsing config file: invalid configuration "Accept-Encoding: *;q=0.0", "compression: gzip"`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-icmp-ttl",
			want:   "error parsing config file: \"ttl\" cannot be negative",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-icmp-ttl-overflow",
			want:   "error parsing config file: \"ttl\" cannot exceed 255",
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-tcp-query-response-regexp",
			want:   `error parsing config file: "Could not compile regular expression" regexp=":["`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-http-body-config",
			want:   `error parsing config file: setting body and body_file both are not allowed`,
			format: []string{"yml", "json"},
		},
		{
			input:  "testdata/invalid-module-prober",
			want:   `error parsing config file: prober 'hTTp' is invalid`,
			format: []string{"yml", "json"},
		},
	}
	for _, test := range tests {
		require.NoError(t, yamlToJson(t, test.input))
		for _, format := range test.format {
			path := fmt.Sprintf("%s.%s", test.input, format)
			t.Run(path, func(t *testing.T) {
				got := sc.ReloadConfig(path, nil)
				if got == nil || got.Error() != test.want {
					t.Fatalf("ReloadConfig(%q) = %v; want %q", path, got, test.want)
				}
			})
		}
	}
}

func TestHideConfigSecrets(t *testing.T) {
	sc := NewSafeConfig(prometheus.NewRegistry())

	err := sc.ReloadConfig("testdata/blackbox-good.yml", nil)
	if err != nil {
		t.Errorf("Error loading config %v: %v", "testdata/blackbox-good.yml", err)
	}

	// String method must not reveal authentication credentials.
	sc.RLock()
	c, err := yaml.Marshal(sc.C)
	sc.RUnlock()
	if err != nil {
		t.Errorf("Error marshalling config: %v", err)
	}
	if strings.Contains(string(c), "mysecret") {
		t.Fatal("config's String method reveals authentication credentials.")
	}
}

func TestIsEncodingAcceptable(t *testing.T) {
	testcases := map[string]struct {
		input          string
		acceptEncoding string
		expected       bool
	}{
		"empty compression": {
			input:          "",
			acceptEncoding: "gzip",
			expected:       true,
		},
		"trivial": {
			input:          "gzip",
			acceptEncoding: "gzip",
			expected:       true,
		},
		"trivial, quality": {
			input:          "gzip",
			acceptEncoding: "gzip;q=1.0",
			expected:       true,
		},
		"first": {
			input:          "gzip",
			acceptEncoding: "gzip, compress",
			expected:       true,
		},
		"second": {
			input:          "gzip",
			acceptEncoding: "compress, gzip",
			expected:       true,
		},
		"missing": {
			input:          "br",
			acceptEncoding: "gzip, compress",
			expected:       false,
		},
		"*": {
			input:          "br",
			acceptEncoding: "gzip, compress, *",
			expected:       true,
		},
		"* with quality": {
			input:          "br",
			acceptEncoding: "gzip, compress, *;q=0.1",
			expected:       true,
		},
		"rejected": {
			input:          "br",
			acceptEncoding: "gzip, compress, br;q=0.0",
			expected:       false,
		},
		"rejected *": {
			input:          "br",
			acceptEncoding: "gzip, compress, *;q=0.0",
			expected:       false,
		},
		"complex": {
			input:          "br",
			acceptEncoding: "gzip;q=1.0, compress;q=0.5, br;q=0.1, *;q=0.0",
			expected:       true,
		},
		"complex out of order": {
			input:          "br",
			acceptEncoding: "*;q=0.0, compress;q=0.5, br;q=0.1, gzip;q=1.0",
			expected:       true,
		},
		"complex with extra blanks": {
			input:          "br",
			acceptEncoding: " gzip;q=1.0, compress; q=0.5, br;q=0.1, *; q=0.0 ",
			expected:       true,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actual := isCompressionAcceptEncodingValid(tc.input, tc.acceptEncoding)
			if actual != tc.expected {
				t.Errorf("Unexpected result: input=%q acceptEncoding=%q expected=%t actual=%t", tc.input, tc.acceptEncoding, tc.expected, actual)
			}
		})
	}
}
