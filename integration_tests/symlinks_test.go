package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestPushAfterSymlink() {
	t := suite.T()
	protocol := suite.HTTPSProtocol()
	suite.addRole(t, suite.users.Kopatych, suite.repos.SymLink, iam.Roles.RepositoriesDeveloper)
	repoURL := protocol.RepoURL("yandex", "symlinks")

	tmpDir := testutils.TempDir(t, "", "symlinks")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych).
		Must(t, "clone", repoURL, "symlinks1")

	w1 := w.ChildDir("symlinks1")
	cg := protocol.PrepareCGit(w1, testutils.UserIdentities.Kopatych)
	cg.Must(t, "checkout", "main")

	fn := "testPush.txt"
	w1.MkFile(fn, "another file in main dir")
	cg.Must(t, "add", fn)
	cg.Must(t, "commit", "-m", "pushfile\n")
	cg.Must(t, "push", "-u", "origin", "main")

	t.Run("ok", func(t *testing.T) {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

		resp, err := client.PathInfo(ctx, &pb.PathInfoRequest{
			Id:  grpc_marshalling.IDInverse(suite.repos.SymLink.ID),
			Rev: "main",
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		// yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.DirEntry{}, "last_commit"),
		)
	})
}
