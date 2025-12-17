package integrationtests

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"

	"common/grpc"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/access/common"
	"gitcore/internal/adapters/ci"
	"gitcore/internal/entities"
	"gitcore/internal/entities/signals"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	pb_ci "private_api/generated/yandex/cloud/priv/ci/v1"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) getCIEventsWith(t *testing.T, fn func()) []*entities.AuthenticatedCIRequest {
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	suite.ciService.ClearSentEvents()

	fn()

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	events := suite.ciService.SentEvents()

	triggerType := func(a *entities.AuthenticatedCIRequest) int {
		if a.TriggerRequest != nil {
			switch a.TriggerRequest.Trigger.Payload.(type) {
			case *pb_ci.Trigger_Push:
				return 0
			case *pb_ci.Trigger_Pr:
				return 1
			default:
				return 2
			}
		}

		if a.ConfigurationRequest != nil {
			return 3
		}

		return 4
	}
	slices.SortFunc(events, func(a, b *entities.AuthenticatedCIRequest) int {
		tta := triggerType(a)
		ttb := triggerType(b)
		if tta != ttb {
			return cmp.Compare(tta, ttb)
		}
		return cmp.Compare(a.Time.Unix(), b.Time.Unix())
	})

	return events
}

func (suite *RwApiTestSuite) primitivePush(t *testing.T, user *entities.User, repo *entities.Repository, refName plumbing.ReferenceName, createBranch bool) plumbing.Hash {
	ctx := testutils.AuthorizeGRPC(user.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)
	uploadKey := suite.uploadPic(user.Identity)

	if createBranch {
		_, err := client.CreateBranch(ctx, &pb.CreateBranchRequest{
			Id:      grpc.MarshalID(repo.ID),
			Name:    refName.Short(),
			FromRev: repo.DefaultBranch,
		})
		require.NoError(t, err)
	}
	_, err := client.Commit(ctx, &pb.CommitRequest{
		Id:     grpc.MarshalID(repo.ID),
		Branch: refName.Short(),
		Actions: []*pb.CommitAction{
			{
				NewPath:   utils.PtrFromValue(fmt.Sprintf("tmp/%s.txt", uuid.NewString())),
				UploadKey: &uploadKey.Key,
			},
		},
		Message: "Commit",
	})
	require.NoError(t, err)

	ref, err := suite.RefRepoFactory.Build(repo.ID).GetReferenceByName(ctx, refName)
	require.NoError(t, err)

	return ref.Hash()
}

const DefaultOYaml = `
workflows:
  target-workflow:
    tasks: ci

on:
  push:
    - workflows: [target-workflow]
  pull_request:
    - workflows: [target-workflow]

tasks:
  - name: ci
    cubes:
      - name: some-name
        image: none
        script:
          - echo 1
`

const BrokenOYaml = `
codereview:
  need_ships: many

workflows:
  something: error

on:
  push:
    - workflows: [\"something\"]
  pull_request:
    - workflows: [\"something\"]
`

const FilteredOYaml = `
workflows:
  target-workflow-1:
    tasks: ci
  target-workflow-2:
    tasks: ci
  target-workflow-3:
    tasks: ci

on:
  push:
    - workflows: [target-workflow-1]
      filter:
        branches: [master]
    - workflows: [target-workflow-2]
      filter:
        tags: [release-*]
  pull_request:
    - workflows: [target-workflow-3]
      filter:
        target_branches: [master]

tasks:
  - name: ci
    cubes:
      - name: some-name
        image: none
        script:
          - echo 1
`

func (suite *RwApiTestSuite) addOYaml(repo *entities.Repository, branch, path, content string) *entities.Reference {
	// empty old config to check newer is preferred
	suite.mustBash(repo, fmt.Sprintf(`
		git checkout %s
		echo -n "" > %s
		mkdir %s
		echo "%s" > %s
		git add .
		git commit -m "oyaml"
	`, branch, oyaml.OldPath, oyaml.SourceCraftDirectory, content, path))

	head, err := suite.RefRepoFactory.Build(repo.ID).GetReferenceByName(
		context.Background(),
		plumbing.NewBranchReferenceName(branch),
	)
	require.NoError(suite.T(), err)

	return head
}

func (suite *RwApiTestSuite) addDefaultOYaml(repo *entities.Repository, branch, path string) *entities.Reference {
	return suite.addOYaml(repo, branch, path, DefaultOYaml)
}

