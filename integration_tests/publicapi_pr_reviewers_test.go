package integrationtests

import (
	"common/functools"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"net/http"
	"path"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIAutoAssign() {
	const reviewersPath = "reviewers_delta"
	const pathToUser = "user.id"

	t := suite.T()

	pr := suite.makePullRequest(
		suite.users.Barash,
		&makePrOptions{
			Repo:  suite.repos.Alpha,
			Title: "1 - alpha",
		},
	)

	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	cg, tmpDir := suite.initCGit(suite.users.Barash, suite.repos.Alpha.FullSlug())

	users := []*entities.User{suite.users.Kopatych, suite.users.Krosh, suite.users.Pikachu}

	commit(t, &cg, path.Join(tmpDir, suite.repos.Alpha.Slug, oyaml.ReviewPath), fmt.Sprintf(`
codereview:
  need_ships: 1
  rules:
    - patterns:
        - '**'
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
    - patterns:
        - "**"
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
`, getUsernames(users)...))
	cg.Must(t, "push")

	t.Run("happy path", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.AutoAssignBody{}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/reviewers/auto", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug, pr.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusOK)

		uuids, err := getUUIDS(resp.Body(), reviewersPath, pathToUser)
		require.NoError(t, err)

		require.True(t, isBelongToSetOfUsers(uuids, users))
	})

	t.Run("happy path by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.AutoAssignBody{}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers/auto", pr.UUID.String()))
		yarequire.StatusCode(t, resp, err, http.StatusOK)

		uuids, err := getUUIDS(resp.Body(), reviewersPath, pathToUser)
		require.NoError(t, err)

		require.True(t, isBelongToSetOfUsers(uuids, users))
	})

	t.Run("with notify", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.AutoAssignBody{
				Silent: false,
			}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers/auto", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusOK)

		uuids, err := getUUIDS(resp.Body(), reviewersPath, pathToUser)
		require.NoError(t, err)

		require.True(t, isBelongToSetOfUsers(uuids, users))

		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		ids := suite.getSetID(uuids)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
		notifyHistory := suite.NotifyMessages(ctx)

		for _, notify := range notifyHistory {
			id, err := grpc_marshalling.IDDirect(notify.Receiver.ID)
			require.NoError(t, err)

			_, ok := ids[id]
			require.True(t, ok)
		}
	})

	t.Run("access denied", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Slowpoke.Identity).
			SetBody(&pbPub.AutoAssignBody{}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/reviewers/auto", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug, pr.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})
}

func (suite *RwApiTestSuite) getSetID(uuids []uuid.UUID) map[uint64]struct{} {
	users, err := suite.UserRepo.GetBulkByUUIDs(context.Background(), uuids)
	require.NoError(suite.T(), err)

	return functools.SliceToMapKV(users, func(user *entities.User) (uint64, struct{}) {
		return user.ID, struct{}{}
	})
}

func getUUIDS(resp []byte, path, pathToUser string) ([]uuid.UUID, error) {
	reviewersDelta, err := yarequire.GetArrayFromJSON(resp, path)
	if err != nil {
		return nil, err
	}

	uuids := make([]uuid.UUID, len(reviewersDelta))
	for i, byteArr := range reviewersDelta {
		u, err := yarequire.GetUUIDFromJSON(byteArr, pathToUser)
		if err != nil {
			return nil, err
		}

		uuids[i] = u
	}

	return uuids, nil
}

func isSortedBy(resp []byte, path string, field string) (bool, error) {
	reviewers, err := yarequire.GetArrayFromJSON(resp, path)
	if err != nil {
		return false, err
	}

	createdArr := make([]time.Time, len(reviewers))
	for i, byteArr := range reviewers {
		u, err := yarequire.GetStringFromJSON(byteArr, field)
		if err != nil {
			return false, err
		}
		createdAt, err := time.Parse("2006-01-02T15:04:05.999999Z", u)
		if err != nil {
			return false, err
		}

		createdArr[i] = createdAt
		if i != 0 && createdArr[i].Compare(createdArr[i-1]) == -1 {
			return false, nil
		}
	}

	return true, nil
}

func getUsernames(users []*entities.User) []any {
	userNames := make([]any, len(users))
	for i, user := range users {
		userNames[i] = user.Username
	}
	return userNames
}

func isBelongToSetOfUsers(uuids []uuid.UUID, users []*entities.User) bool {
	userSet := make(map[uuid.UUID]struct{}, len(users))
	for _, user := range users {
		userSet[user.UUID] = struct{}{}
	}
	for _, u := range uuids {
		if _, ok := userSet[u]; !ok {
			return false
		}
	}
	return true
}

type pageOpts struct {
	pageSize *int
	sortBy   []string
}

