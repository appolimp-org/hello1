package integrationtests

import (
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestRestrictInternalVisibility() {
	defer func() {
		suite.cfg.FeatureFlags.Visibilities.MeForbidden = nil
		suite.cfg.FeatureFlags.Visibilities.RepoForbidden = nil
		suite.cfg.FeatureFlags.Visibilities.ProjForbidden = nil
		suite.cfg.FeatureFlags.Visibilities.Prepare()
	}()
	suite.cfg.FeatureFlags.Visibilities.MeForbidden = []string{"internal"}
	suite.cfg.FeatureFlags.Visibilities.RepoForbidden = []string{"internal"}
	suite.cfg.FeatureFlags.Visibilities.ProjForbidden = []string{"internal"}
	suite.cfg.FeatureFlags.Visibilities.Prepare()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Admin)
	c := pb.NewRepoServiceClient(suite.grpcClient)

	grpcExpectedCode := codes.InvalidArgument
	grpcExpectedMessage := "Invalid argument: internal visibility is restricted"

	httpExpectedCode := http.StatusBadRequest
	httpExpectedMessage := "internal visibility is restricted"

	suite.T().Run("GRPC: Create repository with internal visibility", func(t *testing.T) {
		_, err := c.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:       "foo",
			Visibility: pb.ResourceVisibility_RESOURCE_INTERNAL,
		})
		checkGrpcCodeAndMessage(t, err, grpcExpectedCode, grpcExpectedMessage)
	})

	suite.T().Run("GRPC: Update repository with internal visibility", func(t *testing.T) {
		_, err := c.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:         grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
			Visibility: pb.ResourceVisibility_RESOURCE_INTERNAL,
		})
		checkGrpcCodeAndMessage(t, err, grpcExpectedCode, grpcExpectedMessage)
	})

	suite.T().Run("HTTP: Update user profile with internal visibility", func(t *testing.T) {
		request := schemas.UpdateUserProfileRequest{
			Visibility: &entities.Visibilities.Internal,
		}

		httpErr := httperrors.APIError{}
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			SetBody(request).
			SetError(&httpErr).
			Post("/api/v1/me")

		require.NoError(t, err)
		checkHTTPCodeAndMessage(t, resp, httpErr, httpExpectedCode, httpExpectedMessage)
	})

	suite.T().Run("HTTP: Create repository with internal visibility", func(t *testing.T) {
		request := schemas.CreateRepositoryRequest{
			Name:       "TestRepo",
			Slug:       "test-repo",
			OrgSlug:    &suite.orgs.Yandex.Slug,
			Visibility: &entities.Visibilities.Internal,
		}

		httpErr := httperrors.APIError{}
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			SetBody(request).
			SetError(&httpErr).
			Post("/api/v1/repos/")

		require.NoError(t, err)
		checkHTTPCodeAndMessage(t, resp, httpErr, httpExpectedCode, httpExpectedMessage)
	})

	suite.T().Run("HTTP: Update repository with internal visibility", func(t *testing.T) {
		request := schemas.UpdateRepositoryRequest{
			Visibility: &entities.Visibilities.Internal,
		}

		httpErr := httperrors.APIError{}
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			SetBody(request).
			SetError(&httpErr).
			Post("/api/v1/repos/yandex/alpha")

		require.NoError(t, err)
		checkHTTPCodeAndMessage(t, resp, httpErr, httpExpectedCode, httpExpectedMessage)
	})
}

func checkGrpcCodeAndMessage(t *testing.T, err error, code codes.Code, message string) {
	status, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, code, status.Code())
	require.Equal(t, message, status.Message())
}

func checkHTTPCodeAndMessage(t *testing.T, resp *resty.Response, httpErr httperrors.APIError, code int, message string) {
	require.Equal(t, code, resp.StatusCode())
	require.Equal(t, message, httpErr.Details)
}
