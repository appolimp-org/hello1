package integrationtests

import (
	log "common/logging"
	"gitcore/internal/access"
	"gitcore/internal/adapters/appsec"
	"gitcore/internal/adapters/audittrails"
	"gitcore/internal/adapters/centrifugo"
	"gitcore/internal/adapters/ci"
	"gitcore/internal/adapters/ide"
	"gitcore/internal/adapters/notify"
	"gitcore/internal/adapters/opensearch"
	"gitcore/internal/adapters/postgres"
	"gitcore/internal/adapters/secrets"
	"gitcore/internal/app"
	"gitcore/internal/cache"
	"gitcore/internal/config"
	"gitcore/internal/git/packcache"
	"gitcore/internal/git/packstorage"
	"gitcore/internal/services/quota"
	"testing"

	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func NewApp(t *testing.T, configOpts config.UnittestAppConfigOptions) fx.Option {
	cfg, err := config.GetUnittestAppConfigOpts(configOpts)
	require.NoError(t, err)
	return fx.Options(
		fx.Options(
			fx.Supply(fx.Annotate(t, fx.As(new(testing.TB)))),
			app.NewApp(cfg),
			access.InjectStubs(cfg.IAM, true),
			notify.InjectStubs(true),
			centrifugo.InjectStubs(true),
			audittrails.InjectStubs(true),
			ci.InjectStubs(true),
			appsec.InjectStubs(),
			ide.InjectStubs(true),
			secrets.InjectStubs(true),
			postgres.DecorateForUnittest(),
			cache.DecorateForUnittest(),
			quota.DecorateForUnittest(),
			packcache.DecorateForUnittest(),
			packstorage.DecorateForUnittest(),
			opensearch.DecorateForUnittest(),
		),
		fx.WithLogger(fxLogger), // comment for debug
		fx.Invoke(log.SetDevelopmentLogger),
	)
}

func fxLogger(logger *zap.Logger) fxevent.Logger {
	logger = logger.WithOptions(zap.IncreaseLevel(zapcore.ErrorLevel)).With(zap.String("component", "fx"))
	return &fxevent.ZapLogger{Logger: logger}
}
