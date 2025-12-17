package integrationtests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"common/cgit"
	"common/functools"
	"common/grpc"
	log "common/logging"
	"common/oyaml"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/adapters/ci"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/messages"
	"gitcore/internal/revision"
	"gitcore/internal/testutils"
	yautils "gitcore/internal/utils"
	pb_ci "private_api/generated/yandex/cloud/priv/ci/v1"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestGrpcForkList() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	user := suite.users.Krosh
	ctx := testutils.AuthorizeGRPC(user.Identity)

	repo, _ := suite.createRepo(t, user, "test-repo-2", pb.ResourceVisibility_RESOURCE_PUBLIC, "")

	forkOriginRepoID, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)

	func() {
		rev, err := suite.RevRepo.GetCurrent(ctx, revision.Repo(forkOriginRepoID).Forks)
		require.NoError(t, err)
		require.NotNil(t, rev.Count)
		require.Equal(t, 0, *rev.Count)
	}()

	for i := 0; i < 7; i++ {
		fork := suite.forkRepo(t, user, repo.Id)
		_ = fork
	}

	list, err := client.ListForks(ctx, &pb.ListForksRequest{
		Id:       repo.Id,
		PageSize: utils.PtrFromValue(uint64(5)),
	})
	require.NoError(t, err)
	require.Len(t, list.Repositories, 5)
	require.Empty(t, list.PrevPageToken)
	require.NotEmpty(t, list.NextPageToken)

	require.NotNil(t, list.Revision)
	require.NotNil(t, list.Revision.Count)
	require.Equal(t, 7, int(*list.Revision.Count))

	list2, err := client.ListForks(ctx, &pb.ListForksRequest{
		Id:        repo.Id,
		PageSize:  utils.PtrFromValue(uint64(5)),
		PageToken: &list.NextPageToken,
	})
	require.NoError(t, err)
	require.Len(t, list2.Repositories, 2)
	require.NotEmpty(t, list2.PrevPageToken)
	require.Empty(t, list2.NextPageToken)

	require.NotNil(t, list2.Revision)
	require.NotNil(t, list2.Revision.Count)
	require.Equal(t, 7, int(*list2.Revision.Count))

	func() {
		rev, err := suite.RevRepo.GetCurrent(ctx, revision.Repo(forkOriginRepoID).Forks)
		require.NoError(t, err)
		require.NotNil(t, rev.Count)
		require.Equal(t, 7, *rev.Count)
	}()

	repo2, err := client.Get(ctx, &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{Id: repo.Id},
	})
	require.NoError(t, err)
	require.NotNil(t, repo2.Forks)
	require.Equal(t, 7, int(*repo2.Forks))

	require.Len(t, functools.Filter(suite.WebSocketRequests(), func(e entities.WsUnittestBackendRequest) bool {
		return strings.Contains(string(e.Message), `"type":"RepoForksCollection"`)
	}), 7, "Should contain 7 events of type RepoForksCollection")
}

func (suite *RwApiTestSuite) TestForkedRepoToDuplicate() {
	t := suite.T()

	krosh := suite.users.Krosh

	ctx := context.Background()

	const forkLen = 3

	for it := 0; it < forkLen; it++ {
		repo, _ := suite.createRepo(t, krosh, fmt.Sprintf("test-repo-%d", it), pb.ResourceVisibility_RESOURCE_PUBLIC, "")

		var accessible []plumbing.Hash

		cmts := suite.addCommits(t, repo.CloneUrl.Https, krosh, commitsOptions{id: "original1"})
		accessible = append(accessible, cmts...)

		suite.addSomeLFSObjects(t, repo.CloneUrl.Https, krosh)

		inaccessible := suite.addCommits(t,
			repo.CloneUrl.Https,
			krosh,
			commitsOptions{
				id: "original2",
				initializeFunc: func(t testing.TB, cg *cgit.CGit) {
					cg.Must(t, "checkout", "-b", "separate", cmts[0].String())
				},
			})

		var forkedCommits [][]plumbing.Hash
		var forkedInaccessible [][]plumbing.Hash
		var chain []*pb.Repository
		parent := repo
		for i := 0; i < forkLen; i++ {
			fork := suite.forkRepo(t, krosh, parent.Id)
			parent = fork

			forkedCommits = append(forkedCommits,
				suite.addCommits(t, fork.CloneUrl.Https, krosh, commitsOptions{id: fmt.Sprintf("fork%d", i)}))

			chain = append(chain, fork)

			un := suite.addCommits(t,
				fork.CloneUrl.Https,
				krosh,
				commitsOptions{
					id: fmt.Sprintf("fork%d", i),
					initializeFunc: func(t testing.TB, cg *cgit.CGit) {
						cg.Must(t, "checkout", "-b", "separate", cmts[0].String())
					},
				})
			forkedInaccessible = append(forkedInaccessible, un)
		}

		func(idx int) {
			r := chain[idx]

			t.Run(fmt.Sprintf("chain %d", idx), func(t *testing.T) {
				repoID, err := grpc_marshalling.IDDirect(r.Id)
				require.NoError(t, err)

				rr, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
				require.NoError(t, err)

				repacker := suite.RepackerFactory.
					WithHeartBeat(func(s string) error {
						// log.Info(ctx, s)
						return nil
					}).
					Build(rr.OrgID, rr.ID, rr.Visibility)
				err = repacker.ForkToDuplicate(ctx)
				require.NoError(t, err)

				// Here this repo should not be a fork anymore
				repo, err := suite.RepoRepo.GetRepositoryByID(ctx, rr.ID)
				require.NoError(t, err)
				require.Nil(t, repo.ForkOriginID)
				// And still be able to clone
				cg, err := suite.clone(t, r.CloneUrl.Https, krosh)
				require.NoError(t, err)
				// Get lfs
				cg.WithAuthTokenSite(r.CloneUrl.Https).Must(t, "lfs", "pull")

				// Make gitFS factory to check objects existence
				loader := suite.GitFSFactory.Build(rr.ID)

				gitFS, closer, err := loader.Load(ctx)
				require.NoError(t, err)
				defer func() { require.NoError(t, closer()) }()

				// Check that objects from sepCmts are not accesible
				missing, err := gitFS.CheckObjectsByHash(ctx, inaccessible)
				require.NoError(t, err)
				require.ElementsMatch(t, inaccessible, missing)

				// Check that commits from master are accessible
				missing2, err := gitFS.CheckObjectsByHash(ctx, accessible)
				require.NoError(t, err)
				require.Len(t, missing2, 0)

				// Check that commits from parent forks are accessible
				for _, fcmts := range forkedCommits[0:idx] {
					missing3, err := gitFS.CheckObjectsByHash(ctx, fcmts)
					require.NoError(t, err)
					require.Len(t, missing3, 0)
				}

				// Check that added separate commits from parent forks are inaccessible
				if idx > 0 {
					for _, fcmts := range forkedInaccessible[0 : idx-1] {
						missing3, err := gitFS.CheckObjectsByHash(ctx, fcmts)
						require.NoError(t, err)
						require.ElementsMatch(t, fcmts, missing3)
					}
				}

				// Test that fork-to-duplicate housekeeper works.
				var f2dhk interfaces.HousekeepingMethod
				for _, hk := range suite.HouseKeepers {
					if hk.Name() == "check-fork-to-duplicate" {
						f2dhk = hk
					}
				}
				require.NotNil(t, f2dhk, "can't find check-fork-to-duplicate housekeeper")

				err = f2dhk.Invoke("--dry-run=0", fmt.Sprintf("--repo-id=%d", repoID))
				require.NoError(t, err)
			})
		}(it)
	}
}

