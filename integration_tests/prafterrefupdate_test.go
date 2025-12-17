package integrationtests

import (
	"common/cgit"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPullRequestOnAfterRefUpdate() {
	t := suite.T()
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	slug := "yandex/alpha"
	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, slug)

	tmpDir := testutils.TempDir(t, "", "basic")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	cg.Must(t, "clone", repoURL)
	cg = cgit.NewCGit(path.Join(tmpDir, "alpha")).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	cg.Must(t, "checkout", "branch")

	file, err := os.Create(path.Join(tmpDir, "alpha", "newfile.txt"))
	require.NoError(t, err)

	_, err = file.WriteString("new commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"add newfile.txt\"")

	_, err = file.WriteString("another commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"append to newfile.txt\"")

	testcases := []struct {
		name                 string
		body                 schemas.CreatePullRequestRequest
		expectedNewIteration bool
		expectedPRStatus     entities.PRStatus
	}{
		{
			name: "status=open",
			body: schemas.CreatePullRequestRequest{
				Title:             "TASK-N",
				Description:       utils.PtrFromValue("some changes"),
				Source:            "branch",
				Target:            "master",
				Publish:           true,
				NotificationParam: testutils.SkipNotification,
			},
			expectedNewIteration: true,
			expectedPRStatus:     entities.PRStatuses.Open,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pr1 := schemas.PullRequest{}

			resp, err := suite.client.As(suite.users.Kopatych.Identity).
				SetBody(tc.body).
				SetResult(&pr1).
				Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests", slug))

			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))

			prID := pr1.ID
			headCommitSHA := pr1.HeadCommitSHA

			cg.Must(t, "push")

			pr2 := schemas.PullRequest{}

			resp, err = suite.client.R().
				SetResult(&pr2).
				Get(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%s", slug, prID))

			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))

			require.NotEqual(t, headCommitSHA, pr2.HeadCommitSHA)
			require.Equal(t, tc.expectedPRStatus, pr2.Status)

			if tc.expectedNewIteration {
				require.NotEqual(t, pr1.Iteration, pr2.Iteration)
			} else {
				require.Equal(t, pr1.Iteration, pr2.Iteration)
			}
		})
	}
}