func (suite *RwApiTestSuite) TestListReviewersPublicAPI() {
	const reviewersPath = "reviewers"
	const pathToUserID = "user.id"

	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)
	org, err := suite.OrgRepo.GetOrganization(ctx, suite.repos.AuthRepoPrivate.OrgSlug)
	require.NoError(t, err)

	err = suite.RepoService.UpdateVisibility(ctx, suite.repos.AuthRepoPrivate.OrgSlug, suite.repos.AuthRepoPrivate.Slug, entities.Visibilities.Private)
	require.NoError(t, err)

	err = suite.OrgService.RemoveUser(ctx, nil, org.Identity, suite.users.Slowpoke.Identity)
	require.NoError(t, err)

	for name, test := range map[string]struct {
		repo   *entities.Repository
		users  []*entities.User
		code   int
		pgOpts pageOpts
		user   *entities.User

		byID bool
	}{
		"happy path": {
			repo: suite.repos.Alpha,
			users: []*entities.User{
				suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu,
			},
			code: http.StatusOK,
			user: suite.users.Barash,
		},
		"happy path by id": {
			repo: suite.repos.Alpha,
			users: []*entities.User{
				suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu,
			},
			code: http.StatusOK,
			byID: true,
			user: suite.users.Barash,
		},
		"access denied": {
			repo: suite.repos.AuthRepoPrivate,
			users: []*entities.User{
				suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu,
			},
			code: http.StatusForbidden,
			user: suite.users.Slowpoke,
		},
		"with page size": {
			repo: suite.repos.Alpha,
			users: []*entities.User{
				suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu,
			},
			code: http.StatusOK,
			byID: true,
			user: suite.users.Barash,
			pgOpts: pageOpts{
				pageSize: utils.PtrFromValue(1),
			},
		},
		"with sort by": {
			repo: suite.repos.Alpha,
			users: []*entities.User{
				suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu,
			},
			code: http.StatusOK,
			byID: true,
			user: suite.users.Barash,
			pgOpts: pageOpts{
				sortBy: []string{"created_at"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pr := suite.makePullRequest(
				suite.users.Barash,
				&makePrOptions{
					Repo:  test.repo,
					Title: name,
				},
			)

			for _, user := range test.users {
				suite.addReviewer(t, pr, user)
			}

			makeRequest := func(token *string) (*resty.Response, error) {
				req := suite.gwClient.As(test.user.Identity)

				if test.pgOpts.pageSize != nil {
					req.SetQueryParam("page_size", strconv.Itoa(*test.pgOpts.pageSize))
				}

				if test.pgOpts.sortBy != nil {
					req.SetQueryParam("sort_by", strings.Join(test.pgOpts.sortBy, ","))
				}

				if token != nil {
					req.SetQueryParam("page_token", *token)
				}

				if test.byID {
					return req.Get(fmt.Sprintf("/pulls/id:%s/reviewers", pr.UUID.String()))
				}
				return req.Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/reviewers", test.repo.OrgSlug, test.repo.Slug, pr.PublicID))
			}

			resp, err := makeRequest(nil)

			yarequire.StatusCode(t, resp, err, test.code)

			if test.code != http.StatusOK {
				return
			}

			uuids, err := getUUIDS(resp.Body(), reviewersPath, pathToUserID)
			require.NoError(t, err)

			require.True(t, isBelongToSetOfUsers(uuids, test.users))

			if test.pgOpts.sortBy != nil {
				ok, err := isSortedBy(resp.Body(), "reviewers", "created_at")
				require.NoError(t, err)
				require.True(t, ok)
			}

			if test.pgOpts.pageSize == nil || *test.pgOpts.pageSize >= len(test.users) {
				return
			}

			require.Len(t, uuids, *test.pgOpts.pageSize)

			token, err := yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
			require.NoError(t, err)

			resp, err = makeRequest(&token)
			yarequire.StatusCode(t, resp, err, test.code)

			uuids, err = getUUIDS(resp.Body(), reviewersPath, pathToUserID)
			require.NoError(t, err)

			require.True(t, isBelongToSetOfUsers(uuids, test.users))

			require.Len(t, uuids, *test.pgOpts.pageSize)
		})
	}
}