func (suite *RwApiTestSuite) TestForkChainRebuildOnVisibilityChange() {
	t := suite.T()
	ctx := context.Background()
	user := suite.users.Krosh

	// Create parent repository as public
	parentRepo, _ := suite.createRepo(t, user, "parent-repo", pb.ResourceVisibility_RESOURCE_PUBLIC, "")
	parentRepoID, err := grpc_marshalling.IDDirect(parentRepo.Id)
	require.NoError(t, err)
	suite.addCommits(t, parentRepo.CloneUrl.Https, user, commitsOptions{id: "parentRepo"})

	// Create fork chain: parent -> fork1, fork2
	fork1 := suite.forkRepo(t, user, parentRepo.Id)
	fork1ID, err := grpc_marshalling.IDDirect(fork1.Id)
	require.NoError(t, err)
	suite.addCommits(t, fork1.CloneUrl.Https, user, commitsOptions{id: "fork1"})

	fork2 := suite.forkRepo(t, user, parentRepo.Id)
	fork2ID, err := grpc_marshalling.IDDirect(fork2.Id)
	require.NoError(t, err)
	suite.addCommits(t, fork2.CloneUrl.Https, user, commitsOptions{id: "fork2"})

	t.Run("initial_structure", func(t *testing.T) {
		parentEntity, err := suite.RepoRepo.GetRepositoryByID(ctx, parentRepoID)
		require.NoError(t, err)
		require.Nil(t, parentEntity.ForkOriginID)

		fork1Entity, err := suite.RepoRepo.GetRepositoryByID(ctx, fork1ID)
		require.NoError(t, err)
		require.NotNil(t, fork1Entity.ForkOriginID)
		require.Equal(t, parentRepoID, *fork1Entity.ForkOriginID)

		fork2Entity, err := suite.RepoRepo.GetRepositoryByID(ctx, fork2ID)
		require.NoError(t, err)
		require.NotNil(t, fork2Entity.ForkOriginID)
		require.Equal(t, parentRepoID, *fork2Entity.ForkOriginID)
	})

	// Change parent repository visibility to private
	client := pb.NewRepoServiceClient(suite.grpcClient)
	updateCtx := testutils.WithAuthorizedGRPC(ctx, user.Identity)

	updateOp, err := client.Update(updateCtx, &pb.UpdateRepositoryRequest{
		Id:         parentRepo.Id,
		Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	require.NoError(t, err)

	_, err = grpc_marshalling.OperationResponse(updateOp, &pb.Repository{})
	require.NoError(t, err)

	// Wait for fork chain rebuild workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.RebuildForkChain)

	t.Run("chain", func(t *testing.T) {
		parentEntity, err := suite.RepoRepo.GetRepositoryByID(ctx, parentRepoID)
		require.NoError(t, err)
		require.Nil(t, parentEntity.ForkOriginID)
		require.Equal(t, entities.Visibilities.Private, parentEntity.Visibility)

		fork1Entity, err := suite.RepoRepo.GetRepositoryByID(ctx, fork1ID)
		require.NoError(t, err)
		require.Nil(t, fork1Entity.ForkOriginID, "fork1 should be converted to duplicate")

		fork2Entity, err := suite.RepoRepo.GetRepositoryByID(ctx, fork2ID)
		require.NoError(t, err)
		require.NotNil(t, fork2Entity.ForkOriginID)
		require.Equal(t, fork1ID, *fork2Entity.ForkOriginID, "fork2 should now point to fork1")
	})

	t.Run("clone", func(t *testing.T) {
		_, err := suite.clone(t, fork1.CloneUrl.Https, user)
		require.NoError(t, err, "fork1 should be cloneable after conversion to duplicate")

		_, err = suite.clone(t, fork2.CloneUrl.Https, user)
		require.NoError(t, err, "fork2 should be cloneable after fork chain rebuild")
	})
}

func (suite *RwApiTestSuite) TestForkedRepoAuth() {
	t := suite.T()
	t.Skip("Skipped till we allow forks of private repos")

	krosh := suite.users.Krosh
	pikachu := suite.users.Pikachu

	repo, _ := suite.createRepo(t, krosh, "test-repo-3", pb.ResourceVisibility_RESOURCE_PRIVATE, "")

	func() { // Add permission to view repo
		ctx := context.Background()
		accessBindingsclient := pb.NewAccessBindingsServiceClient(suite.grpcClient)
		ctx = testutils.WithAuthorizedGRPC(ctx, krosh.Identity)

		op, err := accessBindingsclient.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
			Object: &pb.Object{Identifier: &pb.Object_RepoId{RepoId: repo.Id}},
			BindingDeltas: []*pb.AccessBindingDelta{
				{
					Action: pb.DeltaAction_ADD,
					Binding: &pb.AccessBinding{
						Role: string(iam.Roles.Viewer),
						Subject: &pb.Subject{
							Type: pb.Subject_USER,
							Id:   grpc_marshalling.IDInverse(pikachu.ID),
						},
					},
				},
			},
		})
		require.NoError(t, err)
		resp := testutils.UnmarshalGrpcResult[*pb.AccessBindingsOperationResult](t, op)
		_ = resp

	}()

	for i := 0; i < 3; i++ {
		fork := suite.forkRepo(t, pikachu, repo.Id)

		_ = fork

		require.Len(t, suite.listCommits(t, pikachu, fork.Id).Commits, 1,
			"As we forked a new repo we should have exactly one commit")
	}

	func() {
		reposList := suite.listRepos(t, pikachu)
		require.Len(t, reposList, 3)

		require.Len(t, suite.listCommits(t, pikachu, reposList[0].Id).Commits, 1)
	}()

	func() {
		ctx := context.Background()

		accessBindingsclient := pb.NewAccessBindingsServiceClient(suite.grpcClient)
		ctx = testutils.WithAuthorizedGRPC(ctx, krosh.Identity)

		op, err := accessBindingsclient.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
			Object: &pb.Object{Identifier: &pb.Object_RepoId{RepoId: repo.Id}},
			BindingDeltas: []*pb.AccessBindingDelta{
				{
					Action: pb.DeltaAction_REMOVE,
					Binding: &pb.AccessBinding{
						Role: string(iam.Roles.Viewer),
						Subject: &pb.Subject{
							Type: pb.Subject_USER,
							Id:   grpc_marshalling.IDInverse(pikachu.ID),
						},
					},
				},
			},
		})
		require.NoError(t, err)
		resp := testutils.UnmarshalGrpcResult[*pb.AccessBindingsOperationResult](t, op)

		bindings, err := accessBindingsclient.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
			Object: &pb.Object{Identifier: &pb.Object_RepoId{RepoId: repo.Id}},
		})
		require.NoError(t, err)

		_ = bindings
		_ = resp
	}()

	func() {
		reposList2 := suite.listRepos(t, pikachu)
		require.Len(t, reposList2, 3)

		_, err := suite.clone(t, reposList2[0].CloneUrl.Https, krosh)
		require.NoError(t, err)

		_, err = suite.clone(t, reposList2[0].CloneUrl.Https, pikachu)
		require.Error(t, err)
	}()
}

