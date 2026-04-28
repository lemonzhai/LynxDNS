package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/miekg/dns"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
)

type Handler func(msg *dns.Msg, proto string, clientAddr net.Addr) *dns.Msg

type DNSServer struct {
	mu       sync.Mutex
	udp      *dns.Server
	tcp      *dns.Server
	addr     string
	port     int
	handler  Handler
	running  bool
	stopCh   chan struct{}
}

func New(addr string, port int, handler Handler) *DNSServer {
	return &DNSServer{
		addr:    addr,
		port:    port,
		handler: handler,
		stopCh:  make(chan struct{}),
	}
}

func (s *DNSServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	listenAddr := net.JoinHostPort(s.addr, dnsPort(s.port))

	dnsHandler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		proto := "udp"
		if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
			proto = "tcp"
		}

		resp := s.handler(r, proto, w.RemoteAddr())
		if resp != nil {
			w.WriteMsg(resp)
		}
	})

	s.udp = &dns.Server{
		Addr:    listenAddr,
		Net:     "udp",
		Handler: dnsHandler,
	}

	s.tcp = &dns.Server{
		Addr:    listenAddr,
		Net:     "tcp",
		Handler: dnsHandler,
	}

	go func() {
		if err := s.udp.ListenAndServe(); err != nil {
			xlog.Error("DNS UDP server error: %v", err)
		}
	}()

	go func() {
		if err := s.tcp.ListenAndServe(); err != nil {
			xlog.Error("DNS TCP server error: %v", err)
		}
	}()

	s.running = true
	xlog.Info("DNS server started on %s (UDP/TCP)", listenAddr)
	return nil
}

func (s *DNSServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if s.udp != nil {
		s.udp.Shutdown()
	}
	if s.tcp != nil {
		s.tcp.Shutdown()
	}

	s.running = false
	close(s.stopCh)
	xlog.Info("DNS server stopped")
	return nil
}

func (s *DNSServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *DNSServer) Addr() string {
	return net.JoinHostPort(s.addr, dnsPort(s.port))
}

func (s *DNSServer) WaitForShutdown(ctx context.Context) {
	<-s.stopCh
}

func dnsPort(port int) string {
	if port == 0 {
		return "5334"
	}
	return fmt.Sprintf("%d", port)
}
