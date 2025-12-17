package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcResolveRevision() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request          *pb.ResolveRevisionRequest
		expectedResponse *pb.ResolveRevisionResponse
		code             codes.Code
	}{
		"happy path": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "main",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "94d20dfa7b2ae3c998de92780be3058a68280d76"},
				RevisionType: pb.GitRevisionType_REVISION_BRANCH,
				Branches:     []string{"main"},
				Tags:         []string{"v3-an"},
			},
		},
		"tag": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "tag:v2.main",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "78350efe20fc33f940c2894e7c74e15963d7356a"},
				RevisionType: pb.GitRevisionType_REVISION_TAG,
				Branches:     []string{},
				Tags:         []string{"v2.main", "v2.main-an"},
			},
		},
		"annotated tag": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "tag:v2.main-an",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "78350efe20fc33f940c2894e7c74e15963d7356a"},
				RevisionType: pb.GitRevisionType_REVISION_ANNOTATED_TAG,
				Branches:     []string{},
				Tags:         []string{"v2.main", "v2.main-an"},
			},
		},
		"commit": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "39427c55a3c59b91b11b5c1413033ebb85f84364",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "39427c55a3c59b91b11b5c1413033ebb85f84364"},
				RevisionType: pb.GitRevisionType_REVISION_COMMIT,
				Branches:     []string{},
				Tags:         []string{"v1.0"},
			},
		},
		"without refs": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.Blame.ID),
				Revision: "43683522b7adcfb445b2c9323083c8a2f9cde4b8",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "43683522b7adcfb445b2c9323083c8a2f9cde4b8"},
				RevisionType: pb.GitRevisionType_REVISION_COMMIT,
				Branches:     []string{},
				Tags:         []string{},
			},
		},
		"merge": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Revision: "1669dce138d9b841a518c64b10914d88f5e488ea",
			},
			expectedResponse: &pb.ResolveRevisionResponse{
				Commit:       &pb.Commit{Hash: "1669dce138d9b841a518c64b10914d88f5e488ea"},
				RevisionType: pb.GitRevisionType_REVISION_COMMIT,
				Branches:     []string{},
				Tags:         []string{},
			},
		},
		"not_found_commit": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "111112222200000000000000000000000000000",
			},
			code: codes.NotFound,
		},
		"not_found_branch": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "unknown_branch",
			},
			code: codes.NotFound,
		},
		"not_found_tag": {
			request: &pb.ResolveRevisionRequest{
				Id:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Revision: "tag:v9.9",
			},
			code: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.ResolveRevision(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			yarequire.ProtoCmp(t, tc.expectedResponse, resp, protocmp.IgnoreFields(&pb.Commit{}, "message", "parent_commits", "author", "commiter"))
		})
	}

}
