package integrationtests

import (
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestSuggestPrsIssue_TextSearch() {
	suite.RestoreOpensearch()
	t := suite.T()
	repo := suite.repos.TreeDiff

	barash, krosh := suite.users.Barash, suite.users.Krosh
	issue := suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Some Public Issue",
		Description: "Desc",
		Priority:    entities.IssuePriorities.Normal,
		Status:      entities.IssueStatuses.Open,
		AssigneeID:  &krosh.ID,
		Visibility:  entities.IssueVisibilities.Public,
	})

	createPrs(suite, repo, krosh, barash)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	c := pb.NewSearchServiceClient(suite.grpcClient)
	testCases := []struct {
		name   string
		caller *entities.User
		query  string
	}{
		{
			name:   "query bloom en by krosh",
			caller: krosh,
			query:  "bloom",
		},
		{
			name:   "query bloom ru by barash",
			caller: barash,
			query:  "цветущий",
		},
		{
			name:   "query winds ru by krosh",
			caller: barash,
			query:  "winds",
		},
		{
			name:   "query abrakadabra by barash",
			caller: barash,
			query:  "abrakadabra",
		},
		{
			name:   "empty by krosh",
			caller: krosh,
			query:  "",
		},
		{
			name:   "prefix col by krosh",
			caller: krosh,
			query:  "Col",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestPrsIssueRequest{
				Id:    grpc2.MarshalID(issue.ID),
				Query: tc.query,
			}
			resp, err := c.SuggestPrsIssue(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//barash id: 6, krosh id: 2
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.PullRequestSuggest{}, "id", "public_id", "repo_id"))
		})
	}
}

func (suite *RwApiTestSuite) TestSuggestPrsIssue_SuggestPrivateIssues() {
	suite.RestoreOpensearch()
	t := suite.T()
	repo := suite.repos.TreeDiff

	admin, barash := suite.users.Admin, suite.users.Barash

	issue := suite.makeIssue(admin, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Some Private Issue",
		Description: "Desc",
		Priority:    entities.IssuePriorities.Normal,
		Status:      entities.IssueStatuses.Open,
		AssigneeID:  &admin.ID,
		Visibility:  entities.IssueVisibilities.Private,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	c := pb.NewSearchServiceClient(suite.grpcClient)

	t.Run("admin suggest no error", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(admin.Identity)
		request := &pb.SuggestPrsIssueRequest{
			Id:    grpc2.MarshalID(issue.ID),
			Query: "",
		}
		_, err := c.SuggestPrsIssue(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})

	t.Run("barash suggest no access", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(barash.Identity)
		request := &pb.SuggestPrsIssueRequest{
			Id:    grpc2.MarshalID(issue.ID),
			Query: "",
		}
		_, err := c.SuggestPrsIssue(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

}

func (suite *RwApiTestSuite) TestSuggestPrsIssue_Fallback() {
	t := suite.T()
	repo := suite.repos.TreeDiff
	suite.OpensearchBackendProxy.MakeUnavaliable()

	barash, krosh := suite.users.Barash, suite.users.Krosh
	issue := suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Some Public Issue",
		Description: "Desc",
		Priority:    entities.IssuePriorities.Normal,
		Status:      entities.IssueStatuses.Open,
		AssigneeID:  &krosh.ID,
		Visibility:  entities.IssueVisibilities.Public,
	})

	createPrs(suite, repo, krosh, barash)
	c := pb.NewSearchServiceClient(suite.grpcClient)
	t.Run("suggest with fallback", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(barash.Identity)
		request := &pb.SuggestPrsIssueRequest{
			Id:    grpc2.MarshalID(issue.ID),
			Query: "blooming",
		}
		resp, err := c.SuggestPrsIssue(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		//yarequire.ProtoDumpFixture(t, resp)
		//barash id: 6, krosh id: 2
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.PullRequestSuggest{}, "id", "public_id", "repo_id"))
	})
	t.Run("suggest all with fallback", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(barash.Identity)
		request := &pb.SuggestPrsIssueRequest{
			Id:    grpc2.MarshalID(issue.ID),
			Query: "",
		}
		resp, err := c.SuggestPrsIssue(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		//yarequire.ProtoDumpFixture(t, resp)
		//barash id: 6, krosh id: 2
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.PullRequestSuggest{}, "id", "public_id", "repo_id"))
	})
}

func createPrs(suite *RwApiTestSuite, repo *entities.Repository, barash *entities.User, krosh *entities.User) {
	barashCg := suite.setupRepo(repo, barash)
	kroshCg := suite.setupRepo(repo, krosh)

	suite.createPr(repo, "feature-1", "blooming garden\nЦветущий сад", "bright flowers\nЯркие цветы",
		barash, barashCg)
	suite.createPr(repo, "feature-2", "Spring is coming\nВесна близко", "blooming tree\nЦветущее дерево", krosh,
		kroshCg)
	suite.createPr(repo, "feature-3", "Cold wind\nХодолный ветер", "Close friend\nБлизкий друг",
		barash, barashCg)
	suite.createPr(repo, "feature-4", "Simple pr\nПросто пр", "Here is a simple code\nТут простой код какой-то",
		krosh, kroshCg)
}
