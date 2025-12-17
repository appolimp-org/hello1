package integrationtests

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"sort"
	"testing"
)

func (suite *RwApiTestSuite) TestUserListPullRequests() {
	t := suite.T()
	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	var titles []string
	var ids []schemas.JsonID

	var reviewedTitles []string
	var reviewedIDs []schemas.JsonID

	pr := &schemas.PullRequest{}
	user := suite.users.Kopatych
	suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
	prCount := 6

	for i := 1; i <= prCount; i++ {
		request := schemas.CreatePullRequestRequest{
			Title:             fmt.Sprintf("TASK-%d", i),
			Description:       utils.PtrFromValue(fmt.Sprintf("DESCRIPTION-%d", prCount-i)),
			Source:            "branch",
			Target:            "master",
			NotificationParam: testutils.SkipNotification,
		}

		testutils.Expect(suite.client.As(user.Identity).
			SetBody(request).
			SetResult(pr).
			Post("/api/v1/repos/yandex/alpha/pullrequests")).
			MustBe(t, http.StatusCreated)

		ids = append(ids, pr.ID)
		titles = append(titles, pr.Title)

		// Assign self as a reviewer
		if i%2 == 0 {
			_, err := client.Update(testutils.AuthorizeGRPC(user.Identity), &pb.UpdateReviewersRequest{
				PrId: grpc_marshalling.IDInverse(pr.ID.MustToUint64()),
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc_marshalling.IDInverse(user.ID),
					},
				},
			})
			require.NoError(t, err)

			reviewedIDs = append(reviewedIDs, pr.ID)
			reviewedTitles = append(reviewedTitles, pr.Title)
		}
	}

	t.Run("test my pull requests", func(t *testing.T) {
		response := &schemas.Collection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.As(user.Identity).
			SetResult(response).
			SetError(httpErr).
			Get("/api/v1/me/pullrequests?feed=my")

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, len(ids), len(response.Result))

		sort.Slice(response.Result, func(i, j int) bool { return response.Result[i].ID < response.Result[j].ID })

		for i, pr := range response.Result {
			require.Equal(t, user.Identity, pr.Author.Identity, "only my pull requests should be here")
			require.Equal(t, ids[i], pr.ID)
			require.Equal(t, titles[i], pr.Title)
		}
	})

	t.Run("test my pull requests (empty)", func(t *testing.T) {
		response := &schemas.Collection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.As(suite.users.Raichu.Identity).
			SetResult(response).
			SetError(httpErr).
			Get("/api/v1/me/pullrequests?feed=my")

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Empty(t, response.Result)
	})

	t.Run("test pull requests as another user (empty)", func(t *testing.T) {
		response := &schemas.Collection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.As(suite.users.Kopatych.Identity).
			SetResult(response).
			SetError(httpErr).
			Get(fmt.Sprintf("/api/v1/users/%s/pullrequests?feed=my", suite.users.Raichu.Username))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Empty(t, response.Result)
	})

	t.Run("test assigned pull requests", func(t *testing.T) {
		response := &schemas.Collection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.As(user.Identity).
			SetResult(response).
			SetError(httpErr).
			Get("/api/v1/me/pullrequests?feed=review")

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, len(reviewedIDs), len(response.Result))

		sort.Slice(response.Result, func(i, j int) bool { return response.Result[i].ID < response.Result[j].ID })

		for i, pr := range response.Result {
			require.Equal(t, reviewedIDs[i], pr.ID)
			require.Equal(t, reviewedTitles[i], pr.Title)
		}
	})

	t.Run("test assigned pull requests as another user", func(t *testing.T) {
		response := &schemas.Collection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.As(suite.users.Slowpoke.Identity).
			SetResult(response).
			SetError(httpErr).
			Get(fmt.Sprintf("/api/v1/users/%s/pullrequests?feed=review", user.Username))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, len(reviewedIDs), len(response.Result))

		sort.Slice(response.Result, func(i, j int) bool { return response.Result[i].ID < response.Result[j].ID })

		for i, pr := range response.Result {
			require.Equal(t, reviewedIDs[i], pr.ID)
			require.Equal(t, reviewedTitles[i], pr.Title)
		}
	})

	t.Run("test full list", func(t *testing.T) {
		response := &schemas.RevisionedCollection[schemas.PullRequest]{}
		httpErr := &httperrors.APIError{}

		resp, err := suite.client.R().
			SetResult(response).
			SetError(httpErr).
			Get("/api/v1/repos/yandex/alpha/pullrequests")

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
		require.Equal(t, len(ids), len(response.Result))

		sort.Slice(response.Result, func(i, j int) bool { return response.Result[i].ID < response.Result[j].ID })

		for i, pr := range response.Result {
			require.Equal(t, ids[i], pr.ID)
			require.Equal(t, titles[i], pr.Title)
		}
		require.Equal(t, prCount, response.Count)
	})

	t.Run("test sorted paged list", func(t *testing.T) {
		httpErr := &httperrors.APIError{}

		var totalResult = testutils.NewListCollection[schemas.PullRequest](
			suite.client.R().SetError(&httpErr),
			"/api/v1/repos/yandex/alpha/pullrequests",
		).
			LoadSorted(t, "author_id,-repo_id,title", 2).
			MustHaveLen(t, prCount).
			Result()

		sort.Slice(totalResult, func(i, j int) bool { return totalResult[i].ID < totalResult[j].ID })

		for i, pr := range totalResult {
			require.Equal(t, ids[i], pr.ID)
			require.Equal(t, titles[i], pr.Title)
		}
	})
}
