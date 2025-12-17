package integrationtests

import (
	"gitcore/internal/config"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestNew(t *testing.T) {
	err := fx.ValidateApp(NewApp(t, config.UnittestAppConfigOptions{}))
	require.NoError(t, err)
}
