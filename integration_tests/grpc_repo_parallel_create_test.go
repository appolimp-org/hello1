package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"sync"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcCreateRepoLoadTest() {
	t := suite.T()
	t.Skip("Local testing only")

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	projID, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)
	projIDStr := grpc_marshalling.IDInverse(projID)

	nRuns := 20
	wg := sync.WaitGroup{}
	wg.Add(nRuns)

	for i := 0; i < nRuns; i++ {
		go func() {
			request := &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        fmt.Sprintf("foo-%d", i),
				ProjectId:   &projIDStr,
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			}
			_, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			wg.Done()
		}()
	}

	wg.Wait()
}