func (suite *RwApiTestSuite) TestGrpcForkHideParentRepo() {
	t := suite.T()

	user := suite.users.Krosh
	forkUser := suite.users.Admin

	client := pb.NewOrgServiceClient(suite.grpcClient)
	commonOrg, err := client.GetProfile(
		testutils.WithAuthorizedGRPC(context.Background(), user.Identity),
		&pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID)},
		})
	require.NoError(t, err)

	tests := map[string]struct {
		getorg      func(t testing.TB) *pb.OrgProfile
		prepare     func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile)
		wasRestored bool
	}{
		"make_parent_repo_private": {
			prepare: func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile) {
				client := pb.NewRepoServiceClient(suite.grpcClient)
				upOp, err := client.Update(
					testutils.WithAuthorizedGRPC(context.Background(), user.Identity),
					&pb.UpdateRepositoryRequest{
						Id:         repoParent.Id,
						Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
						UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
					})
				require.NoError(t, err)

				_, err = grpc_marshalling.OperationResponse(upOp, &pb.Repository{})
				require.NoError(t, err)

				suite.WaitForWorkflows(t, entities.WorkflowTypes.RebuildForkChain)
			},
			wasRestored: true,
		},
		"make_parent_repo_internal": {
			prepare: func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile) {
				client := pb.NewRepoServiceClient(suite.grpcClient)
				upOp, err := client.Update(
					testutils.WithAuthorizedGRPC(context.Background(), user.Identity),
					&pb.UpdateRepositoryRequest{
						Id:         repoParent.Id,
						Visibility: pb.ResourceVisibility_RESOURCE_INTERNAL,
						UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
					})
				require.NoError(t, err)

				_, err = grpc_marshalling.OperationResponse(upOp, &pb.Repository{})
				require.NoError(t, err)

				suite.WaitForWorkflows(t, entities.WorkflowTypes.RebuildForkChain)
			},
			wasRestored: true,
		},
		"delete_parent_repo": {
			prepare: func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile) {
				client := pb.NewRepoServiceClient(suite.grpcClient)
				_, err := client.Delete(
					testutils.WithAuthorizedGRPC(context.Background(), user.Identity),
					&pb.DeleteRepositoryRequest{
						Id: repoParent.Id,
					})
				require.NoError(t, err)

				suite.WaitForWorkflows(t, entities.WorkflowTypes.RebuildForkChain)
			},
			wasRestored: true,
		},
		"make_parent_org_private": {
			prepare: func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile) {
				// user = suite.users.Kopatych

				upOp, err := client.UpdateProfile(
					testutils.WithAuthorizedGRPC(context.Background(), user.Identity),
					testutils.FieldMask(&pb.UpdateOrgProfileRequest{
						Id:         org.Id,
						Visibility: pb.ProfileVisibility_PROFILE_PRIVATE,
					}),
				)
				require.NoError(t, err)

				_, err = grpc_marshalling.OperationResponse(upOp, &pb.OrgProfile{})
				require.NoError(t, err)
			},
			getorg: func(t testing.TB) *pb.OrgProfile {
				return suite.createOrg(t, user, "own-parent-org")
			},
		},
		"delete_parent_org": {
			prepare: func(t testing.TB, repoParent *pb.Repository, org *pb.OrgProfile) {
				orgID, err := grpc_marshalling.IDDirect(org.Id)
				require.NoError(t, err)

				err = suite.OrgRepo.DeleteOrganizationByID(context.Background(), orgID)
				require.NoError(t, err)
			},
			getorg: func(t testing.TB) *pb.OrgProfile {
				return suite.createOrg(t, user, "own-parent-org-to-del")
			},
		},
	}

	for tname, tt := range tests {
		t.Run(tname, func(t *testing.T) {
			ctx := context.Background()

			slug, err := yautils.NormalizeSlug(tname)
			require.NoError(t, err)

			org := commonOrg
			if tt.getorg != nil {
				org = tt.getorg(t)
			}

			repoParent, _ := suite.createRepo(t, user,
				"test-repo-"+slug, pb.ResourceVisibility_RESOURCE_PUBLIC, org.Id)
			suite.addCommits(t, repoParent.CloneUrl.Https, user, commitsOptions{
				commitsCount: 1,
				generateFiles: func(_ testing.TB, cg *cgit.CGit, idx int) string {
					fname := fmt.Sprintf("README_%d", idx)
					suite.makeNewFileWithContent(*cg, fname, utils.MustMakeRandomString("Some random instruction", 32))

					return fname
				},
			})

			repo := suite.forkRepo(t, user, repoParent.Id)

			// We want to create a fork in a personal org
			fork := suite.forkRepo(t, forkUser, repo.Id)
			require.False(t, fork.BrokenForkChain)

			suite.addCommits(t, fork.CloneUrl.Https, forkUser, commitsOptions{})

			clientPR := pb.NewPRServiceClient(suite.grpcClient)
			ctx = testutils.WithAuthorizedGRPC(ctx, forkUser.Identity)

			prOP, err := clientPR.Create(ctx, &pb.CreatePullRequestRequest{
				RepoId:              repo.Id,
				ForkRepoId:          &fork.Id,
				Source:              "master",
				Target:              "master",
				Title:               "ForkToParent",
				Description:         "From fork to parent",
				Publish:             true,
				ReviewerIds:         []string{},
				NotificationOptions: testutils.SkipNotificationPb,
			})
			require.NoError(t, err)

			pr, err := grpc_marshalling.OperationResponse(prOP, &pb.PullRequest{})
			require.NoError(t, err)

			// Check PR is visible
			_, err = clientPR.Get(ctx, &pb.GetPullRequestRequest{
				Identity: &pb.GetPullRequestRequest_Id{Id: pr.Id},
			})
			require.NoError(t, err)

			tt.prepare(t, repoParent, org)

			// Check fork is broken
			client := pb.NewRepoServiceClient(suite.grpcClient)
			fork2, err := client.Get(ctx, &pb.GetRepositoryRequest{
				Repo: &pb.GetRepositoryRequest_Id{Id: fork.Id},
			})
			require.NoError(t, err)
			require.Equal(t, fork2.BrokenForkChain, !tt.wasRestored)

			// Check PR can be get
			func() {
				_, err = clientPR.Get(ctx, &pb.GetPullRequestRequest{
					Identity: &pb.GetPullRequestRequest_Id{Id: pr.Id},
				})
				require.NoError(t, err)
			}()

			// Check PR can't be get through bulk function
			func() {
				resp, err := clientPR.GetBulk(ctx, &pb.GetBulkPullRequestsRequest{
					Ids: []string{pr.Id},
				})
				require.NoError(t, err)

				require.Len(t, resp.Prs, 1)
			}()

			// Check PR can't be merged
			func() {
				_, err := clientPR.Merge(ctx, &pb.MergeRequest{
					PrId: pr.Id,
				})
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, codes.PermissionDenied, st.Code())
			}()

			// Check we can't clone
			func() {
				_, err := suite.clone(t, fork.CloneUrl.Https, forkUser)
				if tt.wasRestored {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}()
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcForkCompareIterations() {
	t := suite.T()

	ctx := context.Background()
	user := suite.users.Krosh

	repo, _ := suite.createRepo(t,
		user,
		"test-grpc-fork-compare-iterations-repo",
		pb.ResourceVisibility_RESOURCE_PUBLIC, "")
	suite.addCommits(t, repo.CloneUrl.Https, user, commitsOptions{id: "original"})

	// We want to create a fork in a personal org
	fork := suite.forkRepo(t, user, repo.Id)

	suite.addCommits(t, fork.CloneUrl.Https, user, commitsOptions{id: "original2"})

	clientPR := pb.NewPRServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	prOP, err := clientPR.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:              repo.Id,
		ForkRepoId:          &fork.Id,
		Source:              "master",
		Target:              "master",
		Title:               "ForkToParent",
		Description:         "From fork to parent",
		Publish:             true,
		ReviewerIds:         []string{},
		NotificationOptions: testutils.SkipNotificationPb,
	})
	require.NoError(t, err)

	pr, err := grpc_marshalling.OperationResponse(prOP, &pb.PullRequest{})
	require.NoError(t, err)

	suite.addCommits(t, fork.CloneUrl.Https, user, commitsOptions{id: "fork"})

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	iters, err := client.ListIterations(ctx, &pb.ListIterationsRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)
	require.Len(t, iters.Iterations, 4)

	// var paths []*pb.DiffPath
	resp, err := clientPR.GetChanges(ctx, &pb.GetChangesRequest{
		PrId:       pr.Id,
		IterId:     &iters.Iterations[2].Id,
		FromIterId: &iters.Iterations[1].Id,
	})
	require.NoError(t, err)
	_ = resp

	respStats, err := clientPR.GetStats(ctx, &pb.GetStatsRequest{
		PrId:       pr.Id,
		IterId:     &iters.Iterations[2].Id,
		FromIterId: &iters.Iterations[1].Id,
	})
	require.NoError(t, err)
	_ = respStats

	paths := functools.Map(resp.TreeDiffs, func(td *pb.TreeDiff) *pb.DiffPath {
		return td.Paths
	})

	respFileChanges, err := clientPR.GetFileChanges(ctx, &pb.GetFileChangesRequest{
		PrId:       pr.Id,
		IterId:     &iters.Iterations[2].Id,
		FromIterId: &iters.Iterations[1].Id,
		Paths:      paths,
	})
	require.NoError(t, err)
	_ = respFileChanges
}

func (suite *RwApiTestSuite) TestGrpcForkHappyPathTest() {
	t := suite.T()
	t.Skip("fix in 4654, required lfs")

	ctx := context.Background()
	user := suite.users.Krosh

	repo, intialRev := suite.createRepo(t,
		user,
		"fork-repo-path-test",
		pb.ResourceVisibility_RESOURCE_PUBLIC, "")

	origCloneURL := repo.CloneUrl.Https

	func() {
		cg := cgit.NewCGit(t.TempDir()).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
		cg.Must(t, "clone", origCloneURL, ".")

		origCommitHashes, err := cg.GetAllCommits()
		require.NoError(t, err)
		require.Len(t, origCommitHashes, 1, "We must have only one commit in initial repo")
	}()

	// We want to create a fork in a personal org
	fork := suite.forkRepo(t, user, repo.Id)
	require.False(t, fork.BrokenForkChain)

	forkID, err := grpc_marshalling.IDDirect(fork.Id)
	require.NoError(t, err)

	// Check that we have only one branch
	rev, err := suite.RevRepo.GetCurrent(ctx, revision.Repo(forkID).Branches)
	require.NoError(t, err)
	require.NotNil(t, rev.Count)
	require.Equal(t, 1, *rev.Count)

	require.Len(t, suite.listCommits(t, user, fork.Id).Commits, 1, "As we forked a new repo we should have exactly one commit")

	forkCloneURL := fork.CloneUrl.Https

	filenameFunc := func(t testing.TB, idx int) string { return fmt.Sprintf("file%d.go", idx) }

	suite.addCommits(t, origCloneURL, user, commitsOptions{
		id:       "original",
		filename: filenameFunc,
	})
	require.Len(t, suite.getCommitHashes(t, origCloneURL, user), 4, "1 initial commit + 3 additional commits")

	require.Len(t, suite.listCommits(t, user, fork.Id).Commits, 1, "New commits should not appear as refs are separate")

	// Add some LFS objects to the fork
	suite.addSomeLFSObjects(t, forkCloneURL, user)

	require.Len(t, suite.listCommits(t, user, fork.Id).Commits, 2, "1 initial commit + 1 lfs commit")

	suite.addCommits(t, forkCloneURL, user, commitsOptions{
		id:       "fork",
		filename: filenameFunc,
	})
	require.Len(t, suite.listCommits(t, user, fork.Id).Commits, 5, "1 initial commit + 1 lfs commit + 3 additional commits")

	clientPR := pb.NewPRServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	prOP, err := clientPR.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:              repo.Id,
		ForkRepoId:          &fork.Id,
		Source:              "master",
		Target:              "master",
		Title:               "ForkToParent",
		Description:         "From fork to parent",
		Publish:             true,
		ReviewerIds:         []string{},
		NotificationOptions: testutils.SkipNotificationPb,
	})
	require.NoError(t, err)

	pr, err := grpc_marshalling.OperationResponse(prOP, &pb.PullRequest{})
	require.NoError(t, err)

	var paths []*pb.DiffPath
	resp, err := clientPR.GetChanges(ctx, &pb.GetChangesRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)

	respStats, err := clientPR.GetStats(ctx, &pb.GetStatsRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)
	_ = respStats

	paths = functools.Map(resp.TreeDiffs, func(td *pb.TreeDiff) *pb.DiffPath {
		return td.Paths
	})

	changes, err := clientPR.GetFileChanges(ctx, &pb.GetFileChangesRequest{
		PrId:  pr.Id,
		Paths: paths,
	})
	require.NoError(t, err)
	_ = changes

	// Wait for mergecheck finish
	suite.WaitForWorkflows(t, entities.WorkflowTypes.CalculateMergeConflict)

	checks, err := clientPR.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)
	require.Len(t, checks.MergeChecks.Conflicts.Conflicts, 3) // there should be 3 conflicts

	// Merge
	func() {
		_, err := clientPR.Merge(ctx, &pb.MergeRequest{
			PrId:  pr.Id,
			Force: true,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

		pr, err := clientPR.Get(ctx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: pr.Id,
			},
		})
		require.NoError(t, err)
		require.Equal(t, pb.PullRequest_OPEN, pr.PrStatus)
		require.NotEmpty(t, pr.MergeInfo.Error, "this should fail as we've added commits to master")
	}()

	// Return master back to initial commit
	suite.resetMaster(t, origCloneURL, user, plumbing.NewHash(intialRev.Commit.Hash))
	require.Len(t, suite.getCommitHashes(t, origCloneURL, user), 1, "1 initial commit")

	// And try to merge again
	func() {
		_, err := clientPR.Merge(ctx, &pb.MergeRequest{
			PrId:  pr.Id,
			Force: true,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

		pr, err := clientPR.Get(ctx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: pr.Id,
			},
		})
		require.NoError(t, err)
		require.Equal(t, pb.PullRequest_MERGED, pr.PrStatus)
		require.Empty(t, pr.MergeInfo.Error, "this should finish OK now")
	}()

	commitHashes := suite.getCommitHashes(t, origCloneURL, user)
	_ = commitHashes

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	// Get contribution suggests
	func() {
		forkBranchPtr := &pb.BranchPointer{
			RepoId:     fork.Id,
			BranchName: "master",
		}

		cs, err := client.GetContributionSuggests(ctx, &pb.ContributionSuggestsRequest{
			Branch: forkBranchPtr,
		})
		require.NoError(t, err)
		require.Len(t, cs.Suggests, 1)
		require.Equal(t, 0, int(cs.Suggests[0].Ahead))
		require.Equal(t, 1, int(cs.Suggests[0].Behind))

		tmpDir := testutils.TempDir(t, "", "")
		cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
		cg.Must(t, "clone", forkCloneURL, ".")

		insts, err := client.GetInstructionToSyncBranch(ctx, &pb.GetInstructionToSyncBranchRequest{
			ForkBranch:   forkBranchPtr,
			ParentBranch: cs.Suggests[0].ParentBranch,
			SyncType:     pb.GetInstructionToSyncBranchRequest_MERGE,
			GitUrlType:   pb.GetInstructionToSyncBranchRequest_HTTPS,
		})
		require.NoError(t, err)

		for _, c := range insts.SyncWithUpstreamCommands {
			log.Info(ctx, "executing: %s", c)
			var out, errs string
			if !strings.HasPrefix(c, "git ") || strings.Contains(c, "||") || strings.Contains(c, "&&") {
				out2, errs2, err2 := cg.Bash(c)
				out = string(out2)
				errs = string(errs2)
				require.NoError(t, err2)
			} else {
				out, errs, err = cg.Exec(strings.Split(strings.TrimPrefix(c, "git "), " ")...)
				require.NoError(t, err)
			}
			log.Info(ctx, out)
			if len(errs) > 0 {
				log.ErrorNoStack(ctx, errs)
			}
		}

		cs2, err := client.GetContributionSuggests(ctx, &pb.ContributionSuggestsRequest{
			Branch: forkBranchPtr,
		})
		require.NoError(t, err)
		require.Len(t, cs2.Suggests, 1)
		require.Equal(t, int(cs2.Suggests[0].Ahead), 0)
		require.Equal(t, int(cs2.Suggests[0].Behind), 0)
	}()

	// Remove parent repo
	delOp, err := client.Delete(ctx, &pb.DeleteRepositoryRequest{
		Id: repo.Id,
	})
	require.NoError(t, err)
	_ = delOp

	// Try to access fork
	fork2, err := client.Get(ctx, &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{Id: fork.Id},
	})
	require.NotNil(t, fork2)
	require.NoError(t, err)
	require.True(t, fork2.BrokenForkChain)

	_, err = suite.clone(t, forkCloneURL, user)
	require.Error(t, err, "clone should not work")
}

