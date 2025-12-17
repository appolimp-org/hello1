package integrationtests

import (
	"common/grpc"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/adapters/ci"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/messages"
	"gitcore/internal/testutils"
	pb_ci "private_api/generated/yandex/cloud/priv/ci/v1"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestMergeChecks_WebSocket() {
	t := suite.T()
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{})
	repo := suite.repos.Alpha
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	t.Run("conflicts update", func(t *testing.T) {
		require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))
		suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrMergeChecksCollection)
	})

	t.Run("review update", func(t *testing.T) {
		suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesMaintainer)
		require.NoError(t, suite.ClearWebSocketRequests())

		client := pb.NewPRReviewersServiceClient(suite.grpcClient)

		_, err := client.Update(ctx, &pb.UpdateReviewersRequest{
			PrId: grpc_marshalling.IDInverse(pr.ID),
			ReviewerDeltas: []*pb.ReviewerDelta{
				{
					Action: pb.DeltaAction_ADD,
					UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
				},
			},
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		_, err = client.SetDecision(ctx, &pb.SetDecisionRequest{
			PrId:           grpc_marshalling.IDInverse(pr.ID),
			ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
		})
		require.NoError(t, err)

		suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrMergeChecksCollection)
	})
}

func (suite *RwApiTestSuite) TestMergeChecks_EmptyFlux() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
		suite.addDefaultOYaml(suite.repos.Alpha, "master", configPath)

		client := pb.NewPRServiceClient(suite.grpcClient)
		pr := suite.makePullRequest(suite.users.Kopatych, nil)

		fluxService, ok := suite.fluxService.(*ci.StubFluxService)
		require.True(t, ok)
		fluxService.Fluxes[pr.HeadHash] = []*pb_ci.Flux{
			{
				Status: pb_ci.TaskStatus_TS_CREATED,
				Dates: &pb_ci.DatesByStage{
					CreatedAt: grpc.TimeToProtocTs(time.Now()),
				},
				Workflows: []*pb_ci.Workflow{},
				EventType: pb_ci.EventType_ET_PR_UPDATE,
				PrId:      grpc.MarshalID(pr.ID),
				PublicId:  100,
				Metadata:  nil,
			},
		}

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		require.Len(t, checks.MergeChecks.CiWorkflows, 1)
		require.Equal(t, messages.MsgMergeCIStartingUp.MessageID, checks.MergeChecks.CiWorkflows[0].Check.DisplayMessage.MessageId)

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestMergeChecks_NoOYaml() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
		PrId: grpc.MarshalID(pr.ID),
	})
	require.NoError(t, err)

	// yarequire.ProtoDumpFixture(t, checks)
	yarequire.ProtoCompareWithFixture(t, checks)
}

