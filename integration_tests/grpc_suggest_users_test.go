package integrationtests

import (
	"context"
	"fmt"
	userservice "gitcore/internal/services/user"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"

	grpc2 "common/grpc"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/consts"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) prepareSuggestUsersTest() (repoID uint64, orgID uint64, prID uint64, issueID uint64) {
	t := suite.T()

	org := suite.orgs.Yandex
	repo := suite.repos.TreeDiff

	// add users to org, expecting org members: Admin, Raichu, BiBiPublic
	for _, user := range []*entities.User{suite.users.Raichu, suite.users.BiBiPublic} {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, org.Identity)
		require.NoError(t, err)
	}

	// add repo contributors, expecting contributors: Admin, Kopatych, Barash, Slowpoke
	for _, user := range []*entities.User{suite.users.Barash, suite.users.Slowpoke, suite.users.Kopatych} {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, org.Identity)
		require.NoError(t, err)
		suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
		cg, tmpDir := suite.initCGit(user, repo.FullSlug())
		cg.Must(t, "checkout", "main")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, user.Username), "content")
		cg.Must(t, "push")
	}

	// create feature branch with changes in dir1 and dir3
	cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
	cg.Must(t, "checkout", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "dir2", ".xx"), "content")
	cg.Must(t, "push", "origin", "HEAD")

	cg.Must(t, "checkout", "-b", "feature", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "new.txt"), "content")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir3", "new.txt"), "content")
	cg.Must(t, "push", "origin", "HEAD")

	// create feature2 branch with changes in dir1 and dir3
	cg.Must(t, "checkout", "-b", "feature2", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "new.txt"), "content2")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir3", "new.txt"), "content2")
	cg.Must(t, "push", "origin", "HEAD")

	// create feature3 branch with changes in dir1/dir2
	cg.Must(t, "checkout", "-b", "feature3", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "dir2", "new.txt"), "content")
	cg.Must(t, "push", "origin", "HEAD")

	// create feature4 branch with deleted in dir1/dir2
	cg.Must(t, "checkout", "-b", "feature4", "main")
	cg.Must(t, "rm", "dir1/dir2", "-r")
	cg.Must(t, "commit", "-m", "removed dir1/dir2")
	cg.Must(t, "push", "origin", "HEAD")

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)

	// create pr
	p := pb.NewPRServiceClient(suite.grpcClient)
	op, err := p.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:      grpc2.MarshalID(repo.ID),
		Title:       "feature2 to main",
		Source:      "feature2",
		Target:      "main",
		ReviewerIds: []string{grpc2.MarshalID(suite.users.Slowpoke.ID), grpc2.MarshalID(suite.users.Raichu.ID)},
		Publish:     true,
	})
	require.NoError(t, err)
	pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)
	prID, err = grpc_marshalling.IDDirect(pr.Id)
	require.NoError(t, err)

	// create issue
	c := pb.NewIssueServiceClient(suite.grpcClient)
	op, err = c.Create(ctx, &pb.CreateIssueRequest{
		RepoId:     grpc2.MarshalID(repo.ID),
		Title:      "some issue",
		AssigneeId: utils.PtrFromValue(grpc2.MarshalID(suite.users.Slowpoke.ID)),
		Visibility: pb.Issue_VISIBILITY_PUBLIC,
	})
	require.NoError(t, err)
	issue, err := grpc_marshalling.OperationResponse(op, &pb.Issue{})
	require.NoError(t, err)
	issueID, err = grpc_marshalling.IDDirect(issue.Id)
	require.NoError(t, err)

	// force merge requires maintainer role
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesMaintainer)

	// merge pr, robot-merger is registered as user, it should not appear in suggests
	_, err = p.Merge(ctx, &pb.MergeRequest{
		PrId:  grpc2.MarshalID(prID),
		Force: true,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	// create main copy without codereview rules
	cg.Must(t, "checkout", "main")
	cg.Must(t, "pull", "origin", "main")
	cg.Must(t, "checkout", "-b", "main-without-rules")
	cg.Must(t, "push", "--set-upstream", "origin", "main-without-rules")

	// add codereview rules into main
	cg.Must(t, "checkout", "main")

	commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.ReviewPath), fmt.Sprintf(`
codereview:
  auto_assign: false
  need_ships: 1
  rules:
    - patterns:
        - "dir1/**"
        - "!dir1/dir2/**"
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 1
    - patterns:
        - "dir3/**"
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 1
    - patterns:
        - "dir4/**"
      reviewers:
        usernames:
          - "%s"
        assign: 1
    - patterns:
        - "dir1/dir2/**"
      reviewers:
        usernames:
          - "%s"
        assign: 1
`, suite.users.Krosh.Username, suite.users.Barash.Username, suite.users.Barash.Username, suite.users.Kopatych.Username, suite.users.Pikachu.Username, suite.users.BiBiPublic.Username))
	cg.Must(t, "push")

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	suite.OpensearchKit.RefreshIndex(context.Background())

	return repo.ID, org.ID, prID, issueID
}

