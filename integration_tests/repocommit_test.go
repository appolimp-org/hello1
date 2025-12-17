package integrationtests

import (
	"common/grpc/exceptions"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestCommitUploads() {
	t := suite.T()

	user := suite.users.Kopatych
	slug := suite.repos.Alpha.FullSlug()

	fileNames := []string{
		"hehe.go",
		"foo.go",
		"vendor/hehe.go",
		"vendor/foo.go",
		"newfolder/nested/hehe.go",
		"newfolder/nested/not_hehe.go",
		"newfolder/branching/hehe.go",
	}
	actions := make([]schemas.CommitAction, len(fileNames))
	data := []byte("hehe")

	for i, filename := range fileNames {
		result := &schemas.UploadFileResponse{}

		resp, err := suite.client.As(user.Identity).
			SetQueryParams(map[string]string{
				"file_name":   "hehe.go",
				"upload_type": string(schemas.FileUploadTypes.Text)}).
			SetHeaders(map[string]string{"Content-Type": "text/plain"}).
			SetBody(data).SetResult(&result).Post("/api/v1/me/uploads")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())

		actions[i] = schemas.CommitAction{Key: utils.PtrFromValue(result.Key), NewPath: utils.PtrFromValue(filename)}
	}

	req := &schemas.CommitRequest{
		Branch:  "branch",
		Message: "commit",
		Actions: actions,
	}

	// contributor can't push commits via UI
	t.Run("contributor can't push commits via UI", func(t *testing.T) {
		suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
		resp, err := suite.client.As(user.Identity).SetBody(req).Post(fmt.Sprintf("/api/v1/repos/%s/commit", slug))
		require.NoError(t, err)
		require.Equal(t, 403, resp.StatusCode())
	})

	t.Run("developer can push", func(t *testing.T) {
		suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
		resp, err := suite.client.As(user.Identity).SetBody(req).Post(fmt.Sprintf("/api/v1/repos/%s/commit", slug))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())

		for _, path := range fileNames {
			resp, err := suite.client.As(user.Identity).
				SetQueryParam("rev", "branch").
				SetQueryParam("path", path).Get(fmt.Sprintf("/api/v1/repos/%s/fileContent", slug))
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode())

			require.Equal(t, resp.Body(), data)
		}
	})
}

func (suite *RwApiTestSuite) TestHTTPCommitPathTraversal() {
	uploadKey := suite.uploadPic(suite.users.AuthDeveloper.Identity)

	resp, err := suite.client.As(suite.users.AuthDeveloper.Identity).
		SetBody(&schemas.CommitRequest{
			Branch:  "master",
			Message: "commit",
			Actions: []schemas.CommitAction{
				{
					Key:     &uploadKey.Key,
					NewPath: utils.PtrFromValue("../../etc/passwd"),
				},
			},
		}).
		Post(fmt.Sprintf("/api/v1/repos/%s/commit", suite.repos.AuthRepoPublic.FullSlug()))

	yarequire.StatusCode(suite.T(), resp, err, 400)
}

