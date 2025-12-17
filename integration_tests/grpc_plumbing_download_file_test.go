package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"io"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingDownloadFile() {
	t := suite.T()
	client := pb.NewContentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.DownloadFileRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "1.txt",
			},
		},
		"file_not_found": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "2.txt",
			},
			code: codes.NotFound,
		},
		"deep_path": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "vendor/foo.go",
			},
		},
		"range": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:      "LICENSE",
				LineStart: 15,
				LineEnd:   17,
			},
		},
		"from_start": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:    "LICENSE",
				LineEnd: 3,
			},
		},
		"to_end": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:      "LICENSE",
				LineStart: 20,
			},
		},
		"to_end_empty": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:    "LICENSE",
				LineEnd: 1,
			},
		},
		"interval_empty": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:      "LICENSE",
				LineStart: 9999,
			},
		},
		"interval_from_out": { // empty response
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:      "LICENSE",
				LineStart: 9999,
			},
		},
		"binary_file": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "binary.jpg",
			},
		},
		"binary_file_rejected": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path:              "binary.jpg",
				RejectBinaryFiles: true,
			},
			code: codes.FailedPrecondition,
		},
		"size_rejected": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path:    "1.txt",
				MaxSize: 1,
			},
			code: codes.FailedPrecondition,
		},
		"invalid_path": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "../../../etc/passwd",
			},
			code: codes.InvalidArgument,
		},
		"empty_path": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "",
			},
			code: codes.NotFound,
		},
		"branch": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "b1",
					},
				},
				Path: "1.txt",
			},
		},
		"commit": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "1ccd97f1bc3277c234d03a6687973d27b6ed765e",
					},
				},
				Path: "1.txt",
			},
		},
		"no_such_branch": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.History.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "missing_branch",
					},
				},
				Path: "1.txt",
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.DownloadFileRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Path: "1.txt",
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			stream, err := client.DownloadFile(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			var done bool
			var chunkCounter int
			for !done && chunkCounter < 10 {
				t.Run(fmt.Sprintf("chunk_%d", chunkCounter), func(t *testing.T) {
					chunk, err := stream.Recv()
					if err == io.EOF {
						done = true
						return
					}

					yarequire.ProtoStatusEqual(t, tc.code, err)
					if tc.code != codes.OK {
						done = true
						return
					}

					// yarequire.ProtoDumpFixture(t, chunk)
					yarequire.ProtoCompareWithFixture(t, chunk)
				})
				chunkCounter++
			}

			require.True(t, done)
		})
	}
}
