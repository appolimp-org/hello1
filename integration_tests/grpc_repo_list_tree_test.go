package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcListTree() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request          *pb.ListTreeRequest
		expectedResponse *pb.ListTreeResponse
		code             codes.Code
	}{
		"happy path": {
			request: &pb.ListTreeRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
			expectedResponse: &pb.ListTreeResponse{TreeEntries: []*pb.TreeEntry{
				{Type: pb.DirEntryType_DE_FILE, Path: ".gitignore"},
				{Type: pb.DirEntryType_DE_FILE, Path: "CHANGELOG"},
				{Type: pb.DirEntryType_DE_FILE, Path: "LICENSE"},
				{Type: pb.DirEntryType_DE_FILE, Path: "binary.jpg"},
				{Type: pb.DirEntryType_DE_DIR, Path: "go"},
				{Type: pb.DirEntryType_DE_FILE, Path: "go/example.go"},
				{Type: pb.DirEntryType_DE_DIR, Path: "json"},
				{Type: pb.DirEntryType_DE_FILE, Path: "json/long.json"},
				{Type: pb.DirEntryType_DE_FILE, Path: "json/short.json"},
				{Type: pb.DirEntryType_DE_DIR, Path: "php"},
				{Type: pb.DirEntryType_DE_FILE, Path: "php/crappy.php"},
				{Type: pb.DirEntryType_DE_DIR, Path: "vendor"},
				{Type: pb.DirEntryType_DE_FILE, Path: "vendor/foo.go"},
			}},
			code: 0,
		},
		"filter": {
			request: &pb.ListTreeRequest{
				Id:     grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Filter: "json",
			},
			expectedResponse: &pb.ListTreeResponse{TreeEntries: []*pb.TreeEntry{
				{Type: pb.DirEntryType_DE_DIR, Path: "json"},
				{Type: pb.DirEntryType_DE_FILE, Path: "json/long.json"},
				{Type: pb.DirEntryType_DE_FILE, Path: "json/short.json"},
			}},
			code: 0,
		},

		"path": {
			request: &pb.ListTreeRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Path: "json",
			},
			expectedResponse: &pb.ListTreeResponse{TreeEntries: []*pb.TreeEntry{
				{Type: pb.DirEntryType_DE_FILE, Path: "json/long.json"},
				{Type: pb.DirEntryType_DE_FILE, Path: "json/short.json"},
			}},
			code: 0,
		},

		"no rev": {
			request: &pb.ListTreeRequest{
				Id:  grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev: "xxxx",
			},

			code: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.ListTree(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			yarequire.ProtoCmp(t, tc.expectedResponse, resp, protocmp.IgnoreFields(&pb.Commit{}, "message", "parent_commits", "author", "commiter"))
		})
	}
}