func (suite *RwApiTestSuite) TestUserIndexing() {
	suite.RestoreOpensearch()

	t := suite.T()

	identity := entities.UserIdentity{
		ID:  "user123",
		Src: entities.IdentityProviders.IAM,
	}

	var user *entities.User
	var err error

	t.Run("create user - not indexed yet", func(t *testing.T) {
		user, err = suite.UserService.CreateUser(
			context.Background(),
			testutils.NewStubAuthenticator(&identity),
			interfaces.UserCreateArgs{Identity: identity},
			false,
		)
		require.NoError(t, err)
		err = suite.UserService.CreatePersonalOrg(context.Background(), user, testutils.NewStubAuthenticator(&identity), true)
		require.NoError(t, err)

		require.Equal(t, "user123", user.Username)
		require.Equal(t, "User123", user.DisplayName)
		require.Equal(t, "user123@ya.ru", user.Email)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		suite.OpensearchKit.RefreshIndex(context.Background())

		suite.WaitForWorkflows(suite.T(), userservice.CreatePersonalOrgWorkflowType)

		hits, err := suite.UserSearchManager.Suggest(context.Background(), "user123", nil, false, 10)
		require.NoError(t, err)
		require.Equal(t, 0, len(hits))
	})

	t.Run("onboard user - should be indexed", func(t *testing.T) {
		_, err := suite.UserService.SetFlag(context.Background(), user, entities.UserFlags.Onboarded, true)
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)

		suite.OpensearchKit.RefreshIndex(context.Background())

		hits, err := suite.UserSearchManager.Suggest(context.Background(), user.Username, nil, false, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(hits))
	})

	t.Run("update public name - should be reindexed", func(t *testing.T) {
		err := suite.UserService.UpdateUser(context.Background(), user, interfaces.UserUpdateArgs{
			DisplayName: utils.PtrFromValue("Ivan Ivanov"),
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)

		suite.OpensearchKit.RefreshIndex(context.Background())

		user, err = suite.UserRepo.GetUserByID(context.Background(), user.ID)
		require.NoError(t, err)

		hits, err := suite.UserSearchManager.Suggest(context.Background(), user.DisplayName, nil, false, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(hits))
	})

	t.Run("update username - should be reindexed", func(t *testing.T) {
		err := suite.UserService.UpdateUsername(context.Background(), user, testutils.NewStubAuthenticator(&identity), "vanyaaa543")
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)

		suite.OpensearchKit.RefreshIndex(context.Background())

		user, err = suite.UserRepo.GetUserByID(context.Background(), user.ID)
		require.NoError(t, err)

		hits, err := suite.UserSearchManager.Suggest(context.Background(), user.Username, nil, false, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(hits))
	})
}

