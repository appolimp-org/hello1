package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/api/repos"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestCreateBranchHTTP() {
	testcases := []struct {
		name, branchName string
		rev              *string
	}{
		{
			name:       "empty rev",
			branchName: "newBranch",
		},
		{
			name:       "from master",
			branchName: "newBranch",
			rev:        utils.PtrFromValue("master"),
		},
		{
			name:       "from branch",
			branchName: "newBranch",
			rev:        utils.PtrFromValue("branch"),
		},
		{
			name:       "from old master commit",
			branchName: "newBranch",
			rev:        utils.PtrFromValue("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		},
		{
			name:       "from branch commit",
			branchName: "newBranch",
			rev:        utils.PtrFromValue("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		},
	}

	for _, tc := range testcases {
		suite.Run(tc.name, func() {
			t := suite.T()

			rev := &schemas.ResolveRevisionResponse{}
			revParam := "master"
			if tc.rev != nil {
				revParam = *tc.rev
			}

			testutils.Expect(suite.client.R().SetQueryParam("rev", revParam).SetResult(rev).
				Get(fmt.Sprintf("/api/v1/repos/%s/resolveRevision", suite.repos.Alpha.FullSlug()))).
				MustBe(t, http.StatusOK)

			body := &schemas.CreateBranchRequest{Name: tc.branchName, FromRev: tc.rev}
			result := &schemas.Branch{}
			testutils.Expect(suite.client.As(suite.users.Admin.Identity).SetBody(body).SetResult(result).
				Post(fmt.Sprintf("/api/v1/repos/%s/branches", suite.repos.Alpha.FullSlug()))).
				MustBe(t, http.StatusCreated)

			require.Equal(t, tc.branchName, result.Name)
			require.Equal(t, rev.Commit, *result.Commit)

			httpErr := &httperrors.APIError{}
			testutils.Expect(suite.client.As(suite.users.Admin.Identity).SetBody(body).SetError(httpErr).
				Post(fmt.Sprintf("/api/v1/repos/%s/branches", suite.repos.Alpha.FullSlug()))).
				MustBe(t, http.StatusBadRequest)

			require.Equal(t, repos.BranchAlreadyExists, httpErr.Details)

			// check branches
			client := pb.NewRepoServiceClient(suite.grpcClient)
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

			resp, err := client.ListBranches(ctx, &pb.ListBranchesRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}

func (suite *RwApiTestSuite) TestCreateBranchGRPC() {
	client := pb.NewRepoServiceClient(suite.grpcClient)
	repoID := grpc_marshalling.IDInverse(suite.repos.Alpha.ID)
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Admin)

	testcases := []struct {
		name           string
		branchName     string
		fromRev        *string
		expectedStatus codes.Code
	}{
		{
			name:           "create a branch",
			branchName:     "new/branch",
			fromRev:        utils.PtrFromValue("master"),
			expectedStatus: codes.OK,
		},
		{
			name:           "branch already exists",
			branchName:     "master",
			fromRev:        utils.PtrFromValue("branch"),
			expectedStatus: codes.AlreadyExists,
		},
		{
			name:           "invalid branch name",
			branchName:     "invalid/branch?name",
			fromRev:        utils.PtrFromValue("master"),
			expectedStatus: codes.InvalidArgument,
		},
		{
			name:           "invalid reference",
			branchName:     "newBranch",
			fromRev:        utils.PtrFromValue("nonexisting"),
			expectedStatus: codes.NotFound,
		},
	}

	for _, tc := range testcases {
		suite.Run(tc.name, func() {
			t := suite.T()

			request := &pb.CreateBranchRequest{
				Id:      repoID,
				Name:    tc.branchName,
				FromRev: tc.fromRev,
			}
			resp, err := client.CreateBranch(ctx, request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus == codes.OK {
				branch, err := grpc_marshalling.OperationResponse(resp, &pb.Branch{})
				require.NoError(t, err)
				require.Equal(t, tc.branchName, branch.Name)
				require.NotNil(t, branch.Commit)
			}
		})
	}
}
