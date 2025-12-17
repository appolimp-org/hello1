package integrationtests

import (
	"common/grpc/exceptions"
	"common/testutils/yarequire"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGetTextFileContent() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	tests := []struct {
		name string
		req  *pb.GetTextFileContentRequest
		err  *exceptions.ExceptionTemplate
	}{
		{
			name: "happy path",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Path: "vendor/foo.go",
			},
		},
		{
			name: "bad revision",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "aaaaa",
				Path: "vendor/foo.go",
			},
			err: except.ReferenceNotFound,
		},
		{
			name: "range",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "LICENSE",
				Range: &pb.TextFileRange{
					From: 14,
					To:   16,
				},
			},
		},
		{
			name: "from_start",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "LICENSE",
				Range: &pb.TextFileRange{
					To: 2,
				},
			},
		},
		{
			name: "to_end",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "LICENSE",
				Range: &pb.TextFileRange{
					From: 19,
				},
			},
		},
		{
			name: "interval_empty",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "LICENSE",
				Range: &pb.TextFileRange{
					From: 9999,
				},
			},
		},
		{
			name: "interval_from_out", // empty response
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "LICENSE",
				Range: &pb.TextFileRange{
					To: 9999,
				},
			},
		},

		{
			name: "invalid_path",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev:  "branch",
				Path: "not a file that exists",
				Range: &pb.TextFileRange{
					To: 9999,
				},
			},
			err: except.PathNotFound,
		},
		{
			name: "binary file",
			req: &pb.GetTextFileContentRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Path: "binary.jpg",
			},
			err: except.FileIsBinary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
			resp, err := client.GetTextFileContent(ctx, tt.req)

			if tt.err != nil {

				yarequire.ProtoExceptionTemplate(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
