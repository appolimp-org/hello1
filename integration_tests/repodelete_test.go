package integrationtests

import (
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"testing"
)

func (suite *RwApiTestSuite) TestRepoDelete() {
	suite.T().Run("no right to delete", func(t *testing.T) {
		resp, err := suite.client.As(testutils.UserIdentities.Krosh).
			Delete("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 403, resp.StatusCode())

		var r schemas.RepoDetails
		resp, err = suite.client.R().
			SetResult(&r).Get("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 200, resp.StatusCode())
	})

	suite.T().Run("green path", func(t *testing.T) {
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			Delete("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 200, resp.StatusCode())

		var r schemas.RepoDetails
		resp, err = suite.client.R().
			SetResult(&r).Get("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 404, resp.StatusCode())
	})
}
