package integrationtests

import (
	"net/http"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestSmoke500() {
	t := suite.T()

	resp, err := suite.client.R().
		Get("/debug/smoke500")

	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode())
}

func (suite *DevEnvTestSuite) TestSmoke500() {
	t := suite.T()

	resp, err := suite.client.R().
		Get("/debug/smoke500")

	require.NoError(t, err)
	require.Equal(t, 500, resp.StatusCode())
}

func (suite *RepoApiTestSuite) TestSmoke403() {
	t := suite.T()

	resp, err := suite.client.R().
		Get("/debug/smoke403")

	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode())
}

func (suite *DevEnvTestSuite) TestSmoke403() {
	t := suite.T()

	resp, err := suite.client.R().
		Get("/debug/smoke403")

	require.NoError(t, err)
	require.Equal(t, 403, resp.StatusCode())
}

func (suite *DevEnvTestSuite) TestRunTemporalSimpleTask() {
	t := suite.T()

	resp, err := suite.client.R().
		Get("/debug/runTemporalSimpleTask?id=1")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
}
