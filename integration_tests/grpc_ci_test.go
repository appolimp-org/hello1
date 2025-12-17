package integrationtests

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"common/grpc"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestCIHandler_ManualRun() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		client := pb.NewCIServiceClient(suite.grpcClient)
		repo := suite.repos.Alpha
		suite.addRole(suite.T(), suite.users.Kopatych, repo, iam.Roles.Admin)

		resolvedMasterRef := suite.addDefaultOYaml(repo, "master", configPath)
		resolvedNoNameRef := suite.addDefaultOYaml(repo, "branch", configPath)

		resolvedMaster := resolvedMasterRef.Hash()
		resolvedNoName := resolvedNoNameRef.Hash()
		resolvedBranch := suite.primitivePush(suite.T(), suite.users.Admin, repo, plumbing.NewBranchReferenceName("branch"), false)

		for _, test := range []struct {
			name           string
			headRef        string
			taskDefRef     *string
			workflowNames  []string
			resolvedHead   string
			resolveTaskDef string
		}{
			{
				name:           "default in master head",
				headRef:        "master",
				workflowNames:  []string{"target-workflow"},
				resolvedHead:   resolvedMaster.String(),
				resolveTaskDef: resolvedMaster.String(),
			},
			{
				name:           "several workflows",
				headRef:        "master",
				taskDefRef:     utils.PtrFromValue("branch"),
				workflowNames:  []string{"target-workflow"},
				resolvedHead:   resolvedMaster.String(),
				resolveTaskDef: resolvedBranch.String(),
			},
			{
				name:           "refs as commits",
				headRef:        resolvedMaster.String(),
				taskDefRef:     utils.PtrFromValue(resolvedNoName.String()),
				workflowNames:  []string{"target-workflow"},
				resolvedHead:   resolvedMaster.String(),
				resolveTaskDef: resolvedNoName.String(),
			},
		} {
			suite.T().Run(test.name, func(t *testing.T) {
				events := suite.getCIEventsWith(t, func() {
					ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
					_, err := client.RunWorkflows(ctx, &pb.RunWorkflowsRequest{
						RepoId:        grpc.MarshalID(repo.ID),
						HeadRef:       test.headRef,
						TaskDefRef:    test.taskDefRef,
						WorkflowNames: test.workflowNames,
					})
					require.NoError(t, err)
				})

				require.Len(t, events, 1)
				require.NotNil(t, events[0].TriggerRequest)
				payload := events[0].TriggerRequest.Trigger.GetManualRun()
				require.NotNil(t, payload)
				require.Equal(t, test.resolvedHead, payload.HeadSha)
				require.Equal(t, test.resolveTaskDef, payload.TaskDefSha)
				require.Equal(t, test.workflowNames, payload.WorkflowNames)
			})
		}

		suite.T().Run("permission denied", func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Raichu)
			_, err := client.RunWorkflows(ctx, &pb.RunWorkflowsRequest{
				RepoId:        grpc.MarshalID(repo.ID),
				HeadRef:       "master",
				WorkflowNames: []string{"workflow"},
			})
			yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
		})

		suite.T().Run("workflow name not found", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				_, err := client.RunWorkflows(ctx, &pb.RunWorkflowsRequest{
					RepoId:        grpc.MarshalID(repo.ID),
					HeadRef:       "master",
					WorkflowNames: []string{"other-workflow"},
				})
				yarequire.ProtoStatusEqual(t, codes.NotFound, err)
			})
			require.Empty(t, events)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIHandler_Restart() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		client := pb.NewCIServiceClient(suite.grpcClient)
		repo := suite.repos.Alpha
		suite.addRole(suite.T(), suite.users.Kopatych, repo, iam.Roles.Admin)

		_ = suite.addDefaultOYaml(repo, "master", configPath)
		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{})

		suite.T().Run("ok", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				_, err := client.RestartWorkflow(ctx, &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     pr.HeadHash.String(),
					WorkflowId:   "1",
					WorkflowName: "target-workflow",
				})
				require.NoError(t, err)
			})

			require.Len(t, events, 1)
			require.NotNil(t, events[0].TriggerRequest)
			payload := events[0].TriggerRequest.Trigger.GetRestart()
			require.NotNil(t, payload)
			require.Equal(t, "1", payload.WorkflowId)
		})

		suite.T().Run("unauthenticated", func(t *testing.T) {
			ctx := context.Background()
			_, err := client.RestartWorkflow(ctx, &pb.RestartWorkflowRequest{
				PrId:         grpc.MarshalID(pr.ID),
				HeadHash:     pr.HeadHash.String(),
				WorkflowId:   "1",
				WorkflowName: "target-workflow",
			})
			yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
		})

		suite.T().Run("wrong workflow", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				_, err := client.RestartWorkflow(ctx, &pb.RestartWorkflowRequest{
					PrId:         grpc.MarshalID(pr.ID),
					HeadHash:     pr.HeadHash.String(),
					WorkflowId:   "1",
					WorkflowName: "other-workflow",
				})
				require.NoError(t, err)
			})
			require.Empty(t, events)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestCIHandler_RestartAll() {
	for _, configPath := range []string{oyaml.CIPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		client := pb.NewCIServiceClient(suite.grpcClient)
		repo := suite.repos.Alpha
		suite.addRole(suite.T(), suite.users.Kopatych, repo, iam.Roles.Admin)

		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{})

		t.Run("no oyaml", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				response, err := client.RestartAllWorkflows(ctx, &pb.RestartAllWorkflowsRequest{
					PrId:     grpc.MarshalID(pr.ID),
					HeadHash: pr.HeadHash.String(),
				})
				require.NoError(t, err)
				require.False(t, response.IsOutdated)
			})

			require.Empty(t, events)
		})

		suite.addDefaultOYaml(repo, "master", configPath)
		suite.mustBash(repo, `
		git checkout branch && git rebase master
	`)

		t.Run("wrong head hash", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				response, err := client.RestartAllWorkflows(ctx, &pb.RestartAllWorkflowsRequest{
					PrId:     grpc.MarshalID(pr.ID),
					HeadHash: plumbing.ZeroHash.String(),
				})
				require.NoError(t, err)
				require.True(t, response.IsOutdated)
			})
			require.Empty(t, events)
		})

		pr, err := suite.PullRequestRepo.Get(context.Background(), pr.ID)
		require.NoError(t, err)

		t.Run("ok", func(t *testing.T) {
			events := suite.getCIEventsWith(t, func() {
				ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
				response, err := client.RestartAllWorkflows(ctx, &pb.RestartAllWorkflowsRequest{
					PrId:     grpc.MarshalID(pr.ID),
					HeadHash: pr.HeadHash.String(),
				})
				require.NoError(t, err)
				require.False(t, response.IsOutdated)
			})

			require.Len(t, events, 1)
			require.NotNil(t, events[0].TriggerRequest)
			payload := events[0].TriggerRequest.Trigger.GetPr()
			require.NotNil(t, payload)
			require.NotNil(t, payload.RefsUpdate)
			require.Equal(t, grpc.MarshalID(pr.ID), payload.PrId)
			require.Equal(t, pr.HeadHash.String(), payload.RefsUpdate.HeadSha)
		})

		suite.AfterTest("", "")
	}
}
