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

func (suite *RwApiTestSuite) TestGrpcFileDiff() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.FileDiffRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("master"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "README"},
					{Source: "vendor/foo.go"},
					{Source: "no/such/file.go"},
					{Source: "vendor"},
					{Source: "binary.jpg"},
				},
				Context: utils.PtrFromValue(uint32(5)),
			},
		},
		"merge_base": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: utils.PtrFromValue("master"),
				SourceRev: utils.PtrFromValue("branch"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "README"},
					{Source: "vendor/foo.go"},
					{Source: "no/such/file.go"},
					{Source: "vendor"},
					{Source: "binary.jpg"},
				},
				Context:   utils.PtrFromValue(uint32(5)),
				MergeBase: true,
			},
		},
		"same rev": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: utils.PtrFromValue("master"),
				SourceRev: utils.PtrFromValue("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "README"},
					{Source: "dont/read/me"},
				},
				Context: utils.PtrFromValue(uint32(5)),
			},
		},
		"nil tgt": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				TargetRev: nil,
				SourceRev: utils.PtrFromValue("branch"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "README"},
				},
				Context: utils.PtrFromValue(uint32(5)),
			},
		},
		"too big": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
				TargetRev: utils.PtrFromValue("main"),
				SourceRev: utils.PtrFromValue("feature"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "toobig"},
					{Source: "okay"},
				},
				Context: utils.PtrFromValue(uint32(0)),
			},
		},
		"too big, no limit": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
				TargetRev: utils.PtrFromValue("main"),
				SourceRev: utils.PtrFromValue("feature"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "toobig"},
				},
				Context:   utils.PtrFromValue(uint32(0)),
				LimitDiff: utils.PtrFromValue(uint32(0)),
			},
		},
		"renamed": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.DifferentDiffs.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "file1.txt", Target: "file1_moved.txt"},
					{Source: "file1.txt", Target: "file3.txt"},
				},
				Context: utils.PtrFromValue(uint32(0)),
			},
		},
		"deleted": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.DifferentDiffs.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
				Paths: []*pb.FileDiffRequest_Path{
					{Target: "file1_moved.txt"},
				},
				Context: utils.PtrFromValue(uint32(0)),
			},
		},
		"empty_path": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.DifferentDiffs.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
				Paths: []*pb.FileDiffRequest_Path{
					{},
				},
				Context: utils.PtrFromValue(uint32(0)),
			},
			code: codes.InvalidArgument,
		},
		"multiple merge bases": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
				TargetRev: utils.PtrFromValue("F"),
				SourceRev: utils.PtrFromValue("G"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "a1"},
				},
				MergeBase: true,
				Context:   utils.PtrFromValue(uint32(0)),
			},
		},
		"utf abuse": {
			request: &pb.FileDiffRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.UtfAbuse.ID),
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
				Paths: []*pb.FileDiffRequest_Path{
					{Source: "utfabuse.txt"},
				},
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.FileDiff(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