func (suite *RwApiTestSuite) TestMergeChecks_CorruptOYaml() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		repo := suite.repos.Alpha

		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
		suite.addBrokenOYaml(repo, "master", configPath)

		client := pb.NewPRServiceClient(suite.grpcClient)
		pr := suite.makePullRequest(suite.users.Kopatych, nil)

		require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		wfs := checks.MergeChecks.CiWorkflows
		require.Len(t, wfs, 1)
		require.Equal(t, except.ConfigAsCodeIsCorrupt.MessageID, wfs[0].Check.DisplayMessage.MessageId)

		if configPath == oyaml.CIPath {
			require.Equal(t, pb.MergeCheck_SUCCESS, checks.MergeChecks.CodeReview.Check.Status)
		} else if configPath == oyaml.OldPath {
			require.Equal(t, except.ConfigAsCodeIsCorrupt.MessageID, checks.MergeChecks.CodeReview.Check.DisplayMessage.MessageId)
		}
		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestMergeChecks_OnlySourceBranch() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		client := pb.NewPRServiceClient(suite.grpcClient)

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		repo := suite.repos.Alpha
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

		suite.mustBash(repo, fmt.Sprintf(`
		git checkout -b target master
		echo -n "" > %s
		mkdir %s
		echo "
workflows:
  target-workflow:
    tasks: ci

on:
  pull_request:
    - workflows: target-workflow
      filter:
        source_branches: [\"master\"]

tasks:
  - name: ci
    cubes:
      - name: some-name
        image: none
        script:
          - make something
" > %s
		git add .
		git commit -m "Added ci.yaml"
	`, oyaml.OldPath, oyaml.SourceCraftDirectory, configPath))
		suite.mustBash(repo, `
		git checkout -b source master
		echo source > source.txt
		git add .
		git commit -m "Source changes"
	`)

		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Source: "source",
			Target: "master",
		})

		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Empty(t, checks.MergeChecks.CiWorkflows)

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestMergeChecks_DeletedBranch() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		ctx := context.Background()
		repo := suite.repos.Alpha
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

		suite.addDefaultOYaml(repo, "master", configPath)
		suite.mustBash(repo, `
		git checkout -b source master
		echo "some changes" > changelog.txt
		git add .
		git commit -m "Changes"
	`)

		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Repo:   repo,
			Source: "source",
			Target: "master",
		})
		fluxService, ok := suite.fluxService.(*ci.StubFluxService)
		require.True(t, ok)
		fluxService.Fluxes[pr.HeadHash] = []*pb_ci.Flux{
			{
				Status: pb_ci.TaskStatus_TS_SUCCESS,
				Dates: &pb_ci.DatesByStage{
					CreatedAt: grpc.TimeToProtocTs(time.Now()),
				},
				Workflows: []*pb_ci.Workflow{
					{
						Name:   "target-workflow",
						Status: pb_ci.TaskStatus_TS_SUCCESS,
					},
				},
				EventType: pb_ci.EventType_ET_PR_UPDATE,
				PrId:      grpc.MarshalID(pr.ID),
				PublicId:  100,
			},
		}

		refRepo := suite.RefRepoFactory.Build(repo.ID)
		require.NoError(t, refRepo.RemoveRef(ctx, plumbing.NewBranchReferenceName("source")))

		client := pb.NewPRServiceClient(suite.grpcClient)
		checks, err := client.ListMergeChecks(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		require.Len(t, checks.MergeChecks.CiWorkflows, 1)
		require.Equal(t, pb.MergeCheck_SUCCESS, checks.MergeChecks.CiWorkflows[0].Check.Status)
		require.Equal(t, "100", checks.MergeChecks.CiWorkflows[0].PublicFluxId)
		require.Equal(t, pb.MergeCheck_SUCCESS, checks.MergeChecks.CodeReview.Check.Status)

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestMergeChecks_CIException() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		repo := suite.repos.Alpha
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

		suite.addDefaultOYaml(repo, "master", configPath)
		suite.mustBash(repo, `
		git checkout -b source master
		echo "some changes" > changelog.txt
		git add .
		git commit -m "Changes"
		`)

		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Repo:   repo,
			Source: "source",
			Target: "master",
		})
		fluxService, ok := suite.fluxService.(*ci.StubFluxService)
		require.True(t, ok)
		fluxService.Fluxes[pr.HeadHash] = []*pb_ci.Flux{
			{
				Status: pb_ci.TaskStatus_TS_SUCCESS,
				Dates: &pb_ci.DatesByStage{
					CreatedAt: grpc.TimeToProtocTs(time.Now()),
				},
				Workflows: []*pb_ci.Workflow{
					{
						Name:   "other-workflow",
						Status: pb_ci.TaskStatus_TS_SUCCESS,
					},
				},
				EventType: pb_ci.EventType_ET_PR_UPDATE,
				PrId:      grpc.MarshalID(pr.ID),
				PublicId:  100,
			},
		}

		client := pb.NewPRServiceClient(suite.grpcClient)
		checks, err := client.ListMergeChecks(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		// See https://st.yandex-team.ru/OO-4471, missing and unexpected workflows are not treated as
		//  exceptions for now, since we don't support auto-refresh on a CI config change
		// require.Len(t, checks.MergeChecks.CiWorkflows, 1)
		// workflow := checks.MergeChecks.CiWorkflows[0]
		// require.Empty(t, workflow.WorkflowId)
		// require.Equal(t, "CI", workflow.Check.Name)
		// require.Equal(t, messages.MsgMergeCIWorkflowNotCreated.MessageID, workflow.Check.DisplayMessage.MessageId)

		// yarequire.ProtoDumpFixture(t, checks)
		yarequire.ProtoCompareWithFixture(t, checks, protocmp.IgnoreFields(&pb.MergeChecks{}, "conflicts"))

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestConfigValidation() {
	t := suite.T()

	tests := []struct {
		name      string
		setupBash string
	}{
		{
			name: "NoChangedConfigs",
			setupBash: `
				git checkout -b source master
				echo "readme" > README.md
				git add .
				git commit -m "Add readme"
			`,
		},
		{
			name: "IgnoreCI",
			setupBash: fmt.Sprintf(`
				git checkout -b source master
				mkdir -p %s
				echo "invalid: [yaml" > %s
				git add .
				git commit -m "Add invalid ci.yaml"
			`, oyaml.SourceCraftDirectory, oyaml.CIPath),
		},
		{
			name: "ValidConfig",
			setupBash: fmt.Sprintf(`
				git checkout -b source master
				mkdir -p %s
				cat > %s << 'EOF'
branch_protection:
  policies:
    - target: branch
      matches: "master"
      rules:
        - prevent_deletion
        - prevent_force_push
        - prevent_non_pr_changes
EOF
				git add .
				git commit -m "Add valid branch.yaml"
			`, oyaml.SourceCraftDirectory, oyaml.BranchPolicyPath),
		},
		{
			name: "InvalidConfig",
			setupBash: fmt.Sprintf(`
				git checkout -b source master
				mkdir -p %s
				cat > %s << 'EOF'
webhooks:
  hooks:
    - slug: wh1
      name: ""
      description: "Triggers on main branch pushes"
      secret: "my-secret"
      ssl_verification: true
      active: true
on:
  push:
    - hooks: ["wh2"]
EOF
				git add .
				git commit -m "Add broken webhook.yaml"
			`, oyaml.SourceCraftDirectory, oyaml.WebhooksPath),
		},
		{
			name: "MultipleConfigs",
			setupBash: fmt.Sprintf(`
				git checkout -b source master
				mkdir -p %s
				cat > %s << 'EOF'
branch_protection:
  policies:
    - target: branch
      matches: "master"
      rules:
        - prevent_deletion
        - prevent_force_push
        - prevent_non_pr_changes
EOF
				echo "codereview: invalid" > %s
				git add .
				git commit -m "Add configs"
			`, oyaml.SourceCraftDirectory, oyaml.BranchPolicyPath, oyaml.ReviewPath),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			repo := suite.repos.Alpha

			suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
			suite.mustBash(repo, tt.setupBash)

			pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
				Repo:   repo,
				Source: "source",
				Target: "master",
			})

			suite.WaitForWorkflows(t, entities.WorkflowTypes.ConfigValidation)

			client := pb.NewPRServiceClient(suite.grpcClient)
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

			checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
				PrId: grpc.MarshalID(pr.ID),
			})
			require.NoError(t, err)

			// yarequire.ProtoDumpFixture(t, checks)
			yarequire.ProtoCompareWithFixture(t, checks,
				protocmp.IgnoreFields(&pb.MergeChecks{}, "code_review", "conflicts", "ci_workflows", "neuro_review"),
			)

			suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrMergeChecksCollection)
		})
	}
}

