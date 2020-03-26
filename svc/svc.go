package svc

import (
	"context"
	"fmt"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"path/filepath"
	"strings"
)

type Service interface {
	Name() string
	Flags(*pflag.FlagSet) error
	Configure() interface{}
	ValidateConfig() error
	Run(ctx context.Context) error
}

type globalConfig struct {
	LogEncoding string `mapstructure:"log_encoding"`
	LogLevel    string `mapstructure:"log_level"`
}

func initLogger(sc *globalConfig) (*zap.Logger, error) {
	zc := zap.NewProductionConfig()
	zc.Sampling = nil
	ll := zapcore.DebugLevel
	err := ll.Set(sc.LogLevel)
	if err != nil {
		return nil, err
	}
	zc.Level.SetLevel(ll)

	switch sc.LogEncoding {
	case "console":
		zc.EncoderConfig = zap.NewDevelopmentEncoderConfig()
		zc.Encoding = "console"
	case "json":
		zc.EncoderConfig = zap.NewProductionEncoderConfig()
		zc.Encoding = "json"
	default:
		return nil, fmt.Errorf("Unknown log_encoding: '%s' must be json or console", sc.LogEncoding)
	}

	l, err := zc.Build()
	if err != nil {
		return nil, err
	}
	zap.ReplaceGlobals(l)
	grpc_zap.ReplaceGrpcLoggerV2(l)
	return l, nil
}

func Command(s Service) *cobra.Command {
	viper.SetEnvPrefix(s.Name())
	viper.AutomaticEnv()

	fn, err := os.Executable()
	if err != nil {
		panic(err)
	}

	rv := &cobra.Command{
		Long: filepath.Base(fn),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := cobra.NoArgs(cmd, args)
			if err != nil {
				return err
			}

			return run(s, cmd, args)
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var errs error
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				err := viper.BindPFlag(f.Name, cmd.Flags().Lookup(f.Name))
				if err != nil {
					errs = multierr.Append(errs, fmt.Errorf("error binding flag: '%s': %w", f.Name, err))
				}
			})
			if errs != nil {
				return errs
			}
			return nil
		},
	}

	flags := rv.Flags()
	err = s.Flags(flags)
	if err != nil {
		panic(err)
	}

	_ = flags.String("log_level", "debug", "")
	_ = flags.String("log_encoding", "console", "")

	envPrefix := strings.ToUpper(s.Name())
	flags.VisitAll(func(f *pflag.Flag) {
		envKey := strings.ToUpper(envPrefix + "_" + f.Name)
		if f.Usage != "" {
			f.Usage += " "
		}
		f.Usage = f.Usage + "${" + envKey + "}"
	})

	return rv
}

func run(s Service, cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ctx = ctxzap.ToContext(ctx, zap.L())

	gc := &globalConfig{}
	err := viper.Unmarshal(gc)
	if err != nil {
		zap.L().Error("Failed to load base configuration",
			zap.Error(err),
		)
		return err
	}

	_, err = initLogger(gc)
	if err != nil {
		zap.L().Error("Failed to configure logging",
			zap.Error(err),
		)
		return err
	}

	err = viper.Unmarshal(s.Configure())
	if err != nil {
		zap.L().Error("Failed to load service configuration",
			zap.Error(err),
		)
	}

	err = s.ValidateConfig()
	if err != nil {
		return err
	}

	envPrefix := strings.ToUpper(s.Name())
	configDump := map[string]interface{}{}
	validEnv := map[string]bool{}
	allkeys := viper.AllKeys()
	for _, k := range allkeys {
		envKey := strings.ToUpper(envPrefix + "_" + k)
		validEnv[envKey] = true
		v := viper.Get(k)
		switch {
		case strings.Contains(strings.ToLower(k), "secret"):
			configDump[k] = "%%%_SECRET_MASKED_%%%"
		case !viper.IsSet(k):
			continue
		default:
			configDump[k] = v
		}
	}

	for k, _ := range envMap() {
		if !strings.HasPrefix(k, envPrefix) {
			continue
		}
		if validEnv[k] {
			continue
		}
		zap.L().Warn("Unknown environment variable detected!",
			zap.String("env_name", k),
		)
	}

	zap.L().Info("Starting service",
		zap.String("version", "0.1.0"),
		zap.String("service", s.Name()),
		zap.Any("config", configDump),
	)

	return s.Run(ctx)
}

func envMap() map[string]string {
	rv := make(map[string]string)
	for _, s := range os.Environ() {
		ix := strings.IndexByte(s, '=')
		if ix >= 0 {
			rv[s[:ix]] = s[ix+1:]
		}
	}
	return rv
}
