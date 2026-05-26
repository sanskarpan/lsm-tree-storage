package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/hashicorp/raft"
)

type tcpStreamLayer struct {
	net.Listener
	dialer func(address raft.ServerAddress, timeout time.Duration) (net.Conn, error)
}

func (l *tcpStreamLayer) Dial(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	return l.dialer(address, timeout)
}

func newStreamLayer(cfg Config) (raft.StreamLayer, error) {
	ln, err := net.Listen("tcp", cfg.BindAddress)
	if err != nil {
		return nil, err
	}

	if !cfg.TLS.Enabled {
		return &tcpStreamLayer{
			Listener: ln,
			dialer: func(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
				return net.DialTimeout("tcp", string(address), timeout)
			},
		}, nil
	}

	serverTLS, clientTLS, err := buildTransportTLSConfig(cfg)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &tcpStreamLayer{
		Listener: tls.NewListener(ln, serverTLS),
		dialer: func(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: timeout}
			clientCfg := clientTLS.Clone()
			if clientCfg.ServerName == "" {
				host, _, splitErr := net.SplitHostPort(string(address))
				if splitErr == nil {
					clientCfg.ServerName = host
				}
			}
			return tls.DialWithDialer(dialer, "tcp", string(address), clientCfg)
		},
	}, nil
}

func buildTransportTLSConfig(cfg Config) (*tls.Config, *tls.Config, error) {
	if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
		return nil, nil, fmt.Errorf("cluster: tls cert_file and key_file required when tls is enabled")
	}
	cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		return nil, nil, err
	}

	var pool *x509.CertPool
	if cfg.TLS.CAFile != "" {
		pool = x509.NewCertPool()
		caBytes, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, nil, err
		}
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, nil, fmt.Errorf("cluster: failed to parse tls ca file")
		}
	}

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	if pool != nil {
		serverCfg.ClientCAs = pool
		serverCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	clientCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
		RootCAs:            pool,
		ServerName:         cfg.TLS.ServerName,
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
	}
	return serverCfg, clientCfg, nil
}