func (suite *RwApiTestSuite) TestForkedRepoPR() {
	t := suite.T()
	user := suite.users.Krosh

	repo, _ := suite.createRepo(t,
		user,
		"test-repo",
		pb.ResourceVisibility_RESOURCE_PUBLIC,
		"")

	repoID, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)
	repoEntity, err := suite.RepoService.Get(context.Background(), repoID)
	require.NoError(t, err)

	// add CI declaration that triggers 2 workflows on pr creation
	var yamlContent = `
workflows:
  target-workflow:
    tasks: ci
  target-workflow-2:
    tasks: ci

on:
  push:
    - workflows: [target-workflow]
  pull_request:
    - workflows: [target-workflow, target-workflow-2]

tasks:
  - name: ci
    cubes:
      - name: some-name
        image: none
        script:
          - echo 1
`
	suite.addRole(t, suite.users.Admin, repoEntity, iam.Roles.RepositoriesDeveloper)
	suite.addOYaml(repoEntity, "master", oyaml.CIPath, yamlContent)

	fork := suite.forkRepo(t, user, repo.Id)

	forkCloneURL := suite.HTTPSProtocol().RepoURL(fork.OrgSlug, fork.Slug)

	branchName := "master"
	suite.addCommits(t, forkCloneURL, user, commitsOptions{})
	require.Len(t, suite.getCommitHashes(t, forkCloneURL, user), 5, "2 initial commits + 3 additional commits")

	// clear events sent to CI if any
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	suite.ciService.ClearSentEvents()

	pr := suite.createPR(t, user, repo.Id, &fork.Id, branchName, "master")
	prID, err := grpc_marshalling.IDDirect(pr.Id)
	require.NoError(t, err)
	prEntity, err := suite.PullRequestRepo.Get(context.Background(), prID)
	require.NoError(t, err)

	// check that no CI events were sent (because it's a fork)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	events := suite.ciService.SentEvents()
	require.Nil(t, events)

	fluxService, ok := suite.fluxService.(*ci.StubFluxService)
	require.True(t, ok)
	fluxService.Fluxes[prEntity.HeadHash] = nil

	// check that we show expected workflows in checks when there is no flux for forked pr
	mergeChecks, err := suite.PullRequestService.MergeChecks(context.Background(), testutils.NewStubAuthenticator(&user.Identity), prEntity, user, time.Second*5)
	require.NoError(t, err)

	require.Equal(t, 2, len(mergeChecks.CIWorkflows))
	for _, mergeCheck := range mergeChecks.CIWorkflows {
		require.Equal(t, entities.MergeCheckStatuses.ManualRun, mergeCheck.Status)
		require.Equal(t, messages.MsgMergeCIAwaitsManualRun.Build(), mergeCheck.DisplayMessage)
	}

	// now if one of workflows was started manually we must show both
	fluxService.Fluxes[prEntity.HeadHash] = []*pb_ci.Flux{
		{
			Status: pb_ci.TaskStatus_TS_PROCESSING,
			Dates: &pb_ci.DatesByStage{
				CreatedAt: grpc.TimeToProtocTs(time.Now()),
			},
			Workflows: []*pb_ci.Workflow{
				{
					Name:   "target-workflow-2",
					Status: pb_ci.TaskStatus_TS_PROCESSING,
				},
			},
			EventType: pb_ci.EventType_ET_MANUAL,
			PrId:      pr.Id,
			PublicId:  100,
		},
	}
	mergeChecks, err = suite.PullRequestService.MergeChecks(context.Background(), testutils.NewStubAuthenticator(&user.Identity), prEntity, user, time.Second*5)
	require.NoError(t, err)

	require.Equal(t, 2, len(mergeChecks.CIWorkflows))
	require.Equal(t, entities.MergeCheckStatuses.ManualRun, mergeChecks.CIWorkflows[0].Status)
	require.Equal(t, "target-workflow", mergeChecks.CIWorkflows[0].Name)
	require.Equal(t, messages.MsgMergeCIAwaitsManualRun.Build(), mergeChecks.CIWorkflows[0].DisplayMessage)
	require.Equal(t, entities.MergeCheckStatuses.Processing, mergeChecks.CIWorkflows[1].Status)
	require.Equal(t, "target-workflow-2", mergeChecks.CIWorkflows[1].Name)

	// "restart" all workflows and check that trigger was sent correctly
	suite.runWorkflows(t, user, pr, prEntity.HeadHash.String())
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	events = suite.ciService.SentEvents()
	require.Equal(t, 1, len(events))
	require.NotNil(t, events[0].TriggerRequest)
	require.Equal(t, repo.Id, events[0].TriggerRequest.Trigger.Repository.RepoId)
	require.Equal(t, fork.Id, events[0].TriggerRequest.Trigger.SourceRepository.RepoId)

	suite.ciService.ClearSentEvents()
	suite.runPRWorkflows(t, user, pr, prEntity.HeadHash.String(), []string{"target-workflow-2"})
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	events = suite.ciService.SentEvents()
	require.Equal(t, 1, len(events))
	require.NotNil(t, events[0].TriggerRequest)
	require.Equal(t, repo.Id, events[0].TriggerRequest.Trigger.Repository.RepoId)
	require.Equal(t, fork.Id, events[0].TriggerRequest.Trigger.SourceRepository.RepoId)
	require.Equal(t, []string{"target-workflow-2"}, events[0].TriggerRequest.Trigger.GetManualRun().WorkflowNames)
}

