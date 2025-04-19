package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
	"tailscale.com/util/dnsname"
)

var (
	authKey     = flag.String("authkey", os.Getenv("TS_AUTHKEY"), "Tailscale auth key")
	hostname    = flag.String("hostname", "tsmagicproxy", "Hostname for the tailnet node")
	stateDir    = flag.String("state-dir", "./tsmagicproxy-state", "Directory to store tailscale state")
	listen      = flag.String("listen", ":53", "Address to listen on for DNS requests")
	ttl         = flag.Int("ttl", 600, "TTL for DNS responses")
	domain      = flag.String("domain", "", "Domain suffix to append to hostnames (e.g., tailnet.ts.net)")
	forceLogin  = flag.Bool("force-login", false, "Force login even if state exists")
	debug       = flag.Bool("debug", false, "Enable verbose debug logging")
)

func main() {
	flag.Parse()

	if *authKey == "" {
		log.Fatal("auth key must be provided via -authkey flag or TS_AUTHKEY environment variable")
	}

	// Ensure state directory exists
	if err := os.MkdirAll(*stateDir, 0700); err != nil {
		log.Fatalf("Failed to create state directory: %v", err)
	}

	// Set force login env var if requested
	if *forceLogin {
		os.Setenv("TSNET_FORCE_LOGIN", "1")
	}

	// Create the tsnet server
	s := &tsnet.Server{
		Hostname: *hostname,
		AuthKey:  *authKey,
		Dir:      *stateDir,
	}
	defer s.Close()

	// Start the server to connect to the tailnet
	log.Printf("Connecting to tailnet with hostname %s...", *hostname)
	if err := s.Start(); err != nil {
		log.Fatalf("Error starting tsnet server: %v", err)
	}

	// Wait for the connection to be established
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	status, err := s.Up(ctx)
	if err != nil {
		log.Fatalf("Error connecting to tailnet: %v", err)
	}

	log.Printf("Connected to tailnet as %s with IP %v", status.Self.DNSName, status.TailscaleIPs)

	// If domain suffix is not specified, extract it from Self.DNSName
	if *domain == "" && status.Self.DNSName != "" {
		parts := strings.SplitN(status.Self.DNSName, ".", 2)
		if len(parts) > 1 {
			*domain = parts[1]
			log.Printf("Detected domain suffix: %s", *domain)
		}
	}

	// Log all available DNS names in the tailnet
	log.Printf("Available nodes in tailnet:")
	log.Printf("Self: %s with IPs %v", status.Self.DNSName, status.TailscaleIPs)
	for _, peer := range status.Peer {
		if peer.DNSName != "" {
			log.Printf("Peer: %s with IPs %v", peer.DNSName, peer.TailscaleIPs)
		}
	}

	// Create DNS server
	dnsServer := &DNSServer{
		tsnet:  s,
		status: status,
		domain: *domain,
		debug:  *debug,
	}

	// Start DNS server
	log.Printf("Starting DNS server on %s", *listen)
	dnsServer.Start(*listen)
}

// DNSServer implements a DNS server that proxies requests to Tailscale's MagicDNS
type DNSServer struct {
	tsnet  *tsnet.Server
	status *ipnstate.Status
	domain string
	debug  bool
}

// Start the DNS server on the specified address
func (s *DNSServer) Start(addr string) {
	dns.HandleFunc(".", s.handleDNSRequest)

	// Start server on UDP
	server := &dns.Server{Addr: addr, Net: "udp"}
	log.Fatal(server.ListenAndServe())
}

// handleDNSRequest processes incoming DNS requests
func (s *DNSServer) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.RecursionAvailable = false

	// Process each question
	for _, q := range r.Question {
		log.Printf("Query: %s %s", q.Name, dns.TypeToString[q.Qtype])

		switch q.Qtype {
		case dns.TypeA, dns.TypeAAAA:
			s.handleAddressQuery(q, m)
		case dns.TypePTR:
			s.handlePTRQuery(q, m)
		case dns.TypeTXT, dns.TypeCNAME, dns.TypeSRV:
			// For now we don't implement these record types
		}
	}

	// Log the response
	if s.debug {
		log.Printf("Response: %v", m)
	} else {
		log.Printf("Response has %d answers", len(m.Answer))
	}

	w.WriteMsg(m)
}

