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
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/prometheus/common/log"
)

var (
	icmpSequence      uint16
	icmpSequenceMutex sync.Mutex
)

func getICMPSequence() uint16 {
	icmpSequenceMutex.Lock()
	defer icmpSequenceMutex.Unlock()
	icmpSequence++
	return icmpSequence
}

func probeICMP(target string, w http.ResponseWriter, module Module) (success bool, probe_error string) {
	var (
		socket           *icmp.PacketConn
		requestType      icmp.Type
		replyType        icmp.Type
		fallbackProtocol string
	)

	deadline := time.Now().Add(module.Timeout)

	// Defaults to IPv4 to be compatible with older versions
	if module.ICMP.Protocol == "" {
		module.ICMP.Protocol = "icmp"
	}

	// In case of ICMP prefer IPv6 by default
	if module.ICMP.Protocol == "icmp" && module.ICMP.PreferredIPProtocol == "" {
		module.ICMP.PreferredIPProtocol = "ip6"
	}

	if module.ICMP.Protocol == "icmp4" {
		module.ICMP.PreferredIPProtocol = "ip4"
		fallbackProtocol = ""
	} else if module.ICMP.Protocol == "icmp6" {
		module.ICMP.PreferredIPProtocol = "ip6"
		fallbackProtocol = ""
	} else if module.ICMP.PreferredIPProtocol == "ip6" {
		fallbackProtocol = "ip4"
	} else {
		fallbackProtocol = "ip6"
	}

	resolveStart := time.Now()
	ip, err := net.ResolveIPAddr(module.ICMP.PreferredIPProtocol, target)
	if err != nil && fallbackProtocol != "" {
		ip, err = net.ResolveIPAddr(fallbackProtocol, target)
	}
	fmt.Fprintf(w, "probe_dns_lookup_time_seconds %f\n", time.Since(resolveStart).Seconds())

	if err != nil {
		log.Warnf("Error resolving address %s: %s", target, err)
		return false, "Error resolving address"
	}

	if ip.IP.To4() == nil {
		requestType = ipv6.ICMPTypeEchoRequest
		replyType = ipv6.ICMPTypeEchoReply
		socket, err = icmp.ListenPacket("ip6:ipv6-icmp", "::")
		fmt.Fprintln(w, "probe_ip_protocol 6")
	} else {
		requestType = ipv4.ICMPTypeEcho
		replyType = ipv4.ICMPTypeEchoReply
		socket, err = icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		fmt.Fprintln(w, "probe_ip_protocol 4")
	}

	if err != nil {
		log.Errorf("Error listening to socket for %s: %s", target, err)
		return false, "Error listening to socket"
	}
	defer socket.Close()

	wm := icmp.Message{
		Type: requestType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  int(getICMPSequence()),
			Data: []byte("Prometheus Blackbox Exporter"),
		},
	}

	wb, err := wm.Marshal(nil)
	if err != nil {
		log.Errorf("Error marshalling packet for %s: %s", target, err)
		return false, "Error marshalling packet"
	}
	if _, err := socket.WriteTo(wb, ip); err != nil {
		log.Warnf("Error writing to socket for %s: %s", target, err)
		return false, "Error writing to socket"
	}

	// Reply should be the same except for the message type.
	wm.Type = replyType
	wb, err = wm.Marshal(nil)
	if err != nil {
		log.Errorf("Error marshalling packet for %s: %s", target, err)
		return false, "Error marshalling packet"
	}

	rb := make([]byte, 1500)
	if err := socket.SetReadDeadline(deadline); err != nil {
		log.Errorf("Error setting socket deadline for %s: %s", target, err)
		return false, "Error setting socket deadline"
	}
	for {
		n, peer, err := socket.ReadFrom(rb)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Warnf("Timeout reading from socket for %s: %s", target, err)
				return false, "Timeout reading from socket"
			}
			log.Errorf("Error reading from socket for %s: %s", target, err)
			continue
		}
		if peer.String() != ip.String() {
			continue
		}
		if replyType == ipv6.ICMPTypeEchoReply {
			// Clear checksum to make comparison succeed.
			rb[2] = 0
			rb[3] = 0
		}
		if bytes.Compare(rb[:n], wb) == 0 {
			return true, ""
		}
	}
}