func (suite *RwApiTestSuite) runWorkflows(t testing.TB, user *entities.User, pr *pb.PullRequest, headHash string) string {
	ctx := context.Background()

	client := pb.NewCIServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	resp, err := client.RestartAllWorkflows(ctx, &pb.RestartAllWorkflowsRequest{
		RepoId:   pr.RepoId,
		PrId:     pr.Id,
		HeadHash: headHash,
	})
	require.NoError(t, err)

	return resp.FluxId
}

func (suite *RwApiTestSuite) runPRWorkflows(t testing.TB, user *entities.User, pr *pb.PullRequest, headHash string, wfNames []string) string {
	ctx := context.Background()

	client := pb.NewCIServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	resp, err := client.RunPRWorkflows(ctx, &pb.RunPRWorkflowsRequest{
		PrId:          pr.Id,
		HeadHash:      headHash,
		WorkflowNames: wfNames,
	})
	require.NoError(t, err)

	return resp.FluxId
}

func (suite *RwApiTestSuite) createOrg(t testing.TB,
	user *entities.User,
	slug string,
) *pb.OrgProfile {
	client := pb.NewOrgServiceClient(suite.grpcClient)
	ctx := testutils.WithAuthorizedGRPC(context.Background(), user.Identity)
	org, err := client.CreateOrg(ctx,
		&pb.CreateOrgRequest{
			Slug:        slug,
			Description: slug,
			DisplayName: slug,
			Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
		})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, org.GetResponse().UnmarshalTo(&profile))

	return &profile
}

