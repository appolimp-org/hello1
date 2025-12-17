package integrationtests

import (
	"common/testutils/yarequire"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListSystemStatuses() {
	t := suite.T()

	resp, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Get("/issue_statuses")

	require.NoError(t, err)
	yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
}