func (suite *RwApiTestSuite) TestSearchServiceHandler_SuggestUsers() {
	suite.RestoreOpensearch()

	t := suite.T()

	suite.prepareSuggestUsersTest()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewSearchServiceClient(suite.grpcClient)

	type testcase struct {
		name  string
		query string
	}

	testcases := []testcase{
		{
			name:  "query empty",
			query: "",
		},
		{
			name:  "query sa",
			query: consts.ServiceAccountPublicName,
		},
		{
			name:  "query patych",
			query: "patych",
		},
		{
			name:  "query notfound",
			query: "notfound",
		},
	}

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestUsersRequest{
				Query: tc.query,
			}
			resp, err := c.SuggestUsers(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "id", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}

func (suite *RwApiTestSuite) TestSearchServiceHandler_SuggestUsersOrg() {
	suite.RestoreOpensearch()

	t := suite.T()

	_, orgID, _, _ := suite.prepareSuggestUsersTest()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewSearchServiceClient(suite.grpcClient)

	type testcase struct {
		name  string
		query string
		orgID uint64
		code  codes.Code
	}
	testcases := []testcase{
		{
			name:  "query empty",
			query: "",
			orgID: orgID,
		},
		{
			name:  "query bibi",
			query: "bibi",
			orgID: orgID,
		},
		{
			name:  "query no-one",
			query: "no-one",
			orgID: orgID,
		},
		{
			name:  "org not found",
			query: "",
			orgID: 123,
			code:  codes.NotFound,
		},
	}

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestUsersOrgRequest{
				Id:    grpc2.MarshalID(tc.orgID),
				Query: tc.query,
			}
			resp, err := c.SuggestUsersOrg(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}

func (suite *RwApiTestSuite) TestSearchServiceHandler_SuggestUsersRepo() {
	suite.RestoreOpensearch()

	t := suite.T()

	repoID, _, _, _ := suite.prepareSuggestUsersTest()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewSearchServiceClient(suite.grpcClient)

	type testcase struct {
		name   string
		repoID uint64
		query  string
		code   codes.Code
	}
	testcases := []testcase{
		{
			name:   "query empty",
			repoID: repoID,
			query:  "",
		},
		{
			name:   "patych",
			repoID: repoID,
			query:  "patych",
		},
		{
			name:   "query no-one",
			repoID: repoID,
			query:  "no-one",
		},
		{
			name:   "query bibi",
			query:  "bibi",
			repoID: repoID,
		},
		{
			name:   "repo not found",
			query:  "",
			repoID: 123,
			code:   codes.NotFound,
		},
	}

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestUsersRepoRequest{
				Id:    grpc2.MarshalID(tc.repoID),
				Query: tc.query,
			}
			resp, err := c.SuggestUsersRepo(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}

func (suite *RwApiTestSuite) TestSearchServiceHandler_SuggestUsersPR() {
	suite.RestoreOpensearch()

	t := suite.T()

	_, _, prID, _ := suite.prepareSuggestUsersTest()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewSearchServiceClient(suite.grpcClient)

	type testcase struct {
		name  string
		prID  uint64
		query string
		code  codes.Code
	}
	testcases := []testcase{
		{
			name:  "query empty",
			prID:  prID,
			query: "",
		},
		{
			name:  "query patych",
			prID:  prID,
			query: "patych",
		},
		{
			name:  "query no-one",
			prID:  prID,
			query: "no-one",
		},
		{
			name:  "query bibi",
			query: "bibi",
			prID:  prID,
		},
		{
			name:  "pr not found",
			query: "",
			prID:  123,
			code:  codes.NotFound,
		},
	}

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestUsersPRRequest{
				Id:    grpc2.MarshalID(tc.prID),
				Query: tc.query,
			}
			resp, err := c.SuggestUsersPR(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}

func (suite *RwApiTestSuite) TestSearchServiceHandler_SuggestUsersIssue() {
	suite.RestoreOpensearch()

	t := suite.T()

	_, _, _, issueID := suite.prepareSuggestUsersTest()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewSearchServiceClient(suite.grpcClient)

	type testcase struct {
		name    string
		issueID uint64
		query   string
		code    codes.Code
	}
	testcases := []testcase{
		{
			name:    "query empty",
			issueID: issueID,
			query:   "",
		},
		{
			name:    "query patych",
			issueID: issueID,
			query:   "patych",
		},
		{
			name:    "query no-one",
			issueID: issueID,
			query:   "no-one",
		},
		{
			name:    "query bibi",
			query:   "bibi",
			issueID: issueID,
		},
		{
			name:    "issue not found",
			query:   "",
			issueID: 123,
			code:    codes.NotFound,
		},
	}

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestUsersIssueRequest{
				Id:    grpc2.MarshalID(tc.issueID),
				Query: tc.query,
			}
			resp, err := c.SuggestUsersIssue(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}
