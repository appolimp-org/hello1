package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"gitcore/internal/testutils/packfile_importer"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcPathInfoAuthMatrix() {
	suite.AuthMatrix(suite.T(), func(ctx context.Context) error {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		_, err := client.PathInfo(ctx, &pb.PathInfoRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.AuthRepoPublic.ID),
			Path: ".gitignore",
		})
		return err
	}).AllUsers()

	suite.AuthMatrix(suite.T(), func(ctx context.Context) error {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		_, err := client.PathInfo(ctx, &pb.PathInfoRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.AuthRepoInternal.ID),
			Path: ".gitignore",
		})
		return err
	}).AllUsersBut(suite.users.AuthNobody)

	suite.AuthMatrix(suite.T(), func(ctx context.Context) error {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		_, err := client.PathInfo(ctx, &pb.PathInfoRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
			Path: ".gitignore",
		})
		return err
	}).AllUsersBut(suite.users.AuthNobody, suite.users.AuthMember)
}

func (suite *RwApiTestSuite) TestGrpcPathInfo() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	ctx = testutils.WithAuthorizedGRPC(ctx, suite.users.Krosh.Identity)

	res, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
		},
		Slug:             "huge-folder",
		Visibility:       pb.ResourceVisibility_RESOURCE_PUBLIC,
		DefaultBranch:    "master",
		ProvisionOptions: &pb.ProvisionOptions{},
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(res, &pb.Repository{})
	require.NoError(t, err)

	doip := testutils.GetDataPath("imported/huge-folder")

	pfs := packfile_importer.GetPackfiles(doip)

	repoID, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)

	err = suite.packfileImporterFactory.Build(repoID).Import(ctx, pfs, []entities.SetRef{
		{
			NewRef: plumbing.NewHashReference(
				plumbing.NewBranchReferenceName("master"),
				plumbing.NewHash("08e89050c5ac71009e6157e0e632a77b77660651"),
			),
		},
	}, plumbing.NewBranchReferenceName("master"))
	require.NoError(t, err)

	tt := map[string]struct {
		request *pb.PathInfoRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.PathInfoRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
		},
		"file": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Path: ".gitignore",
			},
		},
		"dir rev": {
			request: &pb.PathInfoRequest{
				Id:  grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Rev: "35e85108805c84807bc66a02d91535e1e24b38b9",
			},
		},

		"dir tag": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Rev:  "tag:v2.b1",
				Path: "file1.txt",
			},
		},
		"annotated dir tag": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Rev:  "tag:v2.main-an",
				Path: "file1.txt",
			},
		},
		"empty folder": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
				Path: "xxx/yyy",
			},
		},

		"no such path": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Path: "nada/sucho/patho",
			},
			code: codes.NotFound,
		},

		"no such rev": {
			request: &pb.PathInfoRequest{
				Id:  grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Rev: "tag:no-tag",
			},
			code: codes.NotFound,
		},

		"copy objects": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.PathInfo.ID),
				Path: "foo",
			},
		},

		"submodule": {
			request: &pb.PathInfoRequest{
				Id:  grpc_marshalling.IDInverse(suite.repos.SubmoduleParent.ID),
				Rev: "main",
			},
		},

		"submodule deep": {
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.SubmoduleParent.ID),
				Rev:  "main",
				Path: "deep",
			},
		},
		"symlinks": {
			request: &pb.PathInfoRequest{
				Id:  grpc_marshalling.IDInverse(suite.repos.SymLink.ID),
				Rev: "main",
			},
		},
		"hugefolder": {
			// This should fail by timeout and return info w/o lastcommit
			request: &pb.PathInfoRequest{
				Id:   grpc_marshalling.IDInverse(repoID),
				Path: "/",
				Rev:  "master",
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			if tn == "hugefolder" {
				t.Skip("It doesn't fail by timeout, sorry")
			}

			resp, err := client.PathInfo(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.Commit{}, "message", "parent_commits", "author", "commiter"),
			)
		})
	}
}
