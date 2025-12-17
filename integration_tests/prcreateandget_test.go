package integrationtests

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"encoding/json"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestCreateAndGetPullRequest() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	testcases := []struct {
		name            string
		user            entities.UserIdentity
		request         schemas.CreatePullRequestRequest
		wantStatus      entities.PRStatus
		wantDescription string
	}{
		{
			name: "draft no description",
			user: suite.users.Kopatych.Identity,
			request: schemas.CreatePullRequestRequest{
				Description:       nil,
				Publish:           false,
				NotificationParam: testutils.SkipNotification,
			},
			wantStatus: entities.PRStatuses.Draft,
		},
		{
			name: "draft with description",
			user: suite.users.Kopatych.Identity,
			request: schemas.CreatePullRequestRequest{
				Description:       utils.PtrFromValue("added readme"),
				Publish:           false,
				NotificationParam: testutils.SkipNotification,
			},
			wantStatus:      entities.PRStatuses.Draft,
			wantDescription: "added readme",
		},
		{
			name: "publish with description",
			user: suite.users.Kopatych.Identity,
			request: schemas.CreatePullRequestRequest{
				Description:       utils.PtrFromValue("added readme"),
				Publish:           true,
				NotificationParam: testutils.SkipNotification,
			},
			wantStatus:      entities.PRStatuses.Open,
			wantDescription: "added readme",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var result schemas.PullRequest

			tc.request.Title = "TASK-1 add README.md"
			tc.request.Source = "branch"
			tc.request.Target = "master"

			expectedJSON := `
				{
					"title": "TASK-1 add README.md",
					"description": "<<PRESENCE>>",
					"repoName": "alpha",
					"repoSlug": "alpha",
					"orgSlug": "yandex",
					"projSlug": null,
					"author": {
						"identity": {
							"id": "kopatych",
							"src": "iam"
						},
						"username": "kopatych"
					},
					"updateBy": {
						"identity": {
							"id": "kopatych",
							"src": "iam"
						},
						"username": "kopatych"
					},
					"headCommitSHA": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
					"mergeInfo": "<<PRESENCE>>",
					"sourceBranch": "branch",
					"targetBranch": "master",
					"status": "<<PRESENCE>>",
					"settings": {
						"autoPublish": false
					},
					"validationResult": "ok"
				}`

			testutils.Expect(suite.client.As(tc.user).
				SetBody(tc.request).
				SetResult(&result).
				Post("/api/v1/repos/yandex/alpha/pullrequests")).
				MustBe(t, http.StatusCreated).
				MustJSON(t, expectedJSON)

			require.Equal(t, result.Status, tc.wantStatus)
			require.Equal(t, result.Description, tc.wantDescription)

			testutils.Expect(suite.client.As(tc.user).
				SetBody(tc.request).
				Get(fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", result.ID))).
				MustBe(t, http.StatusOK).
				MustJSON(t, expectedJSON)
		})
	}
}

func (suite *RepoApiTestSuite) TestCanCreatePRWithoutRoleToPublicRepository() {
	t := suite.T()

	httpErr := httperrors.APIError{}

	result := schemas.PullRequest{}

	request := schemas.CreatePullRequestRequest{
		Title:             "TASK-1 add README.md",
		Description:       utils.PtrFromValue(""),
		Source:            "branch",
		Target:            "master",
		Publish:           false,
		NotificationParam: testutils.SkipNotification,
	}

	resp, err := suite.client.As(testutils.UserIdentities.Krosh).
		SetBody(request).
		SetResult(&result).
		SetError(&httpErr).
		Post("/api/v1/repos/yandex/alpha/pullrequests")

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))
}

func (suite *RepoApiTestSuite) TestCantCreatePRAnonymousToPublicRepository() {
	t := suite.T()

	httpErr := httperrors.APIError{}

	result := schemas.PullRequest{}

	request := schemas.CreatePullRequestRequest{
		Title:             "TASK-1 add README.md",
		Description:       utils.PtrFromValue(""),
		Source:            "branch",
		Target:            "master",
		Publish:           false,
		NotificationParam: testutils.SkipNotification,
	}

	resp, err := suite.client.Anonymous().
		SetBody(request).
		SetResult(&result).
		SetError(&httpErr).
		Post("/api/v1/repos/yandex/alpha/pullrequests")

	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode(), string(resp.Body()))
}

func (suite *RepoApiTestSuite) TestCreatePullRequestNoSuchBranch() {
	t := suite.T()

	testcases := []struct {
		Name   string
		Source string
		Target string
		Error  *httperrors.APIError
	}{
		{
			Name:   "no source",
			Source: "blahblah",
			Target: "master",
			Error:  httperrors.ErrNotFoundBranch.WithDetails("branch blahblah does not exist"),
		},
		{
			Name:   "no target",
			Source: "branch",
			Target: "heehee",
			Error:  httperrors.ErrNotFoundBranch.WithDetails("branch heehee does not exist"),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			result := schemas.PullRequest{}

			request := schemas.CreatePullRequestRequest{
				Title:             "TASK-1 add README.md",
				Source:            tc.Source,
				Target:            tc.Target,
				NotificationParam: testutils.SkipNotification,
			}

			resp, err := suite.client.As(testutils.UserIdentities.Admin).
				SetBody(request).
				SetResult(&result).
				SetError(&httpErr).
				Post("/api/v1/repos/yandex/alpha/pullrequests")

			require.NoError(t, err)
			require.Equal(t, http.StatusNotFound, resp.StatusCode(), string(resp.Body()))
			//require.Equal(t, tc.Error.ErrorCode, httpErr.ErrorCode)
			//require.Equal(t, tc.Error.Details, httpErr.Details)
		})
	}
}

func (suite *RwApiTestSuite) TestGetPullRequestValidation() {
	testcases := []struct {
		Name          string
		DeletedBranch string
		Validation    *entities.PRValidationResult
	}{
		{
			Name:          "delete source",
			DeletedBranch: "branch",
			Validation:    &entities.PRValidationResults.SourceDeleted,
		},
		{
			Name:          "delete target",
			DeletedBranch: "master",
			Validation:    &entities.PRValidationResults.TargetDeleted,
		},
	}

	for _, tc := range testcases {
		suite.Run(tc.Name, func() {
			t := suite.T()
			ctx := context.Background()

			httpErr := httperrors.APIError{}

			result := schemas.PullRequest{}

			request := schemas.CreatePullRequestRequest{
				Title:             "TASK-1 add README.md",
				Source:            "branch",
				Target:            "master",
				NotificationParam: testutils.SkipNotification,
			}

			user := suite.users.Admin
			resp, err := suite.client.As(user.Identity).
				SetBody(request).
				SetResult(&result).
				SetError(&httpErr).
				Post("/api/v1/repos/yandex/alpha/pullrequests")

			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))

			pr := &schemas.PullRequest{}
			require.NoError(t, json.Unmarshal(resp.Body(), pr))

			repo, err := suite.RepoRepo.GetRepository(ctx, "yandex", "alpha")
			require.NoError(t, err)

			err = suite.RefRepoFactory.Build(repo.ID).
				RemoveRef(ctx, plumbing.NewBranchReferenceName(tc.DeletedBranch))
			require.NoError(t, err)

			resp, err = suite.client.As(user.Identity).
				SetResult(&result).
				SetError(&httpErr).
				Get(fmt.Sprintf("/api/v1/repos/yandex/alpha/pullrequests/%s", pr.ID))

			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
			require.NoError(t, json.Unmarshal(resp.Body(), pr))

			require.Equal(t, tc.Validation, pr.ValidationResult)
		})
	}
}

func (suite *RepoApiTestSuite) TestCreateAndNotifyPullRequest() {
	t := suite.T()
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	testcases := []struct {
		name     string
		user     entities.UserIdentity
		request  schemas.CreatePullRequestRequest
		wantJSON string
	}{
		{
			name: "with_subscription",
			user: suite.users.Kopatych.Identity,
			request: schemas.CreatePullRequestRequest{
				Title:  "Title",
				Source: "branch",
				Target: "master",
				NotificationParam: schemas.NotificationParam{
					SkipSubscription: false, // add check in OO-1257
					SkipNotify:       true,
				},
			},
			wantJSON: `
				{
					"id": "<<PRESENCE>>",
					"author": {
						"identity": {"id": "kopatych", "src": "iam"}
					}
				}`,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var result schemas.PullRequest

			testutils.Expect(suite.client.As(tc.user).
				SetBody(tc.request).
				SetResult(&result).
				Post("/api/v1/repos/yandex/alpha/pullrequests")).
				MustBe(t, http.StatusCreated).
				MustJSON(t, tc.wantJSON)

			suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
		})
	}
}
