package integrationtests

import (
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcGetIssueStatus() {
	t := suite.T()
	client := pb.NewIssueStatusServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("get by ID", func(t *testing.T) {
		res, err := client.Get(ctx, &pb.GetIssueStatusRequest{
			Id: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Open.ID),
		})
		require.NoError(t, err)
		require.Equal(t, grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Open.ID), res.Id)
		require.Equal(t, "Open", res.Name)
		require.Equal(t, "open", res.Slug)
		require.Equal(t, pb.IssueStatus_STATUS_TYPE_INITIAL, res.StatusType)
	})

	t.Run("not found by ID", func(t *testing.T) {
		_, err := client.Get(ctx, &pb.GetIssueStatusRequest{
			Id: "123456789",
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcListIssueStatuses() {
	t := suite.T()
	client := pb.NewIssueStatusServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	res, err := client.List(ctx, &pb.ListIssueStatusesRequest{})
	require.NoError(t, err)
	require.Len(t, res.Statuses, len(entities.AllIssueStatuses))

	for i, expectedStatus := range entities.AllIssueStatuses {
		actualStatus := res.Statuses[i]
		require.Equal(t, grpc_marshalling.ShortIDInverse(expectedStatus.ID), actualStatus.Id)
		require.Equal(t, expectedStatus.Name, actualStatus.Name)
		require.Equal(t, expectedStatus.Slug, actualStatus.Slug)
		require.Equal(t, expectedStatus.StatusType, grpc_marshalling.IssueStatusTypeDirect(actualStatus.StatusType))
	}
}
