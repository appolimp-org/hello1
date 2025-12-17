package integrationtests

import (
	"common/oyaml"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPullRequestMerge() {
	t := suite.T()
	var prSchema schemas.PullRequest
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   suite.repos.Alpha,
		Source: "branch",
		Target: "master",
	})
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	suite.addFile(suite.repos.Alpha, "master", oyaml.ReviewPath, []byte(`
codereview:
  need_ships: 1
`))

	request := &schemas.MergePullRequestRequest{
		Rebase:       utils.PtrFromValue(true),
		Squash:       utils.PtrFromValue(false),
		DeleteBranch: utils.PtrFromValue(true),
	}

	// wait task complete
	require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))

	t.Run("merge blocked by code review", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d/%s", suite.repos.Alpha.FullSlug(), pr.ID, schemas.PullRequestActions.Merge))).
			MustBe(t, http.StatusBadRequest).
			MustJSON(t, fmt.Sprintf(`{
				"message": "Bad Request",
				"error_code": "bad_request",
				"details": "merge for pr#%d is blocked by merge check 'codereview'"
			}`, pr.ID))
	})

	ctx := context.Background()
	prEntity, err := suite.PullRequestService.Get(ctx, pr.ID)
	require.NoError(t, err)
	err = suite.PullRequestService.SetDecision(
		context.Background(),
		testutils.NewStubAuthenticator(&suite.users.Krosh.Identity),
		prEntity,
		suite.users.Krosh,
		&entities.PullRequestDecisions.Ship,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)

	t.Run("merge ok", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d/%s", suite.repos.Alpha.FullSlug(), pr.ID, schemas.PullRequestActions.Merge))).
			MustBe(t, http.StatusOK).
			MustJSON(t, `{
				"author":{"identity":{"id":"kopatych","src":"iam"}},
				"updateBy":{"identity":{"id":"krosh","src":"iam"}},
				"headCommitSHA":"e8d3ffab552895c19b9fcf7aa264d277cde33881",
				"mergeInfo":{
					"operationId":"<<PRESENCE>>",
					"merger":{"identity":{"id":"krosh","src":"iam"}},
					"error":null,
					"targetCommitSHA":null,
					"mergeCommitSHA":null,
					"params":{"rebase":true,"squash":false,"deleteBranch":true}
				},
				"sourceBranch":"branch",
				"targetBranch":"master",
				"status":"merging"
			}`)
	})

	// wait task complete

	require.NotNil(t, prSchema.MergeInfo.OperationID)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	t.Run("pr finished", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Get(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d", suite.repos.Alpha.FullSlug(), pr.ID))).
			MustBe(t, http.StatusOK).
			MustJSON(t, `{
				"author":{"identity":{"id":"kopatych","src":"iam"}},
				"updateBy":{"identity":{"id":"krosh","src":"iam"}},
				"headCommitSHA":"e8d3ffab552895c19b9fcf7aa264d277cde33881",
				"mergeInfo":{
					"operationId":"<<PRESENCE>>",
					"merger":{"identity":{"id":"krosh","src":"iam"}},
					"error":null,
					"targetCommitSHA":"<<PRESENCE>>",
					"mergeCommitSHA":"<<PRESENCE>>",
					"params":{"rebase":true,"squash":false,"deleteBranch":true}
				},
				"sourceBranch":"branch",
				"targetBranch":"master",
				"status":"merged"
			}`)
	})

	t.Run("branch deleted", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Get(fmt.Sprintf("/api/v1/repos/%s/resolveRevision?rev=%s", suite.repos.Alpha.FullSlug(), pr.SourceBranch))).
			MustBe(t, http.StatusNotFound)
	})
}

func (suite *RwApiTestSuite) TestPullRequestMergeDeleteMaster() {
	t := suite.T()
	var prSchema schemas.PullRequest
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   suite.repos.Alpha,
		Source: "master",
		Target: "branch",
	})
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	request := &schemas.MergePullRequestRequest{
		Rebase:       utils.PtrFromValue(true),
		Squash:       utils.PtrFromValue(false),
		DeleteBranch: utils.PtrFromValue(true), // We'll attempt but thi will fail, as we prohibit to delete default branch
	}

	// wait task complete
	require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))

	ctx := context.Background()
	prEntity, err := suite.PullRequestService.Get(ctx, pr.ID)
	require.NoError(t, err)
	err = suite.PullRequestService.SetDecision(
		context.Background(),
		testutils.NewStubAuthenticator(&suite.users.Krosh.Identity),
		prEntity,
		suite.users.Krosh,
		&entities.PullRequestDecisions.Ship,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)

	t.Run("merge ok", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d/%s", suite.repos.Alpha.FullSlug(), pr.ID, schemas.PullRequestActions.Merge))).
			MustBe(t, http.StatusOK).
			MustJSON(t, `{
				"author":{"identity":{"id":"kopatych","src":"iam"}},
				"updateBy":{"identity":{"id":"krosh","src":"iam"}},
				"headCommitSHA":"<<PRESENCE>>",
				"mergeInfo":{
					"operationId":"<<PRESENCE>>",
					"merger":{"identity":{"id":"krosh","src":"iam"}},
					"error":null,
					"targetCommitSHA":null,
					"mergeCommitSHA":null,
					"params":{"rebase":true,"squash":false,"deleteBranch":true}
				},
				"sourceBranch":"master",
				"targetBranch":"branch",
				"status":"merging"
			}`)
	})

	// wait task complete
	require.NotNil(t, prSchema.MergeInfo.OperationID)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	t.Run("pr finished", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Get(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d", suite.repos.Alpha.FullSlug(), pr.ID))).
			MustBe(t, http.StatusOK).
			MustJSON(t, `{
				"author":{"identity":{"id":"kopatych","src":"iam"}},
				"updateBy":{"identity":{"id":"krosh","src":"iam"}},
				"headCommitSHA":"<<PRESENCE>>",
				"mergeInfo":{
					"operationId":"<<PRESENCE>>",
					"merger":{"identity":{"id":"krosh","src":"iam"}},
					"error":null,
					"targetCommitSHA":"<<PRESENCE>>",
					"mergeCommitSHA":"<<PRESENCE>>",
					"params":{"rebase":true,"squash":false,"deleteBranch":true}
				},
				"sourceBranch":"master",
				"targetBranch":"branch",
				"status":"merged"
			}`)
	})

	t.Run("branch not deleted", func(t *testing.T) {
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&prSchema).
			SetBody(request).
			Get(fmt.Sprintf("/api/v1/repos/%s/resolveRevision?rev=%s", suite.repos.Alpha.FullSlug(), pr.SourceBranch))).
			MustBe(t, http.StatusOK)
	})
}
