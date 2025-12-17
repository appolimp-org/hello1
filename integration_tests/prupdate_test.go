package integrationtests

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func (suite *RwApiTestSuite) TestUpdatePullRequest() {
	t := suite.T()

	httpErr := httperrors.APIError{}

	pr := schemas.PullRequest{}

	request := schemas.CreatePullRequestRequest{
		Title:             "TASK-1 add README.md",
		Source:            "branch",
		Target:            "master",
		NotificationParam: testutils.SkipNotification,
	}
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	resp, err := suite.client.As(testutils.UserIdentities.Krosh).
		SetBody(request).
		SetResult(&pr).
		SetError(&httpErr).
		Post("/api/v1/repos/yandex/alpha/pullrequests")

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))

	testcases := []struct {
		TestName            string
		URL                 string
		Body                *schemas.UpdatePullRequestRequest
		ExpectedStatus      int
		Err                 *httperrors.APIError
		ExpectedTitle       string
		ExpectedDescription string
		User                entities.UserIdentity
	}{
		{
			TestName: "BadRequest (bad pr_id)",
			URL:      "/api/v1/repos/yandex/alpha/pullrequests/bad_pr_id",
			Body: &schemas.UpdatePullRequestRequest{
				Title: utils.PtrFromValue("New Title"),
			},
			ExpectedStatus: 400,
			Err:            httperrors.ErrBadRequest,
			User:           testutils.UserIdentities.Krosh,
		},
		{
			TestName: "Unsupported (not found target branch)",
			URL:      fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", pr.ID),
			Body: &schemas.UpdatePullRequestRequest{
				Title:  utils.PtrFromValue("New Title"),
				Target: utils.PtrFromValue("nonexistent"),
			},
			ExpectedStatus: 400,
			Err:            httperrors.ErrBackendFeatureNotSupported,
			User:           testutils.UserIdentities.Krosh,
		},
		{
			TestName: "NotFound (not found pull request)",
			URL:      "/api/v1/repos/yandex/alpha/pullrequests/999",
			Body: &schemas.UpdatePullRequestRequest{
				Title: utils.PtrFromValue("New Title"),
			},
			ExpectedStatus: http.StatusNotFound,
			Err:            httperrors.ErrNotFoundPullRequest,
			User:           testutils.UserIdentities.Krosh,
		},
		{
			TestName: "OK",
			URL:      fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", pr.ID),
			Body: &schemas.UpdatePullRequestRequest{
				Title:       utils.PtrFromValue("New Title"),
				Description: utils.PtrFromValue("New Description"),
			},
			ExpectedStatus:      http.StatusOK,
			Err:                 nil,
			ExpectedTitle:       "New Title",
			ExpectedDescription: "New Description",
			User:                testutils.UserIdentities.Krosh,
		},
		{
			TestName: "Forbidden – change other user PR",
			URL:      fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", pr.ID),
			Body: &schemas.UpdatePullRequestRequest{
				Title:       utils.PtrFromValue("New Title"),
				Description: utils.PtrFromValue("New Description"),
			},
			ExpectedStatus: http.StatusForbidden,
			Err:            httperrors.ErrForbidden,
			User:           testutils.UserIdentities.Kopatych,
		},
		{
			TestName: "OK – owner can change other user PR",
			URL:      fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", pr.ID),
			Body: &schemas.UpdatePullRequestRequest{
				Title:       utils.PtrFromValue("New Title"),
				Description: utils.PtrFromValue("New Description"),
			},
			ExpectedStatus:      http.StatusOK,
			Err:                 nil,
			ExpectedTitle:       "New Title",
			ExpectedDescription: "New Description",
			User:                testutils.UserIdentities.Admin,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.TestName, func(t *testing.T) {
			result := schemas.PullRequest{}
			httpErr := &httperrors.APIError{}

			resp, err := suite.client.As(tc.User).
				SetResult(&result).
				SetBody(tc.Body).
				SetError(httpErr).
				Post(tc.URL)

			require.NoError(t, err)
			require.Equal(t, tc.ExpectedStatus, resp.StatusCode())

			if resp.StatusCode() == http.StatusOK {
				require.Equal(t, tc.ExpectedTitle, result.Title)
				require.Equal(t, tc.ExpectedDescription, result.Description)
			} else {
				require.Equal(t, tc.Err.ErrorCode, httpErr.ErrorCode)
			}
		})
	}
}