func (suite *RwApiTestSuite) createRepo(t testing.TB,
	user *entities.User,
	slug string,
	visibility pb.ResourceVisibility,
	orgID string,
) (*pb.Repository, *pb.ResolveRevisionResponse) {
	ctx := context.Background()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	if orgID == "" {
		orgID = grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID)
	}

	res, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: orgID,
		},
		Slug:             slug,
		Visibility:       visibility,
		DefaultBranch:    "master",
		ProvisionOptions: &pb.ProvisionOptions{},
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(res, &pb.Repository{})
	require.NoError(t, err)

	cgc, err := suite.clone(t, repo.CloneUrl.Https, user)
	require.NoError(t, err)

	ct, err := time.Parse(time.RFC822Z, "22 Aug 05 21:18 +0200")
	require.NoError(t, err)

	cg := cgc.WithCommitTime(ct)
	require.NoError(t, err)

	suite.makeNewFileWithContent(cg, "READ.me", "# README")

	cg.Must(t, "checkout", "-b", "master")
	cg.Must(t, "add", "READ.me")
	cg.Must(t, "commit", "-m", "Initial commit")
	cg.Must(t, "push", "-u", "origin", "master")

	initialRev, err := client.ResolveRevision(ctx, &pb.ResolveRevisionRequest{
		Id:       repo.Id,
		Revision: "master",
	})
	require.NoError(t, err)

	return repo, initialRev
}