// handleAddressQuery handles A and AAAA queries
func (s *DNSServer) handleAddressQuery(q dns.Question, m *dns.Msg) {
	// Get the current status to have the latest peer information
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	lc, err := s.tsnet.LocalClient()
	if err != nil {
		log.Printf("Error getting local client: %v", err)
		return
	}
	
	status, err := lc.Status(ctx)
	if err != nil {
		log.Printf("Error getting status: %v", err)
		return
	}

	qname := dnsname.TrimSuffix(q.Name, ".")
	
	if s.debug {
		log.Printf("Looking up: %s", qname)
	}
	
	// Check for matches among peers
	for _, peer := range status.Peer {
		// Skip peers without names
		if peer.DNSName == "" {
			continue
		}
		
		peerName := dnsname.TrimSuffix(peer.DNSName, ".")
		
		if s.debug {
			log.Printf("Checking against peer: %s", peerName)
		}
		
		// Try exact match first
		if qname == peerName {
			log.Printf("Found exact match: %s = %s", qname, peerName)
			addPeerToAnswer(q, m, *peer, *ttl)
			return
		}
		
		// Try hostname without domain if the query includes the domain
		if s.domain != "" {
			// If we have test.tailnet.ts.net and query is just for 'test'
			peerBaseName := strings.SplitN(peerName, ".", 2)[0]
			if qname == peerBaseName {
				log.Printf("Found base match: %s = %s", qname, peerBaseName)
				addPeerToAnswer(q, m, *peer, *ttl)
				return
			}
		}
	}
	
	log.Printf("No match found for: %s", qname)
}

// addPeerToAnswer adds appropriate resource records for a peer to the DNS answer
func addPeerToAnswer(q dns.Question, m *dns.Msg, peer ipnstate.PeerStatus, ttl int) {
	log.Printf("Found match for %s: %v", q.Name, peer.TailscaleIPs)
	
	for _, addr := range peer.TailscaleIPs {
		// Only return the appropriate address type
		if (q.Qtype == dns.TypeA && addr.Is4()) || (q.Qtype == dns.TypeAAAA && addr.Is6()) {
			rr := createRR(q.Name, addr, ttl)
			if rr != nil {
				m.Answer = append(m.Answer, rr)
			}
		}
	}
}

// handlePTRQuery handles PTR queries (reverse lookups)
func (s *DNSServer) handlePTRQuery(q dns.Question, m *dns.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	lc, err := s.tsnet.LocalClient()
	if err != nil {
		log.Printf("Error getting local client: %v", err)
		return
	}
	
	status, err := lc.Status(ctx)
	if err != nil {
		log.Printf("Error getting status: %v", err)
		return
	}

	// Convert PTR query format (e.g., 1.2.3.4.in-addr.arpa) to IP address
	ip := extractIPFromReverseDNS(q.Name)
	if ip == (netip.Addr{}) {
		log.Printf("Invalid PTR query format: %s", q.Name)
		return
	}
	
	log.Printf("PTR lookup for IP: %s", ip)

	// Search peers for matching IP
	for _, peer := range status.Peer {
		if peer.DNSName == "" {
			continue
		}
		
		for _, peerAddr := range peer.TailscaleIPs {
			if peerAddr == ip {
				ptr := &dns.PTR{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypePTR,
						Class:  dns.ClassINET,
						Ttl:    uint32(*ttl),
					},
					Ptr: peer.DNSName + ".",
				}
				m.Answer = append(m.Answer, ptr)
				return
			}
		}
	}
}

// extractIPFromReverseDNS extracts an IP address from a reverse DNS query
// e.g., 1.2.3.4.in-addr.arpa -> 4.3.2.1 (IPv4)
// e.g., 1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa -> 2001:db8::1 (IPv6)
func extractIPFromReverseDNS(name string) netip.Addr {
	name = strings.ToLower(name)
	
	// Handle IPv4
	if strings.HasSuffix(name, ".in-addr.arpa.") {
		parts := strings.Split(strings.TrimSuffix(name, ".in-addr.arpa."), ".")
		if len(parts) != 4 {
			return netip.Addr{}
		}
		
		// Reverse the order (PTR is in reverse)
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		
		ip := strings.Join(parts, ".")
		if addr, err := netip.ParseAddr(ip); err == nil {
			return addr
		}
	}
	
	// Handle IPv6
	if strings.HasSuffix(name, ".ip6.arpa.") {
		parts := strings.Split(strings.TrimSuffix(name, ".ip6.arpa."), ".")
		if len(parts) != 32 {
			return netip.Addr{}
		}
		
		// Reverse and convert to IPv6 hex format
		var hexParts []string
		for i := 0; i < 32; i += 4 {
			if i+4 > len(parts) {
				break
			}
			
			// PTR format has each hex digit separated, we need to group them
			hexPart := parts[i+3] + parts[i+2] + parts[i+1] + parts[i]
			hexParts = append(hexParts, hexPart)
		}
		
		ip := strings.Join(hexParts, ":")
		if addr, err := netip.ParseAddr(ip); err == nil {
			return addr
		}
	}
	
	return netip.Addr{}
}

// createRR creates a resource record for the given name and IP
func createRR(name string, ip netip.Addr, ttl int) dns.RR {
	if ip.Is4() {
		return &dns.A{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    uint32(ttl),
			},
			A: net.IP(ip.AsSlice()),
		}
	} else if ip.Is6() {
		return &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
				Ttl:    uint32(ttl),
			},
			AAAA: net.IP(ip.AsSlice()),
		}
	}
	return nil
}