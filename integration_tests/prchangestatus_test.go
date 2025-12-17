package integrationtests

import (
	"common/oyaml"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func changePRStatus(client *testutils.HTTPTestClient, pr *entities.PullRequest, repo *entities.Repository, httpErr *httperrors.APIError, action schemas.PullRequestAction) (*resty.Response, *schemas.PullRequest, error) {
	var r schemas.PullRequest

	resp, err := client.As(testutils.UserIdentities.Krosh).
		SetResult(&r).
		SetBody(testutils.SkipNotification).
		SetError(httpErr).
		Post(fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d/%s", repo.OrgSlug, repo.Slug, pr.ID, action))

	return resp, &r, err
}

func (suite *RwApiTestSuite) TestChangePullRequestStatus() {
	t := suite.T()

	httpErr := httperrors.APIError{}

	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.mustBash(suite.repos.Alpha, fmt.Sprintf(`
		git checkout master
		echo -n "" > %s
		mkdir %s
		echo "
codereview:
  need_ships: 0
  auto_assign: false" > %s
		git add .
		git commit -m "Disabled codereview"
	`, oyaml.OldPath, oyaml.SourceCraftDirectory, oyaml.ReviewPath))

	pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:    suite.repos.Alpha,
		Title:   "TASK-1 add README.md",
		Source:  "branch",
		Target:  "master",
		Publish: utils.PtrFromValue(false),
	})

	type step struct {
		Action schemas.PullRequestAction
		Status entities.PRStatus
		Feed   string
	}

	fExpectedFeed := func(user *entities.User, from, to string) string {
		return fmt.Sprintf(`{
		  "eventType": "PR_UPDATED",
		  "details": {
		    "update": {
			  "diffs": [
			    {
				  "fieldName": "status",
				  "from": "%s",
				  "to": "%s"
			    }
			  ],
			  "userId": "%d"
		    }
		  }
		}`, from, to, user.ID)
	}

	correctFlow := []step{
		{
			Action: schemas.PullRequestActions.Discard,
			Status: entities.PRStatuses.Discarded,
			Feed:   fExpectedFeed(suite.users.Krosh, "draft", "discarded"),
		},
		{
			Action: schemas.PullRequestActions.Reopen,
			Status: entities.PRStatuses.Open,
			Feed:   fExpectedFeed(suite.users.Krosh, "discarded", "open"),
		},
		{
			Action: schemas.PullRequestActions.Discard,
			Status: entities.PRStatuses.Discarded,
			Feed:   fExpectedFeed(suite.users.Krosh, "open", "discarded"),
		},
		{
			Action: schemas.PullRequestActions.Reopen,
			Status: entities.PRStatuses.Open,
			Feed:   fExpectedFeed(suite.users.Krosh, "discarded", "open"),
		},
		{
			Action: schemas.PullRequestActions.MarkDraft,
			Status: entities.PRStatuses.Draft,
			Feed:   fExpectedFeed(suite.users.Krosh, "open", "draft"),
		},
		{
			Action: schemas.PullRequestActions.Publish,
			Status: entities.PRStatuses.Open,
			Feed:   fExpectedFeed(suite.users.Krosh, "draft", "open"),
		},
	}

	for _, step := range correctFlow {
		resp, r, err := changePRStatus(suite.client, pr, suite.repos.Alpha, &httpErr, step.Action)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, step.Status, r.Status, r)
		suite.validateLastFeedItem(suite.T(), pr, step.Feed)
	}

	resp, _, err := changePRStatus(suite.client, pr, suite.repos.Alpha, &httpErr, schemas.PullRequestActions.MarkDraft)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
	require.Equal(t, entities.PRStatuses.Draft, pr.Status, pr)
	lastFeedID := suite.validateLastFeedItem(suite.T(), pr, fExpectedFeed(suite.users.Krosh, "open", "draft"))

	type incorrectStep struct {
		Action schemas.PullRequestAction
		Expect error
	}

	incorrectFlow := []incorrectStep{
		{
			Action: schemas.PullRequestActions.Reopen,
			Expect: except.WrongPRStatusTransition.Build(pr.ID, string(pr.Status)),
		},
		{
			Action: schemas.PullRequestActions.MarkDraft,
			Expect: except.WrongPRStatusTransition.Build(pr.ID, string(pr.Status)),
		},
		{
			Action: schemas.PullRequestActions.Merge,
			Expect: except.WrongPRStatusTransition.Build(pr.ID, string(pr.Status)),
		},
		{
			Action: schemas.PullRequestActions.Abort,
			Expect: except.WrongPRStatusTransition.Build(pr.ID, string(pr.Status)),
		},
	}

	for _, step := range incorrectFlow {
		resp, _, err := changePRStatus(suite.client, pr, suite.repos.Alpha, &httpErr, step.Action)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, httperrors.ErrBadRequest.ErrorCode, httpErr.ErrorCode, string(resp.Body()))
		require.Equal(t, step.Expect.Error(), httpErr.Details, string(resp.Body()))
		feedID := suite.validateLastFeedItem(suite.T(), pr, "{}")
		require.Equal(t, feedID, lastFeedID)
	}
}