func (suite *RwApiTestSuite) TestSetDecisionPublicAPI() {

	t := suite.T()

	users := []*entities.User{suite.users.Krosh, suite.users.Kopatych, suite.users.Pikachu}
	pr := suite.makePullRequest(
		suite.users.Barash,
		&makePrOptions{
			Repo:  suite.repos.Alpha,
			Title: "set decision",
		},
	)

	for _, user := range users {
		suite.addReviewer(t, pr, user)
	}

	t.Run("happy path", func(t *testing.T) {
		decision := pbPub.ReviewDecision_approve

		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			SetBody(&pbPub.SetDecisionBody{ReviewDecision: decision}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/decision", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug, pr.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusOK)

		decisionStr, err := yarequire.GetStringFromJSON(resp.Body(), "created_decision")
		require.NoError(t, err)
		require.Equal(t, "approve", decisionStr)

		uuid, err := yarequire.GetStringFromJSON(resp.Body(), "pull_request_id")
		require.NoError(t, err)
		require.Equal(t, pr.UUID.String(), uuid)
	})

	t.Run("happy path by id", func(t *testing.T) {
		decision := pbPub.ReviewDecision_block

		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.SetDecisionBody{ReviewDecision: decision}).
			Post(fmt.Sprintf("/pulls/id:%s/decision", pr.UUID.String()))
		yarequire.StatusCode(t, resp, err, http.StatusOK)

		decisionStr, err := yarequire.GetStringFromJSON(resp.Body(), "created_decision")
		require.NoError(t, err)
		require.Equal(t, "block", decisionStr)

		uuid, err := yarequire.GetStringFromJSON(resp.Body(), "pull_request_id")
		require.NoError(t, err)
		require.Equal(t, pr.UUID.String(), uuid)
	})

	t.Run("access denied", func(t *testing.T) {
		decision := pbPub.ReviewDecision_abstain

		resp, err := suite.gwClient.As(suite.users.Slowpoke.Identity).
			SetBody(&pbPub.SetDecisionBody{ReviewDecision: decision}).
			Post(fmt.Sprintf("/pulls/id:%s/decision", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("invalid decision", func(t *testing.T) {
		decision := pbPub.ReviewDecision_review_decision_unspecified

		resp, err := suite.gwClient.As(suite.users.Pikachu.Identity).
			SetBody(&pbPub.SetDecisionBody{ReviewDecision: decision}).
			Post(fmt.Sprintf("/pulls/id:%s/decision", pr.UUID.String()))
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})
}

func (suite *RwApiTestSuite) TestUpdateReviewersPublicAPI() {
	t := suite.T()

	const path = "reviewers"
	const pathToUserID = "user.id"

	pr := suite.makePullRequest(
		suite.users.Barash,
		&makePrOptions{
			Repo:  suite.repos.Alpha,
			Title: "set decision",
		},
	)

	users := []*entities.User{suite.users.Kopatych}

	t.Run("happy path", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.UpdateReviewersBody{
				ReviewersDelta: []*pbPub.ReviewerDelta{
					{
						Action: pbPub.DeltaAction_add,
						UserId: suite.users.Kopatych.UUID.String(),
					},
				},
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/reviewers", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug, pr.PublicID))

		yarequire.StatusCode(t, resp, err, http.StatusOK)

		uuids, err := getUUIDS(resp.Body(), path, pathToUserID)
		require.NoError(t, err)

		require.True(t, isBelongToSetOfUsers(uuids, users))
	})

	users = []*entities.User{suite.users.Krosh}

	t.Run("happy path by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.UpdateReviewersBody{
				ReviewersDelta: []*pbPub.ReviewerDelta{
					{
						Action: pbPub.DeltaAction_remove,
						UserId: suite.users.Kopatych.UUID.String(),
					},
					{
						Action: pbPub.DeltaAction_add,
						UserId: suite.users.Krosh.UUID.String(),
					},
				},
			}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusOK)

		uuids, err := getUUIDS(resp.Body(), path, pathToUserID)
		require.NoError(t, err)

		require.True(t, isBelongToSetOfUsers(uuids, users))
	})

	t.Run("permission denied", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Pikachu.Identity).
			SetBody(&pbPub.UpdateReviewersBody{
				ReviewersDelta: []*pbPub.ReviewerDelta{
					{
						Action: pbPub.DeltaAction_remove,
						UserId: suite.users.Kopatych.UUID.String(),
					},
					{
						Action: pbPub.DeltaAction_add,
						UserId: suite.users.Krosh.UUID.String(),
					},
				},
			}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("invalid delta action", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.UpdateReviewersBody{
				ReviewersDelta: []*pbPub.ReviewerDelta{
					{
						Action: pbPub.DeltaAction_unspecified,
						UserId: suite.users.Kopatych.UUID.String(),
					},
					{
						Action: pbPub.DeltaAction_add,
						UserId: suite.users.Krosh.UUID.String(),
					},
				},
			}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(&pbPub.UpdateReviewersBody{
				ReviewersDelta: []*pbPub.ReviewerDelta{
					{
						Action: pbPub.DeltaAction_remove,
						UserId: "pipopapipuuu2",
					},
					{
						Action: pbPub.DeltaAction_add,
						UserId: suite.users.Krosh.UUID.String(),
					},
				},
			}).
			Post(fmt.Sprintf("/pulls/id:%s/reviewers", pr.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})
}