func (suite *RwApiTestSuite) addBrokenOYaml(repo *entities.Repository, branch, path string) *entities.Reference {
	return suite.addOYaml(repo, branch, path, BrokenOYaml)
}

func (suite *RwApiTestSuite) TestCIService_InvalidOYaml() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		user := suite.users.Raichu
		repo := suite.repos.ListBranches
		suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

		t.Run("no oyaml", func(t *testing.T) {
			branch := plumbing.NewBranchReferenceName("aBranch")
			events := suite.getCIEventsWith(t, func() {
				_ = suite.primitivePush(t, user, repo, branch, false)
			})
			require.Empty(t, events)
		})

		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
		suite.addBrokenOYaml(repo, "main", configPath)
		suite.mustBash(repo, `git checkout aBranch && git rebase main`)

		suite.makePullRequest(user, &makePrOptions{
			Repo:   repo,
			Source: "aBranch",
			Target: "main",
		})

		t.Run("broken oyaml", func(t *testing.T) {
			branch := plumbing.NewBranchReferenceName("aBranch")
			events := suite.getCIEventsWith(t, func() {
				_ = suite.primitivePush(t, user, repo, branch, false)
			})
			require.Empty(t, events)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIService_HandleEvents() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		user := suite.users.Raichu
		repo := suite.repos.ListBranches
		suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
		suite.addRole(t, suite.users.Barash, repo, iam.Roles.RepositoriesDeveloper)
		suite.addRole(t, suite.users.Barash, suite.repos.ThreeDot, iam.Roles.RepositoriesDeveloper)

		mergeBase := suite.addDefaultOYaml(repo, "main", configPath)
		suite.mustBash(repo, `
		git checkout aBranch && git rebase main
		git checkout bBranch && git rebase main
		git checkout cBranch && git rebase main
		`)

		prA := suite.makePullRequest(suite.users.Barash, &makePrOptions{
			Repo:   repo,
			Source: "aBranch",
			Target: "main",
		})
		prB := suite.makePullRequest(suite.users.Barash, &makePrOptions{
			Repo:   repo,
			Source: "cBranch",
			Target: "main",
		})
		suite.makePullRequest(suite.users.Barash, &makePrOptions{
			Repo:   suite.repos.ThreeDot,
			Source: "branch",
			Target: "main",
		})

		t.Run("push updates branch", func(t *testing.T) {
			branch := plumbing.NewBranchReferenceName("aBranch")
			var hash plumbing.Hash
			events := suite.getCIEventsWith(t, func() {
				hash = suite.primitivePush(t, user, repo, branch, false)
			})
			require.Len(t, events, 2)

			t.Run("causes push trigger", func(t *testing.T) {
				require.NotNil(t, events[0].TriggerRequest)
				push := events[0].TriggerRequest.Trigger.GetPush()
				require.NotNil(t, push)
				require.NotNil(t, push.RefUpdate)

				require.Equal(t, "refs/heads/aBranch", push.RefUpdate.Ref)
				require.Equal(t, hash.String(), push.RefUpdate.AfterSha)
				require.Equal(t, mergeBase.Hash().String(), push.CompareTo)
			})

			t.Run("causes pull_request trigger", func(t *testing.T) {
				require.NotNil(t, events[1].TriggerRequest)
				require.Nil(t, events[1].TriggerRequest.Trigger.SourceRepository)
				pr := events[1].TriggerRequest.Trigger.GetPr()
				require.NotNil(t, pr)
				yarequire.ProtoEqual(t, pr.RefsUpdate, &pb_ci.PRRefs{
					SourceBranch: "aBranch",
					TargetBranch: "main",
					HeadSha:      hash.String(),
					MergeBaseSha: mergeBase.Hash().String(),
				})
			})
		})

		t.Run("push creates branch", func(t *testing.T) {
			newBranch := plumbing.NewBranchReferenceName("abacaba")
			var hash plumbing.Hash
			events := suite.getCIEventsWith(t, func() {
				hash = suite.primitivePush(t, user, repo, newBranch, true)
			})
			require.Len(t, events, 2) // first event is triggered by createBranch, second - by pushing a commit

			require.NotNil(t, events[1].TriggerRequest)
			push := events[1].TriggerRequest.Trigger.GetPush()
			require.NotNil(t, push)
			require.NotNil(t, push.RefUpdate)

			require.Equal(t, "refs/heads/abacaba", push.RefUpdate.Ref)
			require.Equal(t, hash.String(), push.RefUpdate.AfterSha)
			require.Equal(t, mergeBase.Hash().String(), push.CompareTo)
		})

		t.Run("push creates tag", func(t *testing.T) {
			master, err := suite.RefRepoFactory.Build(repo.ID).GetDefaultBranch(context.Background())
			require.NoError(t, err)

			events := suite.getCIEventsWith(t, func() {
				suite.mustBash(repo, `
				git tag version-1
				`)
			})
			require.Len(t, events, 1)

			require.NotNil(t, events[0].TriggerRequest)
			push := events[0].TriggerRequest.Trigger.GetPush()
			require.NotNil(t, push)
			require.NotNil(t, push.RefUpdate)

			require.Equal(t, "refs/tags/version-1", push.RefUpdate.Ref)
			require.Equal(t, master.Hash().String(), push.RefUpdate.AfterSha)
			require.Equal(t, master.Hash().String(), push.CompareTo) // TODO this may be a weird behaviour, maybe change to plumbing.ZeroHash?
		})

		var prC *entities.PullRequest
		t.Run("pr created", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				prC = suite.makePullRequest(suite.users.Barash, &makePrOptions{
					Repo:    repo,
					Source:  "cBranch",
					Target:  "aBranch",
					Publish: utils.PtrFromValue(true),
				})
			})
			require.Len(t, events, 1)

			require.NotNil(t, events[0].TriggerRequest)
			pr := events[0].TriggerRequest.Trigger.GetPr()
			require.NotNil(t, pr)
			require.NotNil(t, pr.RefsUpdate)

			yarequire.ProtoEqual(t, &pb_ci.PRStatusChange{
				Before: nil,
				After:  pb.PullRequest_OPEN,
			}, pr.StatusUpdate)
			require.Equal(t, grpc.MarshalID(prC.ID), pr.PrId)
			require.Equal(t, prC.HeadHash.String(), pr.RefsUpdate.HeadSha)
			require.Equal(t, mergeBase.Hash().String(), pr.RefsUpdate.MergeBaseSha)
		})

		t.Run("push deletes branch", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				cg, _ := suite.initCGit(suite.users.Barash, suite.repos.ThreeDot.FullSlug())
				cg.Must(t, "push", "-d", "origin", "branch")
			})
			// currently branch deletion doesn't trigger any events
			require.Empty(t, events)
		})

		t.Run("push updates branch with several PRs", func(t *testing.T) {
			branch := plumbing.NewBranchReferenceName("cBranch")
			var hash plumbing.Hash
			events := suite.getCIEventsWith(t, func() {
				hash = suite.primitivePush(t, user, repo, branch, false)
			})
			require.Len(t, events, 3)

			t.Run("causes push trigger", func(t *testing.T) {
				require.NotNil(t, events[0].TriggerRequest)
				push := events[0].TriggerRequest.Trigger.GetPush()
				require.NotNil(t, push)
				require.Equal(t, "refs/heads/cBranch", push.RefUpdate.Ref)
				require.Equal(t, hash.String(), push.RefUpdate.AfterSha)
				require.Equal(t, mergeBase.Hash().String(), push.CompareTo)
			})

			require.NotNil(t, events[1].TriggerRequest)
			require.NotNil(t, events[2].TriggerRequest)
			require.NotNil(t, events[1].TriggerRequest.Trigger.GetPr())
			require.NotNil(t, events[2].TriggerRequest.Trigger.GetPr())

			affectedPrs := []*pb_ci.PullRequestPayload{
				events[1].TriggerRequest.Trigger.GetPr(),
				events[2].TriggerRequest.Trigger.GetPr(),
			}
			sort.Slice(affectedPrs, func(i, j int) bool {
				return affectedPrs[i].PrId < affectedPrs[j].PrId
			})

			yarequire.ProtoEqual(t, affectedPrs[0], &pb_ci.PullRequestPayload{
				PrId:       grpc.MarshalID(prB.ID),
				PublicPrId: grpc.MarshalID(prB.PublicID),
				PrUuid:     prB.UUID.String(),
				RefsUpdate: &pb_ci.PRRefs{
					SourceBranch: "cBranch",
					TargetBranch: "main",
					HeadSha:      hash.String(),
					MergeBaseSha: mergeBase.Hash().String(),
				},
				StatusUpdate: &pb_ci.PRStatusChange{
					Before: utils.PtrFromValue(pb.PullRequest_OPEN),
					After:  pb.PullRequest_OPEN,
				},
			})
			yarequire.ProtoEqual(t, affectedPrs[1], &pb_ci.PullRequestPayload{
				PrId:       grpc.MarshalID(prC.ID),
				PublicPrId: grpc.MarshalID(prC.PublicID),
				PrUuid:     prC.UUID.String(),
				RefsUpdate: &pb_ci.PRRefs{
					SourceBranch: "cBranch",
					TargetBranch: "aBranch",
					HeadSha:      hash.String(),
					MergeBaseSha: mergeBase.Hash().String(),
				},
				StatusUpdate: &pb_ci.PRStatusChange{
					Before: utils.PtrFromValue(pb.PullRequest_OPEN),
					After:  pb.PullRequest_OPEN,
				},
			})
		})

		t.Run("status change discarded", func(t *testing.T) {
			client := pb.NewPRServiceClient(suite.grpcClient)
			ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)

			events := suite.getCIEventsWith(t, func() {
				_, err := client.Discard(ctx, &pb.DiscardRequest{
					Id: grpc.MarshalID(prA.ID),
				})
				require.NoError(t, err)
			})
			require.Empty(t, events)
		})

		t.Run("status change reopen", func(t *testing.T) {
			client := pb.NewPRServiceClient(suite.grpcClient)

			suite.addRole(suite.T(), suite.users.Barash, repo, iam.Roles.RepositoriesDeveloper)
			ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)
			var hash plumbing.Hash

			events := suite.getCIEventsWith(t, func() {
				hash = suite.primitivePush(t, suite.users.Barash, repo, plumbing.NewBranchReferenceName(prA.SourceBranch), false)
				_, err := client.Reopen(ctx, &pb.ReopenRequest{
					Id: grpc.MarshalID(prA.ID),
				})
				require.NoError(t, err)
			})
			require.Len(t, events, 2)

			require.NotNil(t, events[1].TriggerRequest)
			pr := events[1].TriggerRequest.Trigger.GetPr()
			require.NotNil(t, pr)
			require.NotNil(t, pr.StatusUpdate)
			require.NotNil(t, pr.RefsUpdate)
			require.Equal(t, hash.String(), pr.RefsUpdate.HeadSha)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIService_FilterWorkflows() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		ctx := context.Background()
		owner := suite.users.Kopatych

		repoID, _, _ := suite.makeRandomRepo(t, owner, 0)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)

		suite.ensureDefaultMaster(repo)
		suite.addOYaml(repo, "master", configPath, FilteredOYaml)
		suite.mustBash(repo, `git checkout -b branch`)

		t.Run("branch update hit", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("master"), false)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPush())
		})

		t.Run("branch update miss", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("branch"), false)
			})
			require.Empty(t, events)
		})

		t.Run("tag update hit", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.mustBash(repo, `
				git tag release-0.1
				`)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPush())
		})

		t.Run("tag update miss", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.mustBash(repo, `
				git tag dev-0.1
				`)
			})
			require.Empty(t, events)
		})

		t.Run("annotated tag update hit", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.mustBash(repo, `
				git tag -a release-0.2 -m "message"
				`)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPush())
		})

		t.Run("annotated tag update miss", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.mustBash(repo, `
				git tag -a dev-0.2 -m "message"
				`)
			})
			require.Empty(t, events)
		})

		suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("branch"), false)
		suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("branch-2"), true)

		t.Run("pr hit", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_ = suite.makePullRequest(owner, &makePrOptions{
					Repo:   repo,
					Source: "branch",
					Target: "master",
				})
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPr())
		})

		t.Run("pr miss", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_ = suite.makePullRequest(owner, &makePrOptions{
					Repo:   repo,
					Source: "branch-2",
					Target: "branch",
				})
			})
			require.Empty(t, events)
		})
	}
}

