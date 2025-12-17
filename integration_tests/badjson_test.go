package integrationtests

import (
	"common/testutils/yarequire"
	"github.com/stretchr/testify/require"
	"net/http"
)

func (suite *RepoApiTestSuite) TestRequestID() {

	resp, err := suite.client.R().SetHeader("x-request-id", "awooga").Get("/debug/requestID")
	yarequire.StatusCode(suite.T(), resp, err, http.StatusNotFound)
}

func (suite *DevEnvTestSuite) TestRequestID() {

	resp, err := suite.client.R().SetHeader("x-request-id", "awooga").Get("/debug/requestID")
	yarequire.StatusCode(suite.T(), resp, err, http.StatusOK)
	require.Equal(suite.T(), "awooga", string(resp.Body()))
}

func (suite *RepoApiTestSuite) TestTracing() {

	resp, err := suite.client.R().Get("/debug/tracing")
	yarequire.StatusCode(suite.T(), resp, err, http.StatusNotFound)
}

func (suite *DevEnvTestSuite) TestTracing() {

	resp, err := suite.client.R().Get("/debug/tracing")
	yarequire.StatusCode(suite.T(), resp, err, http.StatusOK)
}
