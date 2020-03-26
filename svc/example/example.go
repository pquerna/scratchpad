package identsvc

import (
	"context"
	"crypto/tls"
	"fmt"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/pquerna/scratchpad/svc"
	"github.com/spf13/pflag"
	"go.uber.org/multierr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"net"
	"net/http"
	"strings"
)

type conf struct {
	ListenAddress    string `mapstructure:"listen_address"`
	ListenTLSCert    string `mapstructure:"listen_tls_cert"`
	ListenTLSKey     string `mapstructure:"listen_tls_key"`
}

type something struct {
	c conf
}

func New() svc.Service {
	return &something{}
}

func (i *something) Name() string {
	return "ident"
}

func (i *something) Flags(f *pflag.FlagSet) error {
	_ = f.String("listen_address", "127.0.0.1:6000", "")
	_ = f.String("listen_tls_cert", "", "")
	_ = f.String("listen_tls_key", "", "")
	return nil
}

func (i *something) Configure() interface{} {
	return &i.c
}

func (i *something) ValidateConfig() error {
	var errs error

	if i.c.ListenTLSCert == "" {
		errs = multierr.Append(errs, fmt.Errorf("listen_tls_cert is empty"))
	}

	if i.c.ListenTLSKey == "" {
		errs = multierr.Append(errs, fmt.Errorf("listen_tls_key is empty"))
	}

	return errs
}

func (i *something) Run(ctx context.Context) error {
	L := ctxzap.Extract(ctx)

	ln, err := net.Listen("tcp", i.c.ListenAddress)
	if err != nil {
		return err
	}

	certs, err := tls.LoadX509KeyPair(i.c.ListenTLSCert, i.c.ListenTLSKey)
	if err != nil {
		return err
	}
	conf := &tls.Config{
		Certificates: []tls.Certificate{certs},
	}

	ln = tls.NewListener(ln, conf)

	rpcServer := grpc.NewServer(
		// you should have some middleware
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer()),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer()),
	)

	// protobufmodulehere.RegisterSomethingServer(rpcServer, newSomethingServer())
	hsrv := health.NewServer()
	hsrv.SetServingStatus("something", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(rpcServer, hsrv)

	return http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			rpcServer.ServeHTTP(w, r)
		} else {
			w.Write([]byte("hello world"))
		}
	}))
}

