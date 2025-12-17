package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingRangeTreeDiff() {
	t := suite.T()
	client := pb.NewDiffServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.RangeTreeDiffRequest
		code    codes.Code
	}{
		"filters master changes": {
			request: &pb.RangeTreeDiffRequest{
				Target: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_base"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter1"},
					},
				},
				Source: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_master_updated"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter2"},
					},
				},
			},
		},
		"same iteration - empty result": {
			request: &pb.RangeTreeDiffRequest{
				Target: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_base"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter1"},
					},
				},
				Source: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_base"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter1"},
					},
				},
			},
		},
		"no master changes": {
			request: &pb.RangeTreeDiffRequest{
				Target: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_base"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter1"},
					},
				},
				Source: &pb.GitRevisionPair{
					Target: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_base"},
					},
					Source: &pb.GitRevision{
						RepoId:   grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
						Revision: &pb.GitRevision_Branch{Branch: "range_iter2"},
					},
				},
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.RangeTreeDiff(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