func (suite *RwApiTestSuite) listRepos(t testing.TB, user *entities.User) []*pb.Repository {
	ctx := context.Background()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	po, err := suite.OrgService.GetPersonalOrganization(ctx, nil, user)
	require.NoError(t, err)

	list, err := client.ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{
		OrgId: grpc_marshalling.IDInverse(po.ID),
	})
	require.NoError(t, err)

	return list.Repositories
}

func (suite *RwApiTestSuite) forkRepo(t testing.TB, user *entities.User, repoID string) *pb.Repository {
	ctx := context.Background()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	po, err := suite.OrgService.GetPersonalOrganization(ctx, nil, user)
	require.NoError(t, err)

	forkRepoSlug := suite.mustGenUniqueSlug()
	forkRes, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
		Org: &pb.ForkRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(po.ID),
		},
		ForkOriginId:      repoID,
		Slug:              forkRepoSlug,
		DefaultBranchOnly: true,
	})
	require.NoError(t, err)

	fork, err := grpc_marshalling.OperationResponse(forkRes, &pb.Repository{})
	require.NoError(t, err)

	return fork
}

func (suite *RwApiTestSuite) forkRepoToOrg(t testing.TB, user *entities.User, repoID, orgID string) *pb.Repository {
	ctx := context.Background()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	forkRepoSlug := suite.mustGenUniqueSlug()
	forkRes, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
		Org:               &pb.ForkRepositoryRequest_OrgId{OrgId: orgID},
		ForkOriginId:      repoID,
		Slug:              forkRepoSlug,
		DefaultBranchOnly: true,
	})
	require.NoError(t, err)

	fork, err := grpc_marshalling.OperationResponse(forkRes, &pb.Repository{})
	require.NoError(t, err)

	return fork
}

