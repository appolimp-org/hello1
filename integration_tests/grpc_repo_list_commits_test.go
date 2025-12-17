package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

// use `git log  --pretty=format:"%h"“ to get fixtures
func (suite *RwApiTestSuite) TestGrpcListCommits() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	tt := map[string]struct {
		request        *pb.ListCommitsRequest
		expected       []string
		expectedStatus codes.Code
	}{
		"happy path": {request: &pb.ListCommitsRequest{
			Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		}, expected: []string{
			"6ecf0ef",
			"918c48b",
			"af2d6a6",
			"1669dce",
			"a5b8b09",
			"35e8510",
			"b8e471f",
			"b029517",
		}},
		"pagination": {request: &pb.ListCommitsRequest{
			Id:       grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			PageSize: utils.PtrFromValue((uint64)(3)),
		}, expected: []string{
			"6ecf0ef",
			"918c48b",
			"af2d6a6",
			"1669dce",
			"a5b8b09",
			"35e8510",
			"b8e471f",
			"b029517",
		}},
		"since rev": {request: &pb.ListCommitsRequest{
			Id:  grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Rev: "35e85108805c84807bc66a02d91535e1e24b38b9",
		}, expected: []string{
			"35e8510",
			"b029517",
		}},
		// git log  --pretty=format:"%h" -- LICENSE
		"file": {request: &pb.ListCommitsRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Path: utils.PtrFromValue("LICENSE"),
		}, expected: []string{
			"b029517",
		}},

		// git log  --pretty=format:"%h" -- LICENSE
		"file2": {request: &pb.ListCommitsRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Path: utils.PtrFromValue("CHANGELOG"),
		}, expected: []string{
			"b8e471f",
		}},

		// git log  --pretty=format:"%h" -- go
		"dir": {request: &pb.ListCommitsRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Path: utils.PtrFromValue("go"),
		}, expected: []string{
			"918c48b",
		}},

		"threedot": {request: &pb.ListCommitsRequest{
			Id:            grpc_marshalling.IDInverse(suite.repos.ThreeDot.ID),
			Rev:           "main",
			MergebaseDest: utils.PtrFromValue("branch"),
		}, expected: []string{
			"77ba86b4", // main commit 3
			"aa28cbf0", // 2-nd merge
			"1162a6b8", // 1-st merge
			"3d5bc73b", // main commit 2
		}},

		"badrev": {request: &pb.ListCommitsRequest{
			Id:  grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Rev: "lol",
		}, expectedStatus: codes.NotFound},

		// git log --pretty=format:"%h" -- 1
		"merge-history": {request: &pb.ListCommitsRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.MergeHistory.ID),
			Rev:  "main",
			Path: utils.PtrFromValue("1"),
		}, expected: []string{
			"d9cc05a", // master commit changing file
		}},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			// paginate and load all commits
			var commits []*pb.Commit
			ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
			for {
				resp, err := client.ListCommits(ctx, tc.request)
				yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
				if tc.expectedStatus != 0 {
					return
				}

				commits = append(commits, resp.Commits...)
				if resp.NextPageToken == "" {
					break
				}
				tc.request.PageToken = &resp.NextPageToken
			}
			// verify prefixes
			require.Equal(t, len(tc.expected), len(commits))
			for id, prefix := range tc.expected {
				require.True(t, strings.HasPrefix(commits[id].Hash, prefix), "%dth pos: expected %s, got %s", id, prefix, commits[id].Hash)
			}
		})
	}
}