func (suite *RwApiTestSuite) TestCIService_CorrectOYaml() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		ctx := context.Background()
		owner := suite.users.Kopatych
		client := pb.NewCIServiceClient(suite.grpcClient)

		repoID, _, _ := suite.makeRandomRepo(t, owner, 0)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)

		suite.ensureDefaultMaster(repo)
		suite.addDefaultOYaml(repo, "master", configPath)
		suite.mustBash(repo, `git checkout -b branch`)
		suite.addBrokenOYaml(repo, "branch", configPath)

		t.Run("ref updates", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("branch"), false)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPush())
			require.Contains(t, string(events[0].TriggerRequest.Trigger.OyamlContent), DefaultOYaml)
		})

		var pr *entities.PullRequest
		t.Run("pull request create", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				pr = suite.makePullRequest(owner, &makePrOptions{
					Repo:   repo,
					Source: "branch",
					Target: "master",
				})
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPr())
			require.Contains(t, string(events[0].TriggerRequest.Trigger.OyamlContent), DefaultOYaml)
		})

		t.Run("pull request refresh", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				pr.HeadHash = suite.primitivePush(t, owner, repo, plumbing.NewBranchReferenceName("branch"), false)
			})
			require.Len(t, events, 2)
			require.NotNil(t, events[1].TriggerRequest)
			require.NotNil(t, events[1].TriggerRequest.Trigger.GetPr())
			require.Contains(t, string(events[0].TriggerRequest.Trigger.OyamlContent), DefaultOYaml)
		})

		t.Run("restart", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RestartWorkflow(testutils.AuthorizeGRPC(owner.Identity), &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     pr.HeadHash.String(),
					WorkflowName: "target-workflow",
					WorkflowId:   "1",
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetRestart())
			require.Contains(t, string(events[0].TriggerRequest.Trigger.OyamlContent), DefaultOYaml)
		})

		t.Run("restart all", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RestartAllWorkflows(testutils.AuthorizeGRPC(owner.Identity), &pb.RestartAllWorkflowsRequest{
					PrId:     grpc.MarshalID(pr.ID),
					HeadHash: pr.HeadHash.String(),
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPr())
			require.Contains(t, string(events[0].TriggerRequest.Trigger.OyamlContent), DefaultOYaml)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIService_ExtraFields() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		ctx := context.Background()
		owner := suite.users.Raichu
		user := suite.users.Kopatych
		client := pb.NewCIServiceClient(suite.grpcClient)

		repoID, _, _ := suite.makeRandomRepo(t, owner, 0)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)

		authenticator := testutils.NewStubAuthenticator(&user.Identity)
		subj := access.UserSubject(user.Identity, authenticator)
		subj.SetUserModel(user)

		deps, err := suite.RepoService.ResolveRepoDeps(ctx, subj, repo)
		require.NoError(t, err)

		suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
		suite.addRole(t, user, repo, iam.Roles.Admin)

		suite.ensureDefaultMaster(repo)
		suite.addDefaultOYaml(repo, "master", configPath)
		suite.mustBash(repo, `git checkout -b branch`)

		workflowInputs := &pb_ci.Inputs{
			WorkflowInputs: []*pb_ci.WorkflowInput{
				{
					WorkflowName: "target-workflow",
					Values: []*pb_ci.InputValue{
						{
							Name:  "input-var-name",
							Value: "input-var-value",
						},
					},
				},
			}}

		requireExtraFields := func(t *testing.T, trigger *pb_ci.Trigger) {
			require.NotNil(t, trigger.OyamlContent)

			if trigger.GetPr() != nil {
				require.NotEmpty(t, trigger.GetPr().PublicPrId)
			}
			require.NotNil(t, trigger.Organization)

			yarequire.ProtoCmp(t, &pb_ci.Repository{
				Id:       repoID,
				RepoId:   grpc.MarshalID(repoID),
				Uuid:     grpc.MarshalUUID(repo.UUID),
				OrgSlug:  repo.OrgSlug,
				RepoSlug: repo.Slug,
				Owner: &pb_ci.User{
					UserId:   grpc.MarshalID(owner.ID),
					UserUuid: grpc.MarshalUUID(owner.UUID),
					Identity: &pb_ci.UserIdentity{
						Src: string(owner.Identity.Src),
						Id:  owner.Identity.ID,
					},
					DisplayName: owner.DisplayName,
					Slug:        owner.Username,
				},
				Visibility: ci.EntityToProto.Visibility(deps.EffectiveVisibility),
			}, trigger.Repository,
				protocmp.IgnoreFields(&pb_ci.Repository{}, "https_url"),
				protocmp.IgnoreFields(&pb_ci.Repository{}, "ssh_url"),
				protocmp.IgnoreFields(&pb_ci.Repository{}, "https_url_external"),
			)

			yarequire.ProtoEqual(t, &pb_ci.User{
				UserId:   grpc.MarshalID(user.ID),
				UserUuid: grpc.MarshalUUID(user.UUID),
				Identity: &pb_ci.UserIdentity{
					Src: string(user.Identity.Src),
					Id:  user.Identity.ID,
				},
				DisplayName: user.DisplayName,
				Slug:        user.Username,
			}, trigger.Initiator)

			require.Equal(t, suite.cfg.Platform.FrontendHost, trigger.FrontendHost)

			if trigger.Inputs != nil {
				require.Equal(t, trigger.Inputs, workflowInputs)
			}
		}

		t.Run("ref updates", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.primitivePush(t, user, repo, plumbing.Master, false)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPush())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		var pr *entities.PullRequest
		t.Run("pull request create", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				pr = suite.makePullRequest(user, &makePrOptions{
					Repo:   repo,
					Source: "branch",
					Target: "master",
				})
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPr())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		t.Run("pull request refresh", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				pr.HeadHash = suite.primitivePush(t, user, repo, plumbing.NewBranchReferenceName("branch"), false)
			})
			require.Len(t, events, 2)
			require.NotNil(t, events[1].TriggerRequest)
			require.NotNil(t, events[1].TriggerRequest.Trigger.GetPr())
			requireExtraFields(t, events[1].TriggerRequest.Trigger)
		})

		t.Run("manual", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RunWorkflows(testutils.AuthorizeGRPC(user.Identity), &pb.RunWorkflowsRequest{
					RepoId:        grpc.MarshalID(repo.ID),
					HeadRef:       "master",
					WorkflowNames: []string{"target-workflow"},
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetManualRun())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		t.Run("manual with inputs", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RunWorkflows(testutils.AuthorizeGRPC(user.Identity), &pb.RunWorkflowsRequest{
					RepoId:        grpc.MarshalID(repo.ID),
					HeadRef:       "master",
					WorkflowNames: []string{"target-workflow"},
					Inputs: &pb.CIInputs{
						WorkflowInputs: []*pb.WorkflowInputs{
							{
								WorkflowName: "target-workflow",
								Values: []*pb.CIInputValue{
									{
										Name:  "input-var-name",
										Value: "input-var-value",
									},
								},
							},
						},
					},
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetManualRun())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		t.Run("restart", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RestartWorkflow(testutils.AuthorizeGRPC(user.Identity), &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     pr.HeadHash.String(),
					WorkflowName: "target-workflow",
					WorkflowId:   "1",
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetRestart())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		t.Run("restart outdated", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				resp, err := client.RestartWorkflow(testutils.AuthorizeGRPC(user.Identity), &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     plumbing.ZeroHash.String(),
					WorkflowName: "target-workflow",
					WorkflowId:   "1",
				})
				require.NoError(t, err)
				require.True(t, resp.IsOutdated)
			})
			require.Empty(t, events)
		})

		t.Run("restart invalid workflow", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				resp, err := client.RestartWorkflow(testutils.AuthorizeGRPC(user.Identity), &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     plumbing.ZeroHash.String(),
					WorkflowName: "some-other-workflow",
					WorkflowId:   "1",
				})
				require.NoError(t, err)
				require.True(t, resp.IsOutdated)
			})
			require.Empty(t, events)
		})

		t.Run("restart all", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				_, err := client.RestartAllWorkflows(testutils.AuthorizeGRPC(user.Identity), &pb.RestartAllWorkflowsRequest{
					PrId:     grpc.MarshalID(pr.ID),
					HeadHash: pr.HeadHash.String(),
				})
				require.NoError(t, err)
			})
			require.NotEmpty(t, events)
			require.NotNil(t, events[0].TriggerRequest)
			require.NotNil(t, events[0].TriggerRequest.Trigger.GetPr())
			requireExtraFields(t, events[0].TriggerRequest.Trigger)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIService_MergeResult() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		user := suite.users.Kopatych
		repo := suite.repos.Alpha
		client := pb.NewPRServiceClient(suite.grpcClient)

		suite.addRole(t, user, repo, iam.Roles.Admin)
		suite.addDefaultOYaml(repo, "branch", configPath)

		for _, test := range []struct {
			name         string
			expectedUser entities.UserIdentity
		}{
			{
				name:         "merge ok",
				expectedUser: user.Identity,
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				_ = suite.primitivePush(t, user, repo, plumbing.NewBranchReferenceName("branch"), false)
				pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
					Repo:    repo,
					Source:  "branch",
					Target:  "master",
					Publish: utils.PtrFromValue(true),
				})

				events := suite.getCIEventsWith(t, func() {
					_, err := client.Merge(testutils.AuthorizeGRPC(user.Identity), &pb.MergeRequest{
						PrId:  grpc.MarshalID(pr.ID),
						Force: true,
					})
					require.NoError(t, err)
					suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)
				})
				require.Len(t, events, 2)

				// merge commit
				require.NotNil(t, events[0].TriggerRequest)
				pushInfo := events[0].TriggerRequest.Trigger.GetPush()
				require.NotNil(t, pushInfo)
				require.NotEqual(t, pr.HeadHash, pushInfo.RefUpdate.AfterSha)
				require.Contains(t, events[0].AccessToken, test.expectedUser.ID)

				require.NotNil(t, events[1].ConfigurationRequest)
				require.Equal(t, events[0].TriggerRequest.Trigger.Organization, events[1].ConfigurationRequest.Update.Organization)
				require.Equal(t, events[0].TriggerRequest.Trigger.Repository, events[1].ConfigurationRequest.Update.Repository)
				require.Equal(t, events[0].TriggerRequest.Trigger.Initiator, events[1].ConfigurationRequest.Update.User)
				require.Equal(t, events[0].TriggerRequest.Trigger.OyamlContent, events[1].ConfigurationRequest.Update.Content)
				require.Equal(t, pushInfo.RefUpdate, events[1].ConfigurationRequest.Update.RefUpdate)
				require.Contains(t, events[1].AccessToken, test.expectedUser.ID)
			})
		}

		t.Run("merge failed", func(t *testing.T) {
			ctx := context.Background()
			pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
				Repo:    repo,
				Source:  "branch",
				Target:  "master",
				Publish: utils.PtrFromValue(true),
			})

			events := suite.getCIEventsWith(t, func() {
				// fake merging because we need to squeeze in at least one push WHILE it's merging
				require.NoError(t, suite.PullRequestRepo.UpdateTable(ctx, pr.ID, map[string]interface{}{
					"status": entities.PRStatuses.Merging,
				}))
				pr.Status = entities.PRStatuses.Merging

				suite.mustBash(repo, `
				git checkout branch
				echo t > t.txt
				git add .
				git commit -m "T"
				`)

				idx := suite.users.Kopatych.Identity
				require.NoError(t, suite.PullRequestService.OnMergeFailed(ctx, pr, "error", signals.SignalCallerPayload{
					UserID:        suite.users.Kopatych.ID,
					Authenticator: common.NewIAMTokenAuthenticator(testutils.FakeIAMAuthToken(idx), &idx).MarshalToStruct(),
				}))
			})
			require.Len(t, events, 2)

			require.NotNil(t, events[0].TriggerRequest)
			pushInfo := events[0].TriggerRequest.Trigger.GetPush()
			require.NotNil(t, pushInfo)

			require.NotNil(t, events[1].TriggerRequest)
			prInfo := events[1].TriggerRequest.Trigger.GetPr()
			require.NotNil(t, prInfo)
			require.NotNil(t, prInfo.RefsUpdate)
			require.NotEqual(t, pr.HeadHash.String(), prInfo.RefsUpdate.HeadSha)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIService_AuthenticatorType() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		repo := suite.repos.ListBranches
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.Admin)

		_ = suite.addDefaultOYaml(repo, "main", configPath)
		_ = suite.makePullRequest(suite.users.Barash, &makePrOptions{
			Repo:   suite.repos.ListBranches,
			Source: "aBranch",
			Target: "main",
		})

		t.Run("via http (iam)", func(t *testing.T) {
			protocol := suite.HTTPSProtocol()
			tmpDir := testutils.TempDir(t, "", "")
			w := testutils.NewWorkdir(t, tmpDir)
			cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
			cg.Must(t, "clone", protocol.RepoURL(repo.OrgSlug, repo.Slug), "repo-iam")

			events := suite.getCIEventsWith(t, func() {
				cg := protocol.PrepareCGit(w.ChildDir("repo-iam"), testutils.UserIdentities.Kopatych)
				cg.Must(t, "checkout", "aBranch")
				require.NoError(t, cg.BashNoCapture(fmt.Sprintf("echo %s > ci_data.txt", uuid.New())))
				cg.Must(t, "add", ".")
				cg.Must(t, "commit", "-m", "'new file'")
				cg.Must(t, "push", "--all", "-f")
			})

			require.Len(t, events, 2)
			for _, event := range events {
				require.Contains(t, event.AccessToken, testutils.StubFakePrefix)
			}
		})

		t.Run("via http (pat)", func(t *testing.T) {
			_, pk, err := suite.PatService.Create(
				context.Background(),
				suite.users.Kopatych,
				nil,
				entities.PAT{
					Scope: entities.Scope{
						Type: entities.ScopeTypes.Unbound,
						Role: &iam.Roles.RepositoriesDeveloper,
						Objects: []entities.IAMObject{{
							Type: entities.ObjectTypes.Repository,
							ID:   repo.ID,
						}}},
					PATParams: entities.PATParams{
						Name: "test pat",
					},
				},
				true)
			require.NoError(t, err)

			protocol := suite.HTTPSProtocol()
			tmpDir := testutils.TempDir(t, "", "")
			w := testutils.NewWorkdir(t, tmpDir)
			cg := w.CGit().WithAuthToken(pk)
			cg.Must(t, "clone", protocol.RepoURL(repo.OrgSlug, repo.Slug), "repo-pat")

			events := suite.getCIEventsWith(t, func() {
				cg := w.ChildDir("repo-pat").CGit().WithAuthToken(pk)
				cg.Must(t, "checkout", "aBranch")
				require.NoError(t, cg.BashNoCapture(fmt.Sprintf("echo %s > ci_data.txt", uuid.New())))
				cg.Must(t, "add", ".")
				cg.Must(t, "commit", "-m", "'new file'")
				cg.Must(t, "push", "--all", "-f")
			})

			require.Len(t, events, 2)
			for _, event := range events {
				require.Contains(t, event.AccessToken, pk)
			}
		})

		t.Run("via ssh", func(t *testing.T) {
			t.Skip("This wont work till OO-3971 is implemented")
			protocol := suite.SSHProtocol()
			tmpDir := testutils.TempDir(t, "", "")
			w := testutils.NewWorkdir(t, tmpDir)
			cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
			cg.Must(t, "clone", suite.GetSSHRepoURLByRepoID(repo.ID), "repo-ssh")

			events := suite.getCIEventsWith(t, func() {
				cg := protocol.PrepareCGit(w.ChildDir("repo-ssh"), testutils.UserIdentities.Kopatych)
				cg.Must(t, "checkout", "aBranch")
				require.NoError(t, cg.BashNoCapture("echo something > ci_data_2.txt"))
				cg.Must(t, "add", ".")
				cg.Must(t, "commit", "-m", "'new file 2'")
				cg.Must(t, "push", "--all", "-f")
			})

			require.Len(t, events, 2)
		})

		t.Run("via interface", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				suite.primitivePush(t, suite.users.Kopatych, repo, plumbing.NewBranchReferenceName("aBranch"), false)
			})

			require.Len(t, events, 2)
			for _, event := range events {
				require.Contains(t, event.AccessToken, testutils.StubFakePrefix)
			}
		})

		suite.AfterTest("", "")
	}
}

// Tests that any new yaml path prevents old config usage
func (suite *RwApiTestSuite) TestCIService_ConfigClash() {
	t := suite.T()
	user := suite.users.Raichu
	repo := suite.repos.ListBranches
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

	events := suite.getCIEventsWith(t, func() {
		suite.addDefaultOYaml(repo, "main", oyaml.OldPath)
	})
	require.Len(t, events, 2)

	events = suite.getCIEventsWith(t, func() {
		suite.addDefaultOYaml(repo, "main", oyaml.ReviewPath)
	})
	require.Len(t, events, 0)

	events = suite.getCIEventsWith(t, func() {
		suite.addDefaultOYaml(repo, "main", oyaml.CIPath)
	})
	require.Len(t, events, 2)
}
