package integrationtests

import (
	"common/cgit"
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestSuggestIssuesPr_TextSearch() {
	suite.RestoreOpensearch()
	t := suite.T()
	admin, barash, krosh := suite.users.Admin, suite.users.Barash, suite.users.Krosh
	repo := suite.repos.TreeDiff
	err := suite.IssueRepo.HardDeleteByRepoIDs(context.Background(), []uint64{repo.ID})
	require.NoError(t, err)
	ctx := testutils.AuthorizeGRPC(admin.Identity)
	suite.addRole(t, barash, repo, iam.Roles.RepositoriesContributor)
	suite.addRole(t, krosh, repo, iam.Roles.RepositoriesContributor)

	cg := suite.setupRepo(repo, krosh)
	prID := suite.createPr(repo, "feature", "Rocket since", "Rocket since", krosh, cg)
	createIssues(suite, repo.ID, krosh, barash)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	c := pb.NewSearchServiceClient(suite.grpcClient)

	testCases := []struct {
		name   string
		caller *entities.User
		query  string
	}{
		{
			name:   "query lake en by krosh",
			caller: krosh,
			query:  "lake",
		},
		{
			name:   "query lake ru by barash",
			caller: barash,
			query:  "озера",
		},
		{
			name:   "query lakes en by krosh",
			caller: krosh,
			query:  "lakes",
		},
		{
			name:   "query mars en by barash",
			caller: barash,
			query:  "mars",
		},
		{
			name:   "empty by krosh",
			caller: krosh,
			query:  "",
		},
		{
			name:   "empty by barash",
			caller: barash,
			query:  "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestIssuesPrRequest{
				Id:    grpc2.MarshalID(prID),
				Query: tc.query,
			}
			resp, err := c.SuggestIssuesPr(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//yarequire.ProtoDumpFixture(t, resp)
			// barash id: 6, krosh id: 2
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}
}

func (suite *RwApiTestSuite) TestSuggestIssuesPr_TextFallbackSearch() {
	t := suite.T()
	suite.OpensearchBackendProxy.MakeUnavaliable()
	barash, krosh := suite.users.Barash, suite.users.Krosh
	repo := suite.repos.TreeDiff
	err := suite.IssueRepo.HardDeleteByRepoIDs(context.Background(), []uint64{repo.ID})
	require.NoError(t, err)
	suite.addRole(t, barash, repo, iam.Roles.RepositoriesContributor)
	suite.addRole(t, krosh, repo, iam.Roles.RepositoriesContributor)

	cg := suite.setupRepo(repo, krosh)
	prID := suite.createPr(repo, "feature", "Rocket since", "Rocket since", krosh, cg)
	createIssuesForFallback(suite, repo.ID, krosh, barash)

	testCases := []struct {
		name   string
		caller *entities.User
		query  string
	}{
		{
			name:   "query by krosh",
			caller: krosh,
			query:  "Важн",
		},
		{
			name:   "query by barash",
			caller: barash,
			query:  "Важн",
		},
		{
			name:   "empty by krosh",
			caller: krosh,
			query:  "",
		},
		{
			name:   "empty by barash",
			caller: barash,
			query:  "",
		},
	}

	c := pb.NewSearchServiceClient(suite.grpcClient)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestIssuesPrRequest{
				Id:    grpc2.MarshalID(prID),
				Query: tc.query,
			}
			resp, err := c.SuggestIssuesPr(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//yarequire.ProtoDumpFixture(t, resp)
			// barash id: 6, krosh id: 2
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}

}

func (suite *RwApiTestSuite) TestSuggestIssuesPr_TestIssueSearchPermissions() {
	suite.RestoreOpensearch()
	t := suite.T()

	admin, barash, krosh := suite.users.Admin, suite.users.Barash, suite.users.Krosh
	repo := suite.repos.TreeDiff

	cg := suite.setupRepo(repo, admin)
	prID := suite.createPr(repo, "feature", "Rocket since", "Rocket since", admin, cg)

	suite.makeIssue(admin, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me",
		Description: "private admin issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Private,
	})

	suite.makeIssue(admin, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me too",
		Description: "public admin issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Public,
	})

	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me too",
		Description: "private barash issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Private,
	})

	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me too",
		Description: "public barash issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Public,
	})

	suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me too",
		Description: "private krosh issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Private,
	})

	suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Find me too",
		Description: "public krosh issue",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Public,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	c := pb.NewSearchServiceClient(suite.grpcClient)
	for _, user := range []*entities.User{admin, krosh, barash} {
		t.Run("suggest by "+user.Username, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(user.Identity)
			request := &pb.SuggestIssuesPrRequest{
				Id:    grpc2.MarshalID(prID),
				Query: "find",
			}
			resp, err := c.SuggestIssuesPr(ctx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//barash id: 6, krosh id: 2
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}

	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, user := range []*entities.User{admin, krosh, barash} {
		t.Run("suggest by "+user.Username+" fallback", func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(user.Identity)
			request := &pb.SuggestIssuesPrRequest{
				Id:    grpc2.MarshalID(prID),
				Query: "Find",
			}
			resp, err := c.SuggestIssuesPr(ctx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//barash id: 6, krosh id: 2
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}
}

func (suite *RwApiTestSuite) TestSuggestIssuesIssue_TextSearch() {
	t := suite.T()
	suite.RestoreOpensearch()
	admin, barash, krosh := suite.users.Admin, suite.users.Barash, suite.users.Krosh

	privateRepo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		Slug:       "private",
		Name:       "private",
		OrgID:      suite.orgs.Yandex.ID,
		Visibility: entities.Visibilities.Private,
	})
	createNotVisibleIssues := func() {
		for range 10 {
			suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
				RepoID:     privateRepo.ID,
				Title:      "The lake issue",
				Priority:   entities.IssuePriorities.Normal,
				AssigneeID: utils.PtrFromValue(suite.users.Krosh.ID),
				Visibility: entities.IssueVisibilities.Public,
			})
		}
	}

	createNotVisibleIssues()

	repo := suite.repos.TreeDiff
	createIssues(suite, repo.ID, krosh, barash)

	createNotVisibleIssues()

	repo2 := suite.repos.Alpha
	createIssues(suite, repo2.ID, krosh, barash)

	createNotVisibleIssues()

	targetIssue := suite.makeIssue(admin, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Task for search",
		Description: "Simple task no explanation",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Public,
	})

	excludeIssue := suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "The lake issue\nОзерный тикет",
		Description: "The ocean to\nВозможно океан",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	testCases := []struct {
		name   string
		caller *entities.User
		query  string
		repoID *uint64
	}{
		{
			name:   "query lake en by krosh",
			caller: krosh,
			query:  "lake",
			repoID: &repo.ID,
		},
		{
			name:   "query lake ru by barash",
			caller: barash,
			query:  "озера",
			repoID: &repo.ID,
		},
		{
			name:   "empty by krosh",
			caller: krosh,
			query:  "",
			repoID: &repo.ID,
		},
		{
			name:   "query lake en by krosh, from all org repos",
			caller: krosh,
			query:  "lake",
		},
		{
			name:   "query lake ru by barash, from all org repos",
			caller: barash,
			query:  "озера",
		},
		{
			name:   "empty by krosh, from all org repos",
			caller: krosh,
			query:  "",
		},
	}

	c := pb.NewSearchServiceClient(suite.grpcClient)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestIssuesIssueRequest{
				Id:              grpc2.MarshalID(targetIssue.ID),
				Query:           tc.query,
				ExcludeIssueIds: []string{grpc2.MarshalID(excludeIssue.ID)},
			}
			if tc.repoID != nil {
				request.RepoId = utils.PtrFromValue(grpc2.MarshalID(*tc.repoID))
			}
			resp, err := c.SuggestIssuesIssue(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			////barash id: 6, krosh id: 2
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}
}

