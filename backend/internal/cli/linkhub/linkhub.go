// Package linkhub is `ikhnos link-hub`: the pod that makes the passive cluster-link series and answers the link prober and the responder pods,
// so that the discovery agent does not. It holds no credential of the server and no Kubernetes token: it is told what to serve by the agent,
// over two signed messages (package linkhub), and keeps serving what it was last told if the agent is gone.
package linkhub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ikhnos/internal/cli"
	"ikhnos/internal/link"
	"ikhnos/internal/linkhub"
)

// Main runs the hub until it is signalled.
func Main(args []string) int {
	fs := cli.NewFlagSet("ikhnos link-hub", os.Stderr)
	listen := fs.String("listen", cli.Env("IKHNOS_LINK_METRICS_LISTEN", ":9104"), "address to serve the link metrics on (Prometheus text at /metrics, scraped by the cluster's telemetry collector), with the signed requests of the link prober and the responder pods beside it")
	ingest := fs.String("ingest-listen", cli.Env("IKHNOS_LINK_HUB_LISTEN", ":9106"), "address the agent sends the rows and the configuration to")
	secretFile := fs.String("secret-file", cli.Env("IKHNOS_LINK_HUB_SECRET_FILE", ""), "file holding the secret shared with the agent, for the rows and the configuration it sends (at least 16 characters; held by no other pod)")
	peersSecretFile := fs.String("peers-secret-file", cli.Env("IKHNOS_LINK_PEERS_SECRET_FILE", ""), "file holding the secret shared with the link prober, for its request for the peers to measure (empty: the request is not answered)")
	iperfSecretFile := fs.String("iperf-secret-file", cli.Env("IKHNOS_LINK_IPERF_SECRET_FILE", ""), "file holding the secret shared with the responder's iperf3 server pods, for their request for the server credentials (empty: no credential is ever served; needs --peers-secret-file)")
	ports := fs.String("prober-ports", cli.Env("IKHNOS_LINK_PROBER_PORTS", ""), "ports (and ranges) the link prober's own traffic uses, left out of the series; the agent leaves them out already, this is the second line")
	if code, done := cli.Parse(fs, args); done {
		return code
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *secretFile == "" {
		return cli.Fatal(log, errors.New("--secret-file is required"))
	}
	secret, err := readSecret(*secretFile)
	if err != nil {
		return cli.Fatal(log, fmt.Errorf("--secret-file: %w", err))
	}
	if *iperfSecretFile != "" && *peersSecretFile == "" {
		return cli.Fatal(log, errors.New("--iperf-secret-file needs --peers-secret-file: the prober's credentials come with its peer list"))
	}
	if err := cli.SecretsDiffer(
		cli.SharedSecret{Flag: "--link-hub-secret-file", File: *secretFile},
		cli.SharedSecret{Flag: "--link-peers-secret-file", File: *peersSecretFile},
		cli.SharedSecret{Flag: "--link-iperf-secret-file", File: *iperfSecretFile},
	); err != nil {
		return cli.Fatal(log, err)
	}
	if *peersSecretFile != "" && *peersSecretFile == *iperfSecretFile {
		return cli.Fatal(log, errors.New("--iperf-secret-file must differ from --peers-secret-file"))
	}

	meter := link.NewMeter(link.NewPeers())
	if err := meter.IgnorePorts(*ports); err != nil {
		return cli.Fatal(log, fmt.Errorf("--prober-ports: %w", err))
	}
	if *peersSecretFile != "" {
		sec, err := readSecret(*peersSecretFile)
		if err != nil {
			return cli.Fatal(log, fmt.Errorf("--peers-secret-file: %w", err))
		}
		meter.ServePeers(sec)
	}
	if *iperfSecretFile != "" {
		sec, err := readSecret(*iperfSecretFile)
		if err != nil {
			return cli.Fatal(log, fmt.Errorf("--iperf-secret-file: %w", err))
		}
		meter.ServeIperf(sec)
	} else {
		// Credentials this hub does not hand to any pod are not kept either.
		meter.Peers.DropIperf(true)
	}
	hub := linkhub.New(secret, meter, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	stopServe, err := link.Serve(*listen, meter, log)
	if err != nil {
		return cli.Fatal(log, fmt.Errorf("--listen: %w", err))
	}
	defer stopServe()
	ln, err := net.Listen("tcp", *ingest)
	if err != nil {
		return cli.Fatal(log, fmt.Errorf("--ingest-listen: %w", err))
	}
	srv := &http.Server{Handler: hub.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 4 << 10}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("the ingest listener stopped", "err", err)
		}
	}()
	log.Info("link hub starting", "version", cli.Version, "metrics", *listen, "ingest", ln.Addr().String(), "peers_api", *peersSecretFile != "", "iperf_api", *iperfSecretFile != "")
	<-ctx.Done()
	shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shut)
	return 0
}

func readSecret(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := []byte(strings.TrimSpace(string(b)))
	if len(s) < 16 {
		return nil, errors.New("must hold a secret of at least 16 characters")
	}
	return s, nil
}
