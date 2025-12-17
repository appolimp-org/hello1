package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcTreeDiff() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.TreeDiffRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("main"),
				SourceRev: utils.PtrFromValue("branch"),
			},
		},
		"revert": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
			},
		},
		"not_found": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("xxxx"),
				SourceRev: utils.PtrFromValue("main"),
			},
			code: codes.NotFound,
		},
		"empty": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("9992a6f69d20dd07d47649125b0d2e9092b4c342"),
				SourceRev: utils.PtrFromValue("9992a6f69d20dd07d47649125b0d2e9092b4c342"),
			},
		},
		"nil target": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: nil,
				SourceRev: utils.PtrFromValue("bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6"),
			},
		},
		"nil source": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6"),
				SourceRev: nil,
			},
			code: codes.InvalidArgument,
		},
		"nil both": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: nil,
				SourceRev: nil,
			},
			code: codes.InvalidArgument,
		},
		"merge_base": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: utils.PtrFromValue("master"),
				SourceRev: utils.PtrFromValue("branch"),
				MergeBase: true,
			},
		},
		"merge_base invert": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("master"),
				MergeBase: true,
			},
		},
		"renames": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: utils.PtrFromValue("a17aa945be4a0b0c14e0318fb3f45aacceb0c3b5"),
				SourceRev: utils.PtrFromValue("575b2e9c3c8d3b433260b84328e4f884bcaf7721"),
			},
		},
		"multiple merge bases": {
			request: &pb.TreeDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
				TargetRev: utils.PtrFromValue("F"),
				SourceRev: utils.PtrFromValue("G"),
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.TreeDiff(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