func (suite *RwApiTestSuite) TestConfigValidation_NewIteration() {
	t := suite.T()
	repo := suite.repos.Alpha

	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

	// Create branch with invalid config
	suite.mustBash(repo, fmt.Sprintf(`
		git checkout -b source master
		mkdir -p %s
		echo "invalid: [yaml" > %s
		git add .
		git commit -m "Add broken branch.yaml"
	`, oyaml.SourceCraftDirectory, oyaml.BranchPolicyPath))

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "source",
		Target: "master",
	})

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	t.Run("first check should be failure", func(t *testing.T) {
		suite.WaitForWorkflows(t, entities.WorkflowTypes.ConfigValidation)

		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		// yarequire.ProtoDumpFixture(t, checks)
		yarequire.ProtoCompareWithFixture(t, checks,
			protocmp.IgnoreFields(&pb.MergeChecks{}, "code_review", "conflicts", "ci_workflows", "neuro_review"),
		)
	})

	suite.mustBash(repo, fmt.Sprintf(`
		git checkout source
        cat > %s << 'EOF'
branch_protection:
  policies:
    - target: branch
      matches: "master"
      rules:
        - prevent_deletion
        - prevent_force_push
        - prevent_non_pr_changes
EOF
		git add .
		git commit -m "Fix branches.yaml"
	`, oyaml.BranchPolicyPath))

	t.Run("second check should be success", func(t *testing.T) {
		suite.WaitForWorkflows(t, entities.WorkflowTypes.ConfigValidation)

		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		// yarequire.ProtoDumpFixture(t, checks)
		yarequire.ProtoCompareWithFixture(t, checks,
			protocmp.IgnoreFields(&pb.MergeChecks{}, "code_review", "conflicts", "ci_workflows", "neuro_review"),
		)
	})
}