func (suite *RwApiTestSuite) TestSuggestIssuesIssue_TextFallbackSearch() {
	t := suite.T()
	suite.OpensearchBackendProxy.MakeUnavaliable()
	suite.cfg.OpenSearch.Enabled = false
	admin, barash, krosh := suite.users.Admin, suite.users.Barash, suite.users.Krosh

	privateRepo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		Slug:       "private",
		Name:       "private",
		OrgID:      suite.orgs.Yandex.ID,
		Visibility: entities.Visibilities.Private,
	})
	createNotVisibleIssues := func() {
		for range 200 {
			suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
				RepoID:     privateRepo.ID,
				Title:      "The lake issue",
				Priority:   entities.IssuePriorities.Normal,
				AssigneeID: utils.PtrFromValue(suite.users.Krosh.ID),
				Visibility: entities.IssueVisibilities.Public,
			})
		}
	}

	createNotVisibleIssues()

	repo := suite.repos.TreeDiff
	createIssuesForFallback(suite, repo.ID, krosh, barash)

	createNotVisibleIssues()

	repo2 := suite.repos.Alpha
	createIssuesForFallback(suite, repo2.ID, krosh, barash)

	createNotVisibleIssues()

	suite.cfg.OpenSearch.Enabled = true

	targetIssue := suite.makeIssue(admin, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Task",
		Description: "Simple task no explanation",
		Priority:    entities.IssuePriorities.Normal,
		Visibility:  entities.IssueVisibilities.Public,
	})

	excludeIssue := suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repo.ID,
		Title:       "Важный заголовок",
		Description: "Какое-то описание",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})

	testCases := []struct {
		name   string
		caller *entities.User
		query  string
		repoID *uint64
	}{
		{
			name:   "query by krosh",
			caller: krosh,
			query:  "Важн",
			repoID: &repo.ID,
		},
		{
			name:   "query by barash",
			caller: barash,
			query:  "Важн",
			repoID: &repo.ID,
		},
		{
			name:   "empty by krosh",
			caller: krosh,
			query:  "",
			repoID: &repo.ID,
		},
		{
			name:   "query by krosh, from all org repos",
			caller: krosh,
			query:  "Важн",
		},
		{
			name:   "query by barash, from all org repos",
			caller: barash,
			query:  "Важн",
		},
		{
			name:   "empty by krosh, from all org repos",
			caller: krosh,
			query:  "",
		},
	}

	c := pb.NewSearchServiceClient(suite.grpcClient)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestIssuesIssueRequest{
				Id:              grpc2.MarshalID(targetIssue.ID),
				Query:           tc.query,
				ExcludeIssueIds: []string{grpc2.MarshalID(excludeIssue.ID)},
			}
			if tc.repoID != nil {
				request.RepoId = utils.PtrFromValue(grpc2.MarshalID(*tc.repoID))
			}
			resp, err := c.SuggestIssuesIssue(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)
			//barash id: 6, krosh id: 2
			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueSuggest{}, "id", "public_id", "repo_id"))
		})
	}
}

