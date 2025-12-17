package integrationtests

import (
	"common/cgit"
	"common/functools"
	grpc2 "common/grpc"
	commongrpc "common/grpc/exceptions"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"os"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestPR_Validation() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	type testcase struct {
		name     string
		prID     string
		wantCode codes.Code
	}
	tests := []testcase{
		{
			name:     "invalid pr id",
			prID:     "x",
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "pr not found",
			prID:     "123",
			wantCode: codes.NotFound,
		},
		{
			name:     "ok",
			prID:     grpc2.MarshalID(pr.ID),
			wantCode: codes.OK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{PrId: test.prID})
			yarequire.ProtoStatusEqual(t, test.wantCode, err)
		})
	}
	t.Run("wrong slugs", func(t *testing.T) {
		_, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
			Parents: &pb.RepositoryFullSlug{
				OrgSlug:  "wrong",
				RepoSlug: "slug",
			},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}

func (suite *RwApiTestSuite) TestPR_Get() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	testcases := []struct {
		name      string
		user      *entities.User
		opts      *makePrOptions
		request   *pb.GetPullRequestRequest
		code      codes.Code
		provision func(pr *entities.PullRequest) error
	}{
		{
			name:    "happy path",
			user:    suite.users.Kopatych,
			request: &pb.GetPullRequestRequest{},
			opts: &makePrOptions{
				Repo:      suite.repos.Alpha,
				Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
			},
		},
		{
			name: "happy path 2",
			user: suite.users.Kopatych,
			request: &pb.GetPullRequestRequest{
				Parents: &pb.RepositoryFullSlug{
					OrgSlug:  suite.repos.Alpha.OrgSlug,
					RepoSlug: suite.repos.Alpha.Slug,
				},
			},
			opts: &makePrOptions{
				Repo:      suite.repos.Alpha,
				Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
			},
		},
		{
			name: "wrong parent",
			user: suite.users.Kopatych,
			request: &pb.GetPullRequestRequest{
				Parents: &pb.RepositoryFullSlug{
					OrgSlug:  suite.repos.BigDiff.OrgSlug,
					RepoSlug: suite.repos.BigDiff.Slug,
				},
			},
			code: codes.NotFound,
			opts: &makePrOptions{
				Repo:      suite.repos.Alpha,
				Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
			},
		},
		{
			name:    "complex",
			user:    suite.users.Kopatych,
			request: &pb.GetPullRequestRequest{},
			provision: func(pr *entities.PullRequest) error {
				opID, err := suite.OpRepo.Create(ctx, &entities.Operation{
					ID:        "aaaaaaaaaaaaaaaaaaaa",
					Type:      entities.OperationTypes.Stub,
					Status:    entities.OperationStatuses.Scheduled,
					UserID:    suite.users.Kopatych.ID,
					IamObject: suite.repos.Alpha.Object(),
				})
				if err != nil {
					return err
				}
				err = suite.PullRequestService.SetDecision(
					ctx,
					testutils.NewStubAuthenticator(&suite.users.Pikachu.Identity),
					pr,
					suite.users.Pikachu,
					&entities.PullRequestDecisions.StickyShip,
					entities.NotifyOptions{},
				)
				if err != nil {
					return err
				}
				err = suite.PullRequestService.SetDecision(
					ctx,
					testutils.NewStubAuthenticator(&suite.users.Slowpoke.Identity),
					pr,
					suite.users.Slowpoke,
					&entities.PullRequestDecisions.Abstain,
					entities.NotifyOptions{},
				)
				if err != nil {
					return err
				}
				err = suite.PullRequestService.SetDecision(
					ctx,
					testutils.NewStubAuthenticator(&suite.users.Kopatych.Identity),
					pr,
					suite.users.Kopatych,
					&entities.PullRequestDecisions.Block,
					entities.NotifyOptions{},
				)
				if err != nil {
					return err
				}

				targetHash := plumbing.NewHash("baaaaaaaaa99e97462daa9dc7bf4dbd40545fdb9")
				mergeHash := plumbing.NewHash("aaaaaaaaaa99e97462daa9dc7bf4dbd40545fdb9")

				return suite.PullRequestRepo.UpdateTable(ctx, pr.ID, map[string]any{
					"merge_operation_id":      opID,
					"status":                  entities.PRStatuses.Merging,
					"merge_merger_id":         suite.users.Slowpoke.ID,
					"merge_merge_commit_sha":  mergeHash[:],
					"merge_target_commit_sha": targetHash[:],
					"merge_params": entities.MergeParams{
						Rebase:       true,
						Squash:       true,
						DeleteBranch: false,
					},
				})
			},
			opts: &makePrOptions{
				Repo:      suite.repos.Alpha,
				Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pr := suite.makePullRequest(tc.user, tc.opts)
			if tc.provision != nil {
				require.NoError(t, tc.provision(pr))
			}
			tc.request.Identity = &pb.GetPullRequestRequest_Id{Id: grpc2.MarshalID(pr.ID)}

			res, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)

			if tc.code != codes.OK {
				return
			}

			tc.request.Identity = &pb.GetPullRequestRequest_PublicId{PublicId: grpc2.MarshalID(pr.PublicID)}
			tc.request.Parents = &pb.RepositoryFullSlug{
				OrgSlug:  suite.repos.Alpha.OrgSlug,
				RepoSlug: suite.repos.Alpha.Slug,
			}
			res2, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)

			// yarequire.ProtoDumpFixture(t, res)
			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.PullRequest{}, "id", "uuid", "iteration", "created_at", "updated_at", "merge_info"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"))

			yarequire.ProtoCompareWithFixture(t, res2,
				protocmp.IgnoreFields(&pb.PullRequest{}, "id", "uuid", "iteration", "created_at", "updated_at", "merge_info"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestPR_GetBulk() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	testcases := []struct {
		name string
		user *entities.User
		opts []*makePrOptions
		code codes.Code
	}{
		{
			name: "empty",
			user: suite.users.Kopatych,
			code: codes.InvalidArgument,
		},
		{
			name: "happy path 2",
			user: suite.users.Kopatych,
			opts: []*makePrOptions{
				{
					Repo:      suite.repos.Alpha,
					Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
				},
			},
		},
		{
			name: "two repos",
			user: suite.users.Kopatych,
			opts: []*makePrOptions{
				{
					Repo:      suite.repos.Alpha,
					Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
				},
				{
					Repo:      suite.repos.Alpha,
					Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID, suite.users.Slowpoke.ID},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			request := &pb.GetBulkPullRequestsRequest{}

			for _, opt := range tc.opts {
				pr := suite.makePullRequest(tc.user, opt)
				ID := grpc2.MarshalID(pr.ID)

				request.Ids = append(request.Ids, ID)
			}

			res, err := client.GetBulk(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)

			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, res)
			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.PullRequest{}, "id", "uuid", "iteration", "created_at", "updated_at", "merge_info"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestPR_Create() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	reviewers := []*entities.User{suite.users.Krosh, suite.users.Pikachu, suite.users.Slowpoke}

	testcases := []struct {
		name       string
		user       *entities.User
		request    *pb.CreatePullRequestRequest
		wantStatus entities.PRStatus
	}{
		{
			name: "draft no description",
			user: suite.users.Kopatych,
			request: &pb.CreatePullRequestRequest{
				Description:         "",
				Publish:             false,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
			},
			wantStatus: entities.PRStatuses.Draft,
		},
		{
			name: "draft with description",
			user: suite.users.Kopatych,
			request: &pb.CreatePullRequestRequest{
				Description:         "added readme",
				Publish:             false,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
			},
			wantStatus: entities.PRStatuses.Draft,
		},
		{
			name: "publish with description",
			user: suite.users.Kopatych,
			request: &pb.CreatePullRequestRequest{
				Description:         "added readme",
				Publish:             true,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
			},
			wantStatus: entities.PRStatuses.Open,
		},
		{
			name: "publish with reviewers",
			user: suite.users.Kopatych,
			request: &pb.CreatePullRequestRequest{
				Description:         "added readme",
				Publish:             true,
				NotificationOptions: testutils.SkipNotificationPb,
				ReviewerIds: functools.Map(reviewers, func(usr *entities.User) string {
					return grpc2.MarshalID(usr.ID)
				}),
			},
			wantStatus: entities.PRStatuses.Open,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			tc.request.RepoId = grpc2.MarshalID(suite.repos.Alpha.ID)
			tc.request.Title = "TASK-1 add README.md"
			tc.request.Source = "branch"
			tc.request.Target = "master"

			res, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)
			meta := testutils.UnmarshalGrpcMetadata[*pb.CreatePullRequestMetadata](t, res)

			require.Equal(t, tc.request.RepoId, meta.RepoId)
			pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, res)

			// yarequire.ProtoDumpFixture(t, pr)
			yarequire.ProtoCompareWithFixture(t, pr,
				protocmp.IgnoreFields(&pb.PullRequest{}, "id", "uuid", "iteration", "created_at", "updated_at", "merge_info"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestPR_CannotCreateWithoutRole() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := context.Background()

	request := &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
		Title:               "TASK-1 add README.md",
		Source:              "branch",
		Target:              "master",
		Publish:             false,
		ReviewerIds:         []string{},
		NotificationOptions: testutils.SkipNotificationPb,
	}

	_, err := client.Create(ctx, request)
	yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
}

func (suite *RwApiTestSuite) TestPR_CreateNoSuchBranch() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	testcases := []struct {
		Name   string
		Source string
		Target string
	}{
		{
			Name:   "no source",
			Source: "blahblah",
			Target: "master",
		},
		{
			Name:   "no target",
			Source: "branch",
			Target: "heehee",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {

			request := &pb.CreatePullRequestRequest{
				RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
				Title:               "TASK-1 add README.md",
				Source:              tc.Source,
				Target:              tc.Target,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
			}

			_, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.NotFound, err)
		})
	}
}

func (suite *RwApiTestSuite) TestPR_FromOtherRepo() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	forkRepo := suite.ImportRepo(suite.orgs.Yandex, "alpha-fork-for-pr-test", testutils.BasicRepo, nil)

	tmpDir := testutils.TempDir(t, "", "")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Admin.Identity))
	cg.Must(t, "clone", suite.HTTPSProtocol().RepoURLByID(forkRepo.ID), ".")
	suite.makeNewFileWithContent(cg, "otherfile.txt", "some other content")
	suite.makeNewFileWithContent(cg, "CHANGELOG", "Initial changelog\nAnd some other content\nand other")
	cg.Must(t, "add", "otherfile.txt")
	cg.Must(t, "add", "CHANGELOG")
	cg.Must(t, "commit", "-m", "otherfile")
	cg.Must(t, "push", "-u", "origin", "master")

	testcases := []struct {
		Name         string
		Source       string
		SourceRepoID uint64
		Target       string
		Err          *commongrpc.ExceptionTemplate
	}{
		{
			Name:         "no_source",
			Source:       "blahblah",
			SourceRepoID: suite.repos.History.ID,
			Target:       "master",
			Err:          except.BranchNotFound,
		},
		{
			Name:         "unrelated_repo",
			Source:       "master",
			SourceRepoID: suite.repos.History.ID,
			Target:       "master",
			Err:          except.NoMergeBases,
		},
		{
			Name:         "related_repo_same_branch",
			Source:       "master",
			SourceRepoID: forkRepo.ID,
			Target:       "master",
			Err:          nil,
		},
		{
			Name:         "related_repo_another_branch",
			Source:       "master",
			SourceRepoID: forkRepo.ID,
			Target:       "branch",
			Err:          nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {
			request := &pb.CreatePullRequestRequest{
				RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
				Title:               "Test title",
				Source:              tc.Source,
				Target:              tc.Target,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
				ForkRepoId:          utils.PtrFromValue(grpc2.MarshalID(tc.SourceRepoID)),
			}

			createRes, err := client.Create(ctx, request)
			if tc.Err == nil {
				require.NoError(t, err)

				pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, createRes)
				prID := pr.Id

				// yarequire.ProtoDumpFixture(t, pr)
				yarequire.ProtoCompareWithFixture(t, pr,
					protocmp.IgnoreFields(&pb.PullRequest{}, "id", "uuid", "public_id", "fork_repo_id", "iteration", "created_at", "updated_at", "merge_info"), // random each time
					protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"))

				require.Equal(t, pr.ForkRepoId, grpc2.MarshalID(tc.SourceRepoID))

				list, err := client.ListUserPullRequests(ctx, &pb.ListUserPullRequestsRequest{
					UserId: grpc2.MarshalID(suite.users.Admin.ID),
					Role:   pb.ListUserPullRequestsRequest_ROLE_AUTHOR,
				})
				require.NoError(t, err)

				found := false
				for _, prr := range list.PullRequests {
					if prr.ForkRepoId == prr.RepoId {
						continue
					}

					srp, err := grpc_marshalling.IDDirect(prr.ForkRepoId)
					require.NoError(t, err)

					if srp == tc.SourceRepoID && prr.Id == prID {
						found = true
					}
				}
				require.True(t, found, "List should contain our PR")

				var paths []*pb.DiffPath
				resp, err := client.GetChanges(ctx, &pb.GetChangesRequest{
					PrId: prID,
				})
				require.NoError(t, err)

				paths = functools.Map(resp.TreeDiffs, func(td *pb.TreeDiff) *pb.DiffPath {
					return td.Paths
				})

				t.Run("get_changes", func(t *testing.T) {
					resp, err := client.GetChanges(ctx, &pb.GetChangesRequest{
						PrId: prID,
					})
					require.NoError(t, err)

					// yarequire.ProtoDumpFixture(t, resp)
					yarequire.ProtoCompareWithFixture(t, resp)
				})

				t.Run("get_file_changes", func(t *testing.T) {
					fileChangedResp, err := client.GetFileChanges(ctx, &pb.GetFileChangesRequest{
						PrId:  prID,
						Paths: paths,
					})
					require.NoError(t, err)

					// yarequire.ProtoDumpFixture(t, fileChangedResp)
					yarequire.ProtoCompareWithFixture(t, fileChangedResp)
				})

				t.Run("get_stats", func(t *testing.T) {
					stats, err := client.GetStats(ctx, &pb.GetStatsRequest{
						PrId: prID,
					})
					require.NoError(t, err)

					// yarequire.ProtoDumpFixture(t, stats)
					yarequire.ProtoCompareWithFixture(t, stats)
				})

				t.Run("create_comment", func(t *testing.T) {
					createCommentRes, err := commentClient.Create(ctx, &pb.CreateCommentRequest{
						PrId:    prID,
						Body:    "this is the best code I've ever seen",
						Publish: true,
						Anchor: &pb.CreateCommentRequest_ShortAnchor{
							Path: paths[0].Source,
							Position: &pb.DiffPos{
								From: 10,
								To:   12,
								Side: pb.DiffPos_TARGET,
							},
						},
					})
					require.NoError(t, err)

					comment := testutils.UnmarshalGrpcResult[*pb.PRComment](t, createCommentRes)

					// yarequire.ProtoDumpFixture(t, comment)
					yarequire.ProtoCompareWithFixture(t, comment,
						protocmp.IgnoreFields(&pb.PRComment{}, "id", "iteration", "created_at", "updated_at", "is_outdated"))
				})
			} else {
				yarequire.ProtoExceptionTemplate(t, err, tc.Err)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPrAndIssue_PublicID() {
	t := suite.T()

	pr1 := suite.makePullRequest(suite.users.Admin, &makePrOptions{Repo: suite.repos.Alpha})
	pr2 := suite.makePullRequest(suite.users.Admin, &makePrOptions{Repo: suite.repos.Alpha})

	pr3 := suite.makePullRequest(suite.users.Admin, &makePrOptions{Repo: suite.repos.BigDiff, Target: "main", Source: "feature"})
	pr4 := suite.makePullRequest(suite.users.Admin, &makePrOptions{Repo: suite.repos.BigDiff, Target: "main", Source: "feature"})

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{RepoID: suite.repos.Alpha.ID})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{RepoID: suite.repos.Alpha.ID})

	issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{RepoID: suite.repos.BigDiff.ID})
	issue4 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{RepoID: suite.repos.BigDiff.ID})

	require.Equal(t, uint64(1), pr1.PublicID)
	require.Equal(t, uint64(2), pr2.PublicID)
	require.Equal(t, uint64(1), pr3.PublicID)
	require.Equal(t, uint64(2), pr4.PublicID)

	require.Equal(t, uint64(1), issue1.PublicID)
	require.Equal(t, uint64(2), issue2.PublicID)
	require.Equal(t, uint64(1), issue3.PublicID)
	require.Equal(t, uint64(2), issue4.PublicID)
}

func (suite *RwApiTestSuite) TestPR_Invalidate() {
	t := suite.T()

	repo := suite.repos.Alpha
	user := suite.users.Admin
	ctx := testutils.AuthorizeGRPC(user.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

	t.Run("delete branch", func(t *testing.T) {
		pr := suite.makePullRequest(user, &makePrOptions{
			Repo:   repo,
			Source: "branch",
		})

		tmpDir := testutils.TempDir(t, "", "")
		w := testutils.NewWorkdir(t, tmpDir)
		cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
		cg.Must(t, "clone", suite.HTTPSProtocol().RepoURLByID(repo.ID), "repo")
		cg = cgit.NewCGit(w.ChildDir("repo").Path()).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
		cg.Must(t, "push", "-d", "origin", "branch")

		prNew, err := suite.PullRequestRepo.Get(ctx, pr.ID)
		require.NoError(t, err)
		require.Equal(t, entities.PRStatuses.Discarded, prNew.Status)

		t.Run("reopen failed", func(t *testing.T) {
			_, err := client.Reopen(ctx, &pb.ReopenRequest{
				Id: grpc2.MarshalID(pr.ID),
			})
			yarequire.ProtoStatusEqual(t, codes.NotFound, err)
		})

		t.Run("reopen success", func(t *testing.T) {
			suite.primitivePush(t, user, repo, plumbing.NewBranchReferenceName("branch"), true)
			_, err := client.Reopen(ctx, &pb.ReopenRequest{
				Id: grpc2.MarshalID(pr.ID),
			})
			require.NoError(t, err)
		})
	})

	t.Run("no merge bases", func(t *testing.T) {
		pr := suite.makePullRequest(user, &makePrOptions{
			Repo:   repo,
			Source: "branch",
		})

		suite.mustBash(repo, `
			git checkout master
			git checkout $(git rev-list --max-parents=0 HEAD | tail -n 1)
			git switch -c master2
			echo something > something.txt
			git commit --amend --no-edit
			git checkout branch
			git rebase master2
		`)

		prNew, err := suite.PullRequestRepo.Get(ctx, pr.ID)
		require.NoError(t, err)
		require.Equal(t, entities.PRStatuses.Discarded, prNew.Status)

		t.Run("reopen failed", func(t *testing.T) {
			_, err := client.Reopen(ctx, &pb.ReopenRequest{
				Id: grpc2.MarshalID(pr.ID),
			})
			yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		})

		t.Run("reopen success", func(t *testing.T) {
			suite.mustBash(repo, `
				git checkout branch
				git rebase master
			`)

			_, err := client.Reopen(ctx, &pb.ReopenRequest{
				Id: grpc2.MarshalID(pr.ID),
			})
			require.NoError(t, err)
		})
	})
}

func (suite *RwApiTestSuite) TestPR_Update() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)

	// Add necessary roles
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	// First create a PR that will be updated
	ctxKrosh := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	createReq := &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
		Title:               "TASK-1 add README.md",
		Source:              "branch",
		Target:              "master",
		NotificationOptions: testutils.SkipNotificationPb,
	}

	createRes, err := client.Create(ctxKrosh, createReq)
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	require.NoError(t, err)

	pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, createRes)
	prID := pr.Id

	testcases := []struct {
		name          string
		user          entities.UserIdentity
		request       *pb.UpdatePullRequestRequest
		expectedCode  codes.Code
		expectedTitle string
		expectedDesc  string
	}{
		{
			name: "BadRequest (bad pr_id)",
			user: suite.users.Krosh.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:    "bad_pr_id",
				Title: utils.PtrFromValue("New Title"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "Unsupported (not found target branch)",
			user: suite.users.Krosh.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:         prID,
				Title:      utils.PtrFromValue("New Title"),
				Target:     utils.PtrFromValue("nonexistent"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title", "target"}},
			},
			expectedCode: codes.Unimplemented,
		},
		{
			name: "NotFound (not found pull request)",
			user: suite.users.Krosh.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:    "999",
				Title: utils.PtrFromValue("New Title"),
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "OK",
			user: suite.users.Krosh.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:          prID,
				Title:       utils.PtrFromValue("New Title"),
				Description: utils.PtrFromValue("New Description"),
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title", "description"}},
			},
			expectedCode:  codes.OK,
			expectedTitle: "New Title",
			expectedDesc:  "New Description",
		},
		{
			name: "forbidden - change other user PR",
			user: suite.users.Kopatych.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:          prID,
				Title:       utils.PtrFromValue("Another Title"),
				Description: utils.PtrFromValue("Another Description"),
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "admin can change other user PR",
			user: suite.users.Admin.Identity,
			request: &pb.UpdatePullRequestRequest{
				Id:          prID,
				Title:       utils.PtrFromValue("Admin Title"),
				Description: utils.PtrFromValue("Admin Description"),
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title", "description"}},
			},
			expectedCode:  codes.OK,
			expectedTitle: "Admin Title",
			expectedDesc:  "Admin Description",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user)

			res, err := client.Update(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedCode, err)

			if tc.expectedCode == codes.OK {
				require.NoError(t, err)
				updatedPR := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, res)
				require.Equal(t, tc.expectedTitle, updatedPR.Title)
				require.Equal(t, tc.expectedDesc, updatedPR.Description)

				// Verify using Get as well
				getRes, err := client.Get(ctx, &pb.GetPullRequestRequest{
					Identity: &pb.GetPullRequestRequest_Id{Id: prID},
				})
				require.NoError(t, err)

				require.Equal(t, tc.expectedTitle, getRes.Title)
				require.Equal(t, tc.expectedDesc, getRes.Description)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPR_SwapPublicPRID() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)

	testcases := []struct {
		name           string
		user           *entities.User
		getRequestUser *entities.User
		opts           *makePrOptions
		request        func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest
		code           codes.Code
	}{
		{
			name:           "happy path",
			user:           suite.users.Kopatych,
			getRequestUser: suite.users.Kopatych,
			opts: &makePrOptions{
				Repo: suite.repos.Alpha,
			},
			request: func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest {
				return &pb.SwapPRUUIDToPrivateIDRequest{
					PullRequestUuid: grpc_marshalling.UUIDInverse(pr.UUID),
				}
			},
			code: codes.OK,
		},
		{
			name:           "invalid uuid format",
			user:           suite.users.Kopatych,
			getRequestUser: suite.users.Kopatych,
			opts: &makePrOptions{
				Repo: suite.repos.Alpha,
			},
			request: func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest {
				return &pb.SwapPRUUIDToPrivateIDRequest{
					PullRequestUuid: "invalid-uuid",
				}
			},
			code: codes.InvalidArgument,
		},
		{
			name:           "uuid not found",
			user:           suite.users.Kopatych,
			getRequestUser: suite.users.Kopatych,
			opts: &makePrOptions{
				Repo: suite.repos.Alpha,
			},
			request: func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest {
				return &pb.SwapPRUUIDToPrivateIDRequest{
					PullRequestUuid: "550e8400-e29b-41d4-a716-446655440000",
				}
			},
			code: codes.NotFound,
		},
		{
			name:           "permission denied - user without access",
			user:           suite.users.Kopatych,
			getRequestUser: suite.users.Slowpoke, // Use another user for permission denied test
			opts: &makePrOptions{
				Repo: suite.repos.AuthRepoPrivate, // Use private repo
			},
			request: func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest {
				return &pb.SwapPRUUIDToPrivateIDRequest{
					PullRequestUuid: grpc_marshalling.UUIDInverse(pr.UUID),
				}
			},
			code: codes.PermissionDenied,
		},
		{
			name:           "permission denied - user without access to forked repo",
			user:           suite.users.Kopatych,
			getRequestUser: suite.users.Slowpoke, // User without access to fork
			opts: &makePrOptions{
				Repo:       suite.repos.Alpha,
				SourceRepo: suite.repos.AuthRepoPrivate, // Private fork source repo
			},
			request: func(pr *entities.PullRequest) *pb.SwapPRUUIDToPrivateIDRequest {
				return &pb.SwapPRUUIDToPrivateIDRequest{
					PullRequestUuid: grpc_marshalling.UUIDInverse(pr.UUID),
				}
			},
			code: codes.PermissionDenied,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pr := suite.makePullRequest(tc.user, tc.opts)

			ctx := testutils.AuthorizeGRPC(tc.getRequestUser.Identity)

			request := tc.request(pr)
			res, err := client.SwapPRUUIDToPrivateID(ctx, request)
			yarequire.ProtoStatusEqual(t, tc.code, err)

			if tc.code == codes.OK {
				require.NoError(t, err)
				require.Equal(t, grpc2.MarshalID(pr.ID), res.PullRequestPrivateId)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPR_CreateWithAutoLinkedIssues() {
	t := suite.T()

	prClient := pb.NewPRServiceClient(suite.grpcClient)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	testcases := []struct {
		name         string
		numIssues    int
		buildTitle   func(issueIDs []string) string
		buildDesc    func(issueIDs []string) string
		expectLinked bool
	}{
		{
			name:         "issue ID in title with hash",
			numIssues:    1,
			buildTitle:   func(ids []string) string { return "Fix #" + ids[0] + " - important bug" },
			buildDesc:    func(ids []string) string { return "Description without issue ID" },
			expectLinked: true,
		},
		{
			name:         "issue ID at start of title",
			numIssues:    1,
			buildTitle:   func(ids []string) string { return ids[0] + " - Fix important bug" },
			buildDesc:    func(ids []string) string { return "Description without issue ID" },
			expectLinked: true,
		},
		{
			name:         "issue ID in description",
			numIssues:    1,
			buildTitle:   func(ids []string) string { return "Fix important bug" },
			buildDesc:    func(ids []string) string { return "This PR fixes #" + ids[0] },
			expectLinked: true,
		},
		{
			name:         "multiple issue IDs",
			numIssues:    1,
			buildTitle:   func(ids []string) string { return "Fix #" + ids[0] },
			buildDesc:    func(ids []string) string { return "This PR also mentions issue #" + ids[0] + " again" },
			expectLinked: true,
		},
		{
			name:         "no issue ID mentioned",
			numIssues:    1,
			buildTitle:   func(ids []string) string { return "Fix some other bug" },
			buildDesc:    func(ids []string) string { return "No issue reference" },
			expectLinked: false,
		},
		{
			name:      "multiple different issues in title and description",
			numIssues: 3,
			buildTitle: func(ids []string) string {
				return "Fix #" + ids[0] + " and #" + ids[1] + " - important bugs"
			},
			buildDesc: func(ids []string) string {
				return "This PR fixes #" + ids[0] + " and also resolves #" + ids[2]
			},
			expectLinked: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Create issues for this test case
			issues := make([]*pb.Issue, 0, tc.numIssues)
			issueIDs := make([]string, 0, tc.numIssues)

			for i := 0; i < tc.numIssues; i++ {
				issueOp, err := issueClient.Create(ctx, &pb.CreateIssueRequest{
					RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
					Title:               fmt.Sprintf("Test issue %d for auto-linking", i+1),
					Description:         "This issue should be auto-linked to PR",
					Visibility:          pb.Issue_VISIBILITY_PRIVATE,
					NotificationOptions: testutils.SkipNotificationPb,
				})
				require.NoError(t, err)
				issue := testutils.UnmarshalGrpcResult[*pb.Issue](t, issueOp)
				issues = append(issues, issue)
				issueIDs = append(issueIDs, issue.PublicId)
			}

			// Create PR with specific title and description
			prOp, err := prClient.Create(ctx, &pb.CreatePullRequestRequest{
				RepoId:              grpc2.MarshalID(suite.repos.Alpha.ID),
				Title:               tc.buildTitle(issueIDs),
				Description:         tc.buildDesc(issueIDs),
				Source:              "branch", // Use existing test branch
				Target:              "master",
				Publish:             true,
				NotificationOptions: testutils.SkipNotificationPb,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)

			pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, prOp)
			prID, err := grpc_marshalling.IDDirect(pr.Id)
			require.NoError(t, err)

			// Check each issue to verify PR is linked
			for i, issue := range issues {
				getIssueReq := &pb.GetIssueRequest{
					Issue: &pb.GetIssueRequest_Id{Id: issue.Id},
				}
				gotIssue, err := issueClient.Get(ctx, getIssueReq)
				require.NoError(t, err)

				// Check if PR is linked to this issue
				var linkedPRIDs []uint64
				for _, linkedPR := range gotIssue.LinkedPrs {
					id, err := grpc_marshalling.IDDirect(linkedPR.Id)
					require.NoError(t, err)
					linkedPRIDs = append(linkedPRIDs, id)
				}

				if tc.expectLinked {
					require.Contains(t, linkedPRIDs, prID, "PR should be auto-linked to issue %d (%s)", i+1, issue.PublicId)
				} else {
					require.NotContains(t, linkedPRIDs, prID, "PR should NOT be auto-linked to issue %d (%s)", i+1, issue.PublicId)
				}
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPR_CreateWithAutoLinkedIssuesFromCommitsAndBranch() {
	t := suite.T()

	prClient := pb.NewPRServiceClient(suite.grpcClient)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	testRepo := suite.repos.Alpha
	testUser := suite.users.Kopatych
	token := testutils.FakeIAMAuthToken(testUser.Identity)

	suite.addRole(t, testUser, testRepo, iam.Roles.RepositoriesDeveloper)

	// Clone the repository once for all test cases
	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, testRepo.FullSlug())
	tmpDir := testutils.TempDir(t, "", "autolink-test")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(token)
	cg.Must(t, "clone", repoURL)

	repoDir := tmpDir + "/" + testRepo.Slug
	cg = cgit.NewCGit(repoDir).WithAuthToken(token)

	testcases := []struct {
		name            string
		numIssues       int
		buildBranchName func(issueIDs []string) string
		buildCommits    func(issueIDs []string) []string
		expectLinked    bool
	}{
		{
			name:      "issue ID only in commit message",
			numIssues: 1,
			buildBranchName: func(ids []string) string {
				return "feature/test-without-issue-in-name"
			},
			buildCommits: func(ids []string) []string {
				return []string{
					fmt.Sprintf("First commit for #%s", ids[0]),
					fmt.Sprintf("Second commit mentions %s", ids[0]),
				}
			},
			expectLinked: true,
		},
		{
			name:      "issue ID only in branch name",
			numIssues: 1,
			buildBranchName: func(ids []string) string {
				return fmt.Sprintf("feature/%s-test-branch", ids[0])
			},
			buildCommits: func(ids []string) []string {
				return []string{
					"First commit without issue reference",
					"Second commit also without issue",
				}
			},
			expectLinked: true,
		},
		{
			name:      "issue ID in both branch name and commits",
			numIssues: 1,
			buildBranchName: func(ids []string) string {
				return fmt.Sprintf("feature/%s-combined", ids[0])
			},
			buildCommits: func(ids []string) []string {
				return []string{
					fmt.Sprintf("Fix #%s in first commit", ids[0]),
					fmt.Sprintf("Continue fixing %s", ids[0]),
				}
			},
			expectLinked: true,
		},
		{
			name:      "multiple different issues in different commits",
			numIssues: 3,
			buildBranchName: func(ids []string) string {
				return fmt.Sprintf("feature/%s-multi", ids[0])
			},
			buildCommits: func(ids []string) []string {
				return []string{
					fmt.Sprintf("First commit for #%s", ids[0]),
					fmt.Sprintf("Second commit for #%s", ids[1]),
					fmt.Sprintf("Third commit for #%s", ids[2]),
				}
			},
			expectLinked: true,
		},
		{
			name:      "same issue mentioned multiple times",
			numIssues: 1,
			buildBranchName: func(ids []string) string {
				return fmt.Sprintf("feature/%s-repeated", ids[0])
			},
			buildCommits: func(ids []string) []string {
				return []string{
					fmt.Sprintf("First mention of #%s", ids[0]),
					fmt.Sprintf("Second mention of %s", ids[0]),
					fmt.Sprintf("Third mention fixes #%s", ids[0]),
				}
			},
			expectLinked: true,
		},
		{
			name:      "no issue references",
			numIssues: 1,
			buildBranchName: func(ids []string) string {
				return "feature/no-issue-refs"
			},
			buildCommits: func(ids []string) []string {
				return []string{
					"First commit without any references",
					"Second commit also clean",
				}
			},
			expectLinked: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Create fresh issues for this test case
			issues := make([]*pb.Issue, 0, tc.numIssues)
			issueIDs := make([]string, 0, tc.numIssues)

			for i := 0; i < tc.numIssues; i++ {
				issueOp, err := issueClient.Create(ctx, &pb.CreateIssueRequest{
					RepoId:              grpc2.MarshalID(testRepo.ID),
					Title:               fmt.Sprintf("Test issue %d for auto-linking", i+1),
					Description:         "This issue should be auto-linked to PR",
					Visibility:          pb.Issue_VISIBILITY_PUBLIC,
					NotificationOptions: testutils.SkipNotificationPb,
				})
				require.NoError(t, err)
				issue := testutils.UnmarshalGrpcResult[*pb.Issue](t, issueOp)
				issues = append(issues, issue)
				issueIDs = append(issueIDs, issue.PublicId)
			}

			// Return to master branch
			cg.Must(t, "checkout", "master")
			cg.Must(t, "pull", "origin", "master")

			// Create a new branch
			branchName := tc.buildBranchName(issueIDs)
			cg.Must(t, "checkout", "-b", branchName)

			// Create commits
			commits := tc.buildCommits(issueIDs)
			for i, commitMsg := range commits {
				testFile := fmt.Sprintf("%s/autolink_test_%s_%d.txt", repoDir, tc.name, i)
				err := os.WriteFile(testFile, []byte(fmt.Sprintf("test content %d", i)), 0644)
				require.NoError(t, err)
				cg.Must(t, "add", ".")
				cg.Must(t, "commit", "-m", commitMsg)
			}

			// Push the branch
			cg.Must(t, "push", "origin", branchName)

			// Create PR from this branch
			prOp, err := prClient.Create(ctx, &pb.CreatePullRequestRequest{
				RepoId:              grpc2.MarshalID(testRepo.ID),
				Title:               "Test PR for " + tc.name,
				Description:         "This PR tests auto-linking",
				Source:              branchName,
				Target:              "master",
				Publish:             true,
				NotificationOptions: testutils.SkipNotificationPb,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)

			pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, prOp)
			prID, err := grpc_marshalling.IDDirect(pr.Id)
			require.NoError(t, err)

			// Check each issue to verify PR is linked
			for i, issue := range issues {
				getIssueReq := &pb.GetIssueRequest{
					Issue: &pb.GetIssueRequest_Id{Id: issue.Id},
				}
				gotIssue, err := issueClient.Get(ctx, getIssueReq)
				require.NoError(t, err)

				// Check if PR is linked to this issue
				linkedPRIDs := make([]uint64, 0)
				for _, linkedPR := range gotIssue.LinkedPrs {
					id, err := grpc_marshalling.IDDirect(linkedPR.Id)
					require.NoError(t, err)
					linkedPRIDs = append(linkedPRIDs, id)
				}

				if tc.expectLinked {
					require.Contains(t, linkedPRIDs, prID, "PR should be auto-linked to issue %d (%s)", i+1, issue.PublicId)
				} else {
					require.NotContains(t, linkedPRIDs, prID, "PR should NOT be auto-linked to issue %d (%s)", i+1, issue.PublicId)
				}
			}
		})
	}
}