func (suite *RwApiTestSuite) TestMergeChecks_UnresolvedCommentsBlocking() {
	t := suite.T()

	repo := suite.repos.Alpha
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesMaintainer)

	// Create a PR
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo: repo,
	})

	require.NoError(t, suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), pr.ID))

	client := pb.NewPRServiceClient(suite.grpcClient)
	reviewClient := pb.NewPRReviewersServiceClient(suite.grpcClient)
	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	ctxKrosh := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	suite.addFile(repo, "master", oyaml.ReviewPath, []byte(`
codereview:
  need_ships: 1
`))

	t.Run("code review initially in progress", func(t *testing.T) {
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, pb.MergeCheck_IN_PROGRESS, checks.MergeChecks.CodeReview.Check.Status)
	})

	t.Run("approve PR to get to Success state", func(t *testing.T) {
		_, err := reviewClient.SetDecision(ctxKrosh, &pb.SetDecisionRequest{
			PrId:           grpc.MarshalID(pr.ID),
			ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
		})
		require.NoError(t, err)

		// Verify code review is now successful
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, pb.MergeCheck_SUCCESS, checks.MergeChecks.CodeReview.Check.Status)
	})

	var commentID string
	t.Run("create unresolved comment blocks merge", func(t *testing.T) {
		resp, err := commentClient.Create(ctxKrosh, &pb.CreateCommentRequest{
			PrId:                grpc.MarshalID(pr.ID),
			Body:                "This needs to be fixed before merging",
			Publish:             true,
			NeedResolution:      true,
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		comment := testutils.UnmarshalGrpcResult[*pb.PRComment](t, resp)
		commentID = comment.Id

		// Verify code review is now blocked by unresolved comments
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, pb.MergeCheck_FAILURE, checks.MergeChecks.CodeReview.Check.Status)
		require.Equal(t, messages.MsgCodeReviewUnresolvedComments.MessageID, checks.MergeChecks.CodeReview.Check.DisplayMessage.MessageId)
	})

	t.Run("resolving comment unblocks merge", func(t *testing.T) {
		require.NotEmpty(t, commentID)

		_, err := commentClient.Update(ctxKrosh, &pb.UpdateCommentRequest{
			PrId:       grpc.MarshalID(pr.ID),
			CommentId:  commentID,
			IsResolved: utils.PtrFromValue(true),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
		})
		require.NoError(t, err)

		// Verify code review is back to successful after resolving comments
		checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, pb.MergeCheck_SUCCESS, checks.MergeChecks.CodeReview.Check.Status)
		require.Nil(t, checks.MergeChecks.CodeReview.Check.DisplayMessage)
	})
}