func (suite *RwApiTestSuite) setupRepo(repo *entities.Repository, user *entities.User) cgit.CGit {
	t := suite.T()

	cg, tmpDir := suite.initCGit(user, repo.FullSlug())
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

	cg.Must(t, "checkout", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, user.Username), "content")
	cg.Must(t, "push")

	return cg
}

func (suite *RwApiTestSuite) createPr(repo *entities.Repository, branch string, title string, desc string, user *entities.User,
	cg cgit.CGit) (prID uint64) {
	t := suite.T()
	tmpDir := cg.Path()[:strings.LastIndex(cg.Path(), "/")]

	cg.Must(t, "checkout", "-b", branch)
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "Venus.java"), "content")
	cg.Must(t, "push", "--set-upstream", "origin", branch)

	ctx := testutils.AuthorizeGRPC(user.Identity)
	p := pb.NewPRServiceClient(suite.grpcClient)
	op, err := p.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:      grpc2.MarshalID(repo.ID),
		Title:       title,
		Description: desc,
		Source:      branch,
		Target:      "main",
		ReviewerIds: []string{grpc2.MarshalID(suite.users.Slowpoke.ID), grpc2.MarshalID(suite.users.Raichu.ID)},
	})

	cg.Must(t, "checkout", "main")
	require.NoError(t, err)
	pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)

	prID, err = grpc_marshalling.IDDirect(pr.Id)
	require.NoError(t, err)
	return
}

func createIssues(suite *RwApiTestSuite,
	repoID uint64, krosh *entities.User, barash *entities.User) {

	suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "The lake issue\nОзерный тикет",
		Description: "The ocean to\nВозможно океан",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Венера\nVenus",
		Description: "Не знаю, есть ли озёра на Венера\nThere are no lakes on Venus",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(krosh.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Что-то про марсианские озера\nSmth about lakes on Mars",
		Description: "Год на марсе в 2 раза дольше\nThe year on a red planet is two times of the Earth's year",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Важная задача\nThe important task",
		Description: "Эта задача очень важная\nThe task is very important",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(krosh.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Не самая интересная задача\nThe boring task",
		Description: "Долететь до Юпитера\nReach Jupiter",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(krosh.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
}

func createIssuesForFallback(suite *RwApiTestSuite,
	repoID uint64, krosh *entities.User, barash *entities.User) {

	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Важный заголовок",
		Description: "Какое-то описание",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(krosh, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Венера",
		Description: "Важное описание",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(krosh.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
	suite.makeIssue(barash, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Простой или сложный тикет",
		Description: "Так сразу и не скажешь",
		Priority:    entities.IssuePriorities.Normal,
		AssigneeID:  utils.PtrFromValue(barash.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})
}
