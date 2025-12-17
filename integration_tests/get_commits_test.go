package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGetCommitsByHashBulk() {
	t := suite.T()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	repoID := grpc_marshalling.IDInverse(suite.repos.Alpha.ID)
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Admin)
	uploadKey := suite.uploadPic(suite.users.Admin.Identity)
	uploadKey2 := suite.uploadPic(suite.users.Admin.Identity)

	commit := func(actions []*pb.CommitAction, branch string, message string) *pb.Commit {
		request := &pb.CommitRequest{
			Id:      repoID,
			Branch:  branch,
			Message: message,
			Actions: actions,
		}

		resp, err := client.Commit(ctx, request)

		require.NoError(t, err)

		commitResp, err := grpc_marshalling.OperationResponse(resp, &pb.Commit{})
		require.NoError(t, err)

		return commitResp
	}

	commit1 := commit(
		[]*pb.CommitAction{
			{
				UploadKey: &uploadKey.Key,
				NewPath:   utils.PtrFromValue("tmp/file.txt"),
			},
		}, "master", "add file")

	commit2 := commit(
		[]*pb.CommitAction{
			{
				UploadKey: &uploadKey2.Key,
				NewPath:   utils.PtrFromValue("file1.txt"),
			},
		}, "master", "add file 2")

	unknownHash := "aaaaaaaaaaaaaaabbbbbbbbbbbbbbbbbbbbbbbbb"

	tests := []struct {
		name         string
		hashes       []string
		expected     []*pb.Commit
		expectedCode codes.Code
	}{
		{
			name:     "ok",
			hashes:   []string{commit1.Hash, commit2.Hash},
			expected: []*pb.Commit{commit1, commit2},
		}, {
			name:         "empty",
			hashes:       []string{},
			expected:     []*pb.Commit{},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "incorrect hash",
			hashes:       []string{"saiuuib9289saydf"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "unknown hash",
			hashes:       []string{unknownHash},
			expectedCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.GetCommitsByHashBulk(ctx, &pb.GetCommitsByHashBulkRequest{
				Id:     repoID,
				Hashes: tt.hashes,
			})

			yarequire.ProtoStatusEqual(t, tt.expectedCode, err)
			if tt.expectedCode != codes.OK {
				return
			}

			yarequire.ProtoElementsMatch(t, tt.expected, resp.Commits)
		})
	}
}
