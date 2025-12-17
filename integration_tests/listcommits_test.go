package integrationtests

import (
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertCommits(t *testing.T, prefixes []string, treeResponse *schemas.Collection[schemas.Commit]) {
	require.Equal(t, len(prefixes), len(treeResponse.Result))
	for id, prefix := range prefixes {
		require.True(t, strings.HasPrefix(treeResponse.Result[id].Hash, prefix), "%dth pos: expected %s, got %s", id, prefix, treeResponse.Result[id].Hash)
	}
}

// use `git log  --pretty=format:"%h"`` to get fixtures

func (suite *NonPackApiTestSuite) TestGetCommits() {
	t := suite.T()

	// git log  --pretty=format:"%h"

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	assertCommits(t, []string{
		"6ecf0ef",
		"918c48b",
		"af2d6a6",
		"1669dce",
		"a5b8b09",
		"35e8510",
		"b8e471f",
		"b029517",
	}, &response)

}

func (suite *NonPackApiTestSuite) TestGetCommitsSinceRev() {
	t := suite.T()

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("rev", "35e85108805c84807bc66a02d91535e1e24b38b9").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	assertCommits(t, []string{
		"35e8510",
		"b029517",
	}, &response)
}

func (suite *NonPackApiTestSuite) TestGetCommitsBadRev() {
	t := suite.T()

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("rev", "lol").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode())
}

func (suite *NonPackApiTestSuite) TestGetCommitsForFile() {
	t := suite.T()

	// git log  --pretty=format:"%h" -- LICENSE
	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("path", "LICENSE").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	assertCommits(t, []string{
		"b029517",
	}, &response)
}

func (suite *NonPackApiTestSuite) TestGetCommitsForDir() {
	t := suite.T()

	// git log  --pretty=format:"%h" -- go

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("path", "go").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	assertCommits(t, []string{
		"918c48b",
	}, &response)

}

func (suite *NonPackApiTestSuite) TestGetCommitsForFileMultipleBranches() {
	t := suite.T()

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("path", "CHANGELOG").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	assertCommits(t, []string{
		"b8e471f",
	}, &response)

}

func (suite *NonPackApiTestSuite) TestGetCommitsUntilMergeBaseWith() {
	t := suite.T()

	response := schemas.Collection[schemas.Commit]{}
	httpErr := httperrors.APIError{}

	resp, err := suite.client.R().
		SetResult(&response).
		SetError(&httpErr).
		SetQueryParam("rev", "main").
		SetQueryParam("untilMergeBaseWith", "branch").
		Get("/api/v1/repos/yandex/threedotdiff/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode(), string(resp.Body()))
	assertCommits(t, []string{
		"77ba86b4", // main commit 3
		"aa28cbf0", // 2-nd merge
		"1162a6b8", // 1-st merge
		"3d5bc73b", // main commit 2
	}, &response)

}
