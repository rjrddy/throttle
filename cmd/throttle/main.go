package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/rjrddy/throttle/internal/campaign"
	"github.com/rjrddy/throttle/internal/gateway"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "adaptive", "fixed or adaptive")
	algorithm := flag.String("algorithm", "token", "token, sliding, gcra")
	address := flag.String("addr", "127.0.0.1:8086", "HTTP listener")
	demo := flag.Bool("demo", false, "run local synthetic origin and known demo API keys")
	upstream := flag.String("upstream", "", "explicit trusted HTTP origin")
	web := flag.String("web", "apps/web/dist", "built React asset directory")
	bench := flag.Bool("bench", false, "run real HTTP comparison campaign and exit")
	out := flag.String("out", "docs/results/campaign", "raw results directory")
	duration := flag.Duration("duration", 4*time.Second, "campaign offered traffic duration")
	trials := flag.Int("trials", 3, "campaign repeated trials")
	flag.Parse()
	if *bench {
		return benchmark(*out, *duration, *trials)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	keys := map[string]string{}
	if *demo {
		keys = gateway.DemoKeys()
	} else {
		if err := json.Unmarshal([]byte(os.Getenv("THROTTLE_CLIENT_KEYS")), &keys); err != nil {
			return errors.New("THROTTLE_CLIENT_KEYS must contain configured client-id to API-key JSON")
		}
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return err
	}
	loopback := host == "127.0.0.1" || host == "localhost"
	var origin *gateway.DemoOrigin
	var originServer *http.Server
	if *demo {
		origin = &gateway.DemoOrigin{}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		*upstream = "http://" + listener.Addr().String()
		originServer = &http.Server{Handler: origin, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 10 * time.Second}
		go func() {
			if err := originServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("origin_stopped", "error", err)
				cancel()
			}
		}()
		defer func() {
			shutdown, c := context.WithTimeout(context.Background(), 2*time.Second)
			defer c()
			_ = originServer.Shutdown(shutdown)
		}()
	}
	s, err := gateway.NewServer(gateway.Config{Policy: gateway.Policy{Mode: *mode, Algorithm: gateway.Algorithm(*algorithm), Rate: 20, Burst: 12}, Keys: keys, AdminToken: os.Getenv("THROTTLE_ADMIN_TOKEN"), Upstream: *upstream, Capacity: 4, WebDir: *web, LoopbackOnly: loopback, Context: ctx, Logger: logger})
	if err != nil {
		return err
	}
	defer s.Close()
	s.SetDemoOrigin(origin)
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	if *demo {
		s.SetCampaignRunner(func(ctx context.Context) (any, error) {
			base := "http://" + listener.Addr().String()
			if !loopback {
				_, port, _ := net.SplitHostPort(listener.Addr().String())
				base = "http://127.0.0.1:" + port
			}
			before, _ := origin.Counts()
			policy := s.Engine.Snapshot(time.Now()).Policy
			result, err := campaign.RunHTTP(ctx, base, "mixed", 4*time.Second, 52017)
			result.Mode = policy.Mode
			result.Algorithm = policy.Algorithm
			after, peak := origin.Counts()
			result.OriginRequests = after - before
			result.PeakOriginInflight = peak
			result.FinalState = s.Engine.Snapshot(time.Now())
			return result, err
		})
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	logger.Info("ready", "url", "http://"+listener.Addr().String(), "demo", *demo, "policy", *mode, "algorithm", *algorithm)
	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	return server.Shutdown(shutdown)
}
func benchmark(out string, duration time.Duration, trials int) error {
	if duration < time.Second || duration > time.Minute || trials < 1 || trials > 5 {
		return errors.New("duration/trial limit")
	}
	results := []campaign.Result{}
	ctx := context.Background()
	for trial := 0; trial < trials; trial++ {
		for _, scenario := range []string{"mixed", "healthy-burst", "unclassified"} {
			algorithms := []gateway.Algorithm{gateway.TokenBucket}
			if scenario == "mixed" {
				algorithms = []gateway.Algorithm{gateway.TokenBucket, gateway.SlidingWindow, gateway.GCRA}
			}
			for _, algorithm := range algorithms {
				modes := []string{"fixed", "adaptive"}
				if trial%2 == 1 {
					modes = []string{"adaptive", "fixed"}
				}
				for _, mode := range modes {
					result, err := campaign.Isolated(ctx, mode, algorithm, scenario, duration, 52017)
					if err != nil {
						return err
					}
					result.Trial = trial
					path := filepath.Join(out, fmt.Sprintf("%s-%s-%s-t%d.json", scenario, algorithm, mode, trial))
					if err := campaign.Write(path, result); err != nil {
						return err
					}
					results = append(results, result)
					fmt.Printf("%s %s %s t%d legitimate success %d/%d peak origin %d\n", scenario, algorithm, mode, trial, result.Classes["legitimate"].Success, result.Classes["legitimate"].Offered, result.PeakOriginInflight)
				}
			}
		}
	}
	return campaign.Write(filepath.Join(out, "summary.json"), map[string]any{"go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "duration_ms": duration.Milliseconds(), "trials": trials, "seed": 52017, "origin_delay_ms": 60, "origin_capacity": 4, "policy_rate": 20, "policy_burst": 12, "runs": results})
}
