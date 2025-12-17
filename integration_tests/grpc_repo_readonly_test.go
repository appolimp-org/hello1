package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"encoding/json"
	"fmt"
	accesscommon "gitcore/internal/access/common"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"
	"testing"
)

func (suite *RwApiTestSuite) TestReadonlyRepo() {
	t := suite.T()

	suite.addOrgRole(t, suite.users.Admin, suite.orgs.AuthSandbox, iam.Roles.Admin)
	repo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:         suite.orgs.AuthSandbox.ID,
		Description:   "Read-only repo",
		Slug:          "read-only",
		Visibility:    entities.Visibilities.Public,
		CreatedBy:     suite.users.Admin.ID,
		DefaultBranch: utils.PtrFromValue("main"),
		Authenticator: accesscommon.NewIAMTokenAuthenticator(
			testutils.FakeIAMAuthToken(suite.users.Admin.Identity),
			&suite.users.Admin.Identity,
		),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true, // to avoid dealing with empty repo
		},
	})
	for user, role := range suite.AllAuthUsersWithRoles() {
		if role != nil {
			suite.addRole(t, user, repo, *role)
		}
	}

	// Prepare

	suite.mustBash(repo, `
		touch my.tralala
		git add . && git commit -m "main"
		git checkout -b source
		touch my.dingdingdong
		git add . && git commit -m "source"
	`)
	pr := suite.makePullRequest(suite.users.Admin, &makePrOptions{
		Repo:   repo,
		Source: "source",
		Target: "main",
	})
	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repo.ID,
	})

	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repo.ID)
	require.NoError(t, err)
	err = suite.RepoRepo.UpdateRepositoryByID(repo.ID).
		SetIsReadOnly(true).
		Commit(context.Background())
	require.NoError(t, err)

	// Test

	tests := []struct {
		name       string
		userAction func(t *testing.T, ctx context.Context) error
	}{
		{
			name: "create pr",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewPRServiceClient(suite.grpcClient).Create(ctx, &pb.CreatePullRequestRequest{
					RepoId:      grpc.MarshalID(repo.ID),
					Title:       "PR",
					Description: "Pull Request",
					Source:      "source",
					Target:      *repo.DefaultBranch,
					Publish:     true,
				})
				return err
			},
		},
		{
			name: "create issue",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewIssueServiceClient(suite.grpcClient).Create(ctx, &pb.CreateIssueRequest{
					RepoId:      grpc.MarshalID(repo.ID),
					Title:       "Issue",
					Description: "Issue",
					Visibility:  pb.Issue_VISIBILITY_PUBLIC,
				})
				return err
			},
		},
		{
			name: "create milestone",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewMilestoneServiceClient(suite.grpcClient).Create(ctx, &pb.CreateMilestoneRequest{
					RepoId:      grpc.MarshalID(repo.ID),
					Name:        "Milestone",
					Description: "Milestone",
				})
				return err
			},
		},
		{
			name: "create label",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewLabelServiceClient(suite.grpcClient).Create(ctx, &pb.CreateLabelRequest{
					RepoId: grpc.MarshalID(repo.ID),
					Name:   "Label",
					Color:  "blue",
				})
				return err
			},
		},
		{
			name: "create issue comment",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewIssueCommentServiceClient(suite.grpcClient).Create(ctx, &pb.CreateIssueCommentRequest{
					IssueId: grpc.MarshalID(issue.ID),
					Body:    "Issue Comment",
				})
				return err
			},
		},
		{
			name: "create pr comment",
			userAction: func(t *testing.T, ctx context.Context) error {
				_, err := pb.NewPRCommentServiceClient(suite.grpcClient).Create(ctx, &pb.CreateCommentRequest{
					PrId:    grpc.MarshalID(pr.ID),
					Body:    "PR Comment",
					Publish: false,
					Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				})
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite.AuthMatrix(t, func(ctx context.Context) error {
				err := tc.userAction(t, ctx)
				yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
				require.Contains(t, err.Error(), except.RepositoryIsReadOnly.Template)
				return err
			}).SpecificUsers( /* no one */ )
		})
	}

	t.Run("push", func(t *testing.T) {
		suite.AuthMatrix(t, func(ctx context.Context) error {
			userIdx := testutils.GetAuthorizedUser(ctx)
			require.NotNil(t, userIdx)
			user, err := suite.UserRepo.GetUser(ctx, *userIdx)
			require.NoError(t, err)

			cg, _ := suite.initCGit(user, repo.FullSlug())
			require.NoError(t, cg.BashNoCapture(fmt.Sprintf(`
				git checkout -b user/%s
				touch grass.please
				git add . && git commit -m "commit"
			`, user.Username)))

			_, stderr, err := cg.Exec("push", "--all")
			require.NotNil(t, err)
			require.Contains(t, stderr, "403")
			return err
		}).SpecificUsers( /* no one */ )
	})

	repoAllowedActions := []string{
		"view_content",
		"view_settings",
		"get_issues",
		"get_labels",
		"list_labels",
		"get_milestones",
		"list_milestones",
		"get_pull_requests",
		"list_pull_requests",
		"list_roles",
		"get_releases",
		"view_associated_artifact_registries",
	}
	repoAdminAllowedActions := []string{
		"get_ci_artifacts",
		"add_contributors",
		"remove_contributors",
		"delete_repo",
		"list_ci_flux",
		"get_ci_workflow_content",
		"get_bypass_status",
		"get_secrets",
		"list_secrets",
		"manage_roles",
	}

	t.Run("repo available actions (for admin)", func(t *testing.T) {
		actions, err := pb.NewRepoServiceClient(suite.grpcClient).GetAvailableActions(
			testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			&pb.GetAvailableRepoActionsRequest{
				Id: grpc.MarshalID(repo.ID),
			},
		)
		require.NoError(t, err)

		rawActions, err := protojson.MarshalOptions{
			UseProtoNames:     true,
			EmitDefaultValues: true,
		}.Marshal(actions)
		require.NoError(t, err)
		var data map[string]bool
		require.NoError(t, json.Unmarshal(rawActions, &data))

		availableActions := functools.Filter(functools.Keys(data), func(s string) bool {
			return data[s]
		})
		expectedActions := append(slices.Clone(repoAllowedActions), repoAdminAllowedActions...)
		slices.Sort(availableActions)
		slices.Sort(expectedActions)
		require.Equal(t, expectedActions, availableActions)
	})

	t.Run("repo available actions (for viewer)", func(t *testing.T) {
		actions, err := pb.NewRepoServiceClient(suite.grpcClient).GetAvailableActions(
			testutils.AuthorizeGRPC(suite.users.AuthViewer.Identity),
			&pb.GetAvailableRepoActionsRequest{
				Id: grpc.MarshalID(repo.ID),
			},
		)
		require.NoError(t, err)

		rawActions, err := protojson.MarshalOptions{
			UseProtoNames:     true,
			EmitDefaultValues: true,
		}.Marshal(actions)
		require.NoError(t, err)
		var data map[string]bool
		require.NoError(t, json.Unmarshal(rawActions, &data))

		availableActions := functools.Filter(functools.Keys(data), func(s string) bool {
			return data[s]
		})
		expectedActions := slices.Clone(repoAllowedActions)
		slices.Sort(availableActions)
		slices.Sort(expectedActions)
		require.Equal(t, expectedActions, availableActions)
	})

	t.Run("pr available actions (for admin)", func(t *testing.T) {
		actions, err := pb.NewPRServiceClient(suite.grpcClient).GetAvailableActions(
			testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			&pb.GetAvailablePRActionsRequest{
				Id: grpc.MarshalID(pr.ID),
			},
		)
		require.NoError(t, err)

		rawActions, err := protojson.MarshalOptions{
			UseProtoNames:     true,
			EmitDefaultValues: true,
		}.Marshal(actions)
		require.NoError(t, err)
		var data map[string]bool
		require.NoError(t, json.Unmarshal(rawActions, &data))

		for action, verdict := range data {
			action = strings.ToLower(action)
			require.False(t, verdict, action)
		}
	})

	t.Run("private issues visible to admin", func(t *testing.T) {
		privateIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repo.ID,
			Visibility: entities.IssueVisibilities.Private,
		})

		resp, err := pb.NewIssueServiceClient(suite.grpcClient).Get(
			testutils.AuthorizeGRPC(suite.users.AuthAdmin.Identity),
			&pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{
					Id: grpc.MarshalID(privateIssue.ID),
				},
			},
		)
		require.NoError(t, err)
		require.Equal(t, grpc.MarshalID(privateIssue.ID), resp.Id)
	})
}
