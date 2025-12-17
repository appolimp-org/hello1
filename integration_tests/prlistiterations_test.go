package integrationtests

import (
	"common/cgit"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"os"
	"path"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestListPullRequestIterations() {
	t := suite.T()
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	// create pull request

	request := schemas.CreatePullRequestRequest{
		Title:             "AAA-1",
		Source:            "branch",
		Target:            "master",
		Publish:           true,
		NotificationParam: testutils.SkipNotification,
	}

	pr := &schemas.PullRequest{}

	user := suite.users.Kopatych
	resp, err := suite.client.As(user.Identity).
		SetBody(request).
		SetResult(pr).
		Post("/api/v1/repos/yandex/alpha/pullrequests")

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))

	firstIterationID := pr.Iteration

	// initialize cgit

	slug := "yandex/alpha"

	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, slug)
	tmpDir := testutils.TempDir(t, "", "basic")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
	cg.Must(t, "clone", repoURL)
	cg = cgit.NewCGit(path.Join(tmpDir, "alpha")).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
	cg.Must(t, "checkout", "branch")

	// create new commit

	file, err := os.Create(path.Join(tmpDir, "alpha", "newfile.txt"))
	require.NoError(t, err)

	_, err = file.WriteString("new commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"add newfile.txt\"")

	// push new commit

	cg.Must(t, "push")

	// create another commit

	_, err = file.WriteString("another commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"append to newfile.txt\"")

	// push again

	cg.Must(t, "push")

	// check response

	response := &schemas.Collection[schemas.PullRequestIteration]{}
	httpErr := &httperrors.APIError{}

	resp, err = suite.client.R().
		SetResult(response).
		SetError(httpErr).
		Get(fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s/iterations", pr.ID))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))

	require.Equal(t, 3, len(response.Result))

	require.Equal(t, firstIterationID, response.Result[0].ID)

	listCommits := &schemas.Collection[schemas.Commit]{}

	resp, err = suite.client.R().
		SetResult(&listCommits).
		SetError(&httpErr).
		SetQueryParam("rev", "branch").
		SetQueryParam("limit", "3").
		Get("/api/v1/repos/yandex/alpha/commits")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode(), string(resp.Body()))

	for i, iteration := range response.Result {
		require.Equal(t, listCommits.Result[2-i].Hash, iteration.CommitHash)
	}
}