func (suite *RwApiTestSuite) createPR(
	t testing.TB, user *entities.User, repoID string, sourceRepoID *string, source, target string,
) *pb.PullRequest {
	ctx := context.Background()

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)
	res, err := client.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:      repoID,
		ForkRepoId:  sourceRepoID,
		Title:       utils.MustMakeRandomString("PR: ", 8),
		Description: "",
		Source:      source,
		Target:      target,
		Publish:     true,
	})
	require.NoError(t, err)

	pr, err := grpc_marshalling.OperationResponse(res, &pb.PullRequest{})
	require.NoError(t, err)

	return pr
}

func (suite *RwApiTestSuite) listCommits(t testing.TB, user *entities.User, repoID string) *pb.ListCommitsResponse {
	ctx := context.Background()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx = testutils.WithAuthorizedGRPC(ctx, user.Identity)

	res, err := client.ListCommits(ctx, &pb.ListCommitsRequest{Id: repoID})
	require.NoError(t, err)

	return res
}

func (suite *RwApiTestSuite) resetMaster(t *testing.T, origCloneURL string, user *entities.User, hash plumbing.Hash) {
	tmpDir := testutils.TempDir(t, "", "")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
	cg.Must(t, "clone", origCloneURL, ".")

	cg.Must(t, "reset", "--hard", hash.String())

	cg.Must(t, "push", "-f")
}

func (suite *RwApiTestSuite) getCommitHashes(t testing.TB, cloneURL string, user *entities.User) []plumbing.Hash {
	cg, err := suite.clone(t, cloneURL, user)
	require.NoError(t, err)

	commitHashes, err := cg.GetAllCommits()
	require.NoError(t, err)
	return commitHashes
}

func (suite *RwApiTestSuite) clone(t testing.TB, cloneURL string, user *entities.User) (*cgit.CGit, error) {
	tmpDir := testutils.TempDir(t, "", "")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
	_, _, err := cg.Exec("clone", cloneURL, ".")
	if err != nil {
		return nil, err
	}

	return &cg, nil
}

type commitsOptions struct {
	id             string
	commitsCount   int
	initializeFunc func(t testing.TB, cg *cgit.CGit)
	generateFiles  func(t testing.TB, cg *cgit.CGit, commitIdx int) string
	filename       func(t testing.TB, idx int) string
}

func (suite *RwApiTestSuite) addCommits(t *testing.T, cloneURL string, user *entities.User, opts commitsOptions) []plumbing.Hash {
	cgc, err := suite.clone(t, cloneURL, user)
	require.NoError(t, err)

	ct, err := time.Parse(time.RFC822Z, "22 Aug 05 21:18 +0200")
	require.NoError(t, err)

	cg := cgc.WithCommitTime(ct)
	require.NoError(t, err)

	o := opts
	if o.commitsCount == 0 {
		o.commitsCount = 3
	}
	if o.filename == nil {
		o.filename = func(t testing.TB, idx int) string {
			return fmt.Sprintf("code_%s_%d.go", opts.id, idx)
		}
	}

	if o.generateFiles == nil {
		o.generateFiles = func(_ testing.TB, cg *cgit.CGit, idx int) string {
			fname := o.filename(t, idx)
			suite.makeNewFileWithContent(*cg, fname, fmt.Sprintf("// Some go code from test %s no %d\n// %s", t.Name(), idx, o.id))

			return fname
		}
	}
	if o.initializeFunc == nil {
		o.initializeFunc = func(_ testing.TB, cg *cgit.CGit) {}
	}

	o.initializeFunc(t, &cg)

	var commits []plumbing.Hash
	for i := 1; i <= o.commitsCount; i++ {
		msg := o.generateFiles(t, &cg, i)

		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", msg)
		cmt := cg.Must(t, "rev-parse", "HEAD")
		commits = append(commits, plumbing.NewHash(cmt))
		cg.Must(t, "push", "origin", "HEAD")
	}

	return commits
}

func (suite *RwApiTestSuite) addSomeLFSObjects(t *testing.T, cloneURL string, user *entities.User) {
	tmpDir := testutils.TempDir(t, "", "")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity)).WithAuthTokenSite(cloneURL)
	cg.Must(t, "clone", cloneURL, ".")

	cg.Must(t, "lfs", "install")
	cg.Must(t, "lfs", "track", "*.bin")

	for i := 0; i < 1; i++ {
		fname := fmt.Sprintf("code%d.bin", i)
		suite.makeNewFileWithContent(cg, fname, utils.MustMakeRandomString("Some go code", 32))
	}

	cg.Must(t, "add", "*")
	cg.Must(t, "commit", "-m", "Code with LFS objects")
	cg.Must(t, "push", "-u", "origin", "master")
}
