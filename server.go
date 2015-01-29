package main

import (
	"log"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/miekg/dns"
)

type server struct {
	servers    []string
	timeout    time.Duration
	cache      *lru.Cache
	nextServer int
	dnsClient  *dns.Client
	MaxAttempt int
}

func NewServer(servers []string) *server {
	c, _ := lru.New(1 << 20)
	return &server{servers, 2 * time.Second, c, 0, &dns.Client{}, 3}
}

func (s *server) Run() error {
	var (
		group = &sync.WaitGroup{}
		mux   = dns.NewServeMux()
	)
	mux.Handle(".", s)

	group.Add(2)

	go runDNSServer(group, mux, "tcp", "127.0.0.1:53", 0, s.timeout, s.timeout)
	go runDNSServer(group, mux, "udp", "127.0.0.1:53", 0, s.timeout, s.timeout)

	group.Wait()
	return nil
}

func (s *server) NextServer() string {
	srv := s.servers[s.nextServer]
	log.Printf("The server hitted is %q", srv)

	s.nextServer = (s.nextServer + 1) % len(s.servers)

	return srv
}

func runDNSServer(group *sync.WaitGroup, mux *dns.ServeMux, net, addr string, udpsize int, writeTimeout, readTimeout time.Duration) {
	defer group.Done()

	server := &dns.Server{
		Addr:         addr,
		Net:          net,
		Handler:      mux,
		UDPSize:      udpsize,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	log.Printf("%q server launched", net)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func (s *server) FetchDNS(req *dns.Msg) *dns.Msg {
	var retry func(attempt int) *dns.Msg

	retry = func(attempt int) *dns.Msg {
		r, _, err := s.dnsClient.Exchange(req, s.NextServer())
		if err == nil {
			s.cache.Add(req.Question[0], r)

			return r
		} else {
			if attempt < s.MaxAttempt {
				return retry(attempt + 1)
			} else {
				return r
			}
		}
	}

	return retry(0)
}

func (s *server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	t0 := time.Now()

	q := req.Question[0]
	var r *dns.Msg

	if value, ok := s.cache.Get(q); ok {
		r = value.(*dns.Msg)
		r.SetReply(req)
	} else {
		r = s.FetchDNS(req)
	}

	w.WriteMsg(r)

	log.Printf("%q type %d : %v", q.Name, q.Qtype, time.Now().Sub(t0))
}