func (suite *RwApiTestSuite) TestCommitGRPC() {
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Admin)
	uploadKey := suite.uploadPic(suite.users.Admin.Identity)
	uploadKey2 := suite.uploadPic(suite.users.Admin.Identity)
	uploadKey3 := suite.uploadPic(suite.users.Admin.Identity)
	uploadKey4 := suite.uploadPic(suite.users.Admin.Identity)
	t := suite.T()

	testcases := []struct {
		name                  string
		repo                  *entities.Repository
		actions               []*pb.CommitAction
		branch                string
		message               string
		expectedStatus        codes.Code
		expectedErrorTemplate *exceptions.ExceptionTemplate
	}{
		{
			name:    "add a file",
			repo:    suite.repos.Alpha,
			branch:  "master",
			message: "TSK-084[internal]: Add new file",
			actions: []*pb.CommitAction{
				{
					UploadKey: &uploadKey.Key,
					NewPath:   utils.PtrFromValue("tmp/file.txt"),
				},
			},
			expectedStatus: codes.OK,
		},
		{
			name:    "branch_policy",
			repo:    suite.repos.BranchPolicy,
			branch:  "nonpr",
			message: "branch policy prevent nonpr changes",
			actions: []*pb.CommitAction{
				{
					UploadKey: &uploadKey4.Key,
					NewPath:   utils.PtrFromValue("tmp/file.txt"),
				},
			},
			expectedErrorTemplate: except.BranchPolicyViolation,
			expectedStatus:        codes.FailedPrecondition,
		},
		{
			name:    "commit with invalid upload key",
			repo:    suite.repos.Alpha,
			branch:  "master",
			message: "Invalid upload key",
			actions: []*pb.CommitAction{
				{
					UploadKey: utils.PtrFromValue("invalid-upload-key"),
					NewPath:   utils.PtrFromValue("new/file.txt"),
				},
			},
			expectedStatus: codes.InvalidArgument,
		},
		{
			name:    "commit with invalid branch",
			repo:    suite.repos.Alpha,
			branch:  "nonexisting",
			message: "Commit to invalid branch",
			actions: []*pb.CommitAction{
				{
					OldPath: utils.PtrFromValue("file.txt"),
				},
			},
			expectedStatus: codes.NotFound,
		},
		{
			name:    "invalid commit action",
			repo:    suite.repos.Alpha,
			branch:  "master",
			message: "Invalid commit action",
			actions: []*pb.CommitAction{
				{
					NewPath: utils.PtrFromValue("file.txt"),
				},
			},
			expectedStatus: codes.InvalidArgument,
		},
		{
			name:    "malicious path",
			repo:    suite.repos.Alpha,
			branch:  "master",
			message: "l33t commit",
			actions: []*pb.CommitAction{
				{
					UploadKey: &uploadKey2.Key,
					NewPath:   utils.PtrFromValue("../../../etc/passwd"),
				},
			},
			expectedErrorTemplate: except.CommitActionInvalidPath,
			expectedStatus:        codes.InvalidArgument,
		},
		{
			name:    "malicious path 2",
			repo:    suite.repos.Alpha,
			branch:  "master",
			message: "l33t commit",
			actions: []*pb.CommitAction{
				{
					UploadKey: &uploadKey3.Key,
					NewPath:   utils.PtrFromValue(".git/config"),
				},
			},
			expectedErrorTemplate: except.CommitActionInvalidPath,
			expectedStatus:        codes.InvalidArgument,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {

			request := &pb.CommitRequest{
				Id:      grpc_marshalling.IDInverse(tc.repo.ID),
				Branch:  tc.branch,
				Message: tc.message,
				Actions: tc.actions,
			}

			resp, err := client.Commit(ctx, request)

			if tc.expectedErrorTemplate != nil {
				yarequire.ProtoExceptionTemplate(t, err, tc.expectedErrorTemplate)
			}

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus == codes.OK {
				commitResp, err := grpc_marshalling.OperationResponse(resp, &pb.Commit{})
				require.NoError(t, err)

				require.NotNil(t, commitResp)
				require.NotEmpty(t, commitResp.Hash)

				// yarequire.ProtoDumpFixture(t, commitResp)
				yarequire.ProtoCompareWithFixture(t, commitResp,
					protocmp.IgnoreFields(&pb.Commit{}, "hash", "parent_commits"),
					protocmp.IgnoreFields(&pb.Signature{}, "date"),
				)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestCommitInEmptyRepoGRPC() {
	t := suite.T()

	for name, test := range map[string]struct {
		user         *entities.User
		org          *entities.Organization
		branch       string
		message      string
		repo         *entities.Repository
		slug         string
		expectedCode codes.Code
	}{
		"successful commit": {
			user:         suite.users.Kopatych,
			org:          suite.orgs.Yandex,
			branch:       "uley",
			message:      "init commit",
			repo:         nil,
			slug:         "repa",
			expectedCode: codes.OK,
		},
		"unsuccessful commit with empty branch": {
			user:         suite.users.Krosh,
			org:          suite.orgs.Yandex,
			branch:       "",
			message:      "init",
			repo:         nil,
			slug:         "pepa",
			expectedCode: codes.InvalidArgument,
		},
		"unsuccessful commit because repo not empty": {
			user:         suite.users.Admin,
			org:          suite.orgs.Yandex,
			branch:       "ulyalya",
			message:      "timmoc",
			repo:         suite.repos.Alpha,
			expectedCode: codes.AlreadyExists,
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := test.repo
			if repo == nil {
				repo = suite.makeRepo(test.user, &interfaces.CreateRepositoryArgs{
					OrgID:      test.org.GetOrgID(),
					Name:       "repa",
					Slug:       test.slug,
					Visibility: entities.Visibilities.Public,
					CreatedBy:  test.user.GetID(),
					IsEmpty:    true,
				})
			}

			uploadKey := suite.uploadPic(test.user.Identity)

			request := &pb.CommitRequest{
				Id:      grpc_marshalling.IDInverse(repo.GetID()),
				Branch:  test.branch,
				Message: test.message,
				Actions: []*pb.CommitAction{
					{
						UploadKey: &uploadKey.Key,
						NewPath:   utils.PtrFromValue("abc/readme"),
					},
				},
				InitialCommit: true,
			}

			client := pb.NewRepoServiceClient(suite.grpcClient)
			ctx := testutils.AuthorizeGRPC(test.user.Identity)

			resp, err := client.Commit(ctx, request)
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)

			if test.expectedCode != codes.OK {
				return
			}

			commitResp, err := grpc_marshalling.OperationResponse(resp, &pb.Commit{})
			require.NoError(t, err)

			require.NotNil(t, commitResp)
			require.NotEmpty(t, commitResp.Hash)
			require.Equal(t, test.message, commitResp.Message)
			require.Equal(t, test.user.Username, commitResp.Commiter.GetName())
			require.Zero(t, len(commitResp.ParentCommits))

			branches, err := client.ListBranches(ctx, &pb.ListBranchesRequest{Id: grpc_marshalling.IDInverse(repo.ID)})
			require.NoError(t, err)
			require.Equal(t, len(branches.Branches), 1)

			var branch string
			if test.branch == "" {
				branch = "main"
			} else {
				branch = test.branch
			}
			require.Equal(t, branch, branches.Branches[0].Name)
			require.Equal(t, branches.Branches[0].Commit.Hash, commitResp.Hash)
		})
	}
}
