package integrationtests

import (
	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcMetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (suite *RwApiTestSuite) TestAccessService_Authenticate() {
	t := suite.T()

	accessClient := pb.NewAccessServiceClient(suite.grpcClient)

	t.Run("known user", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
		resp, err := accessClient.Authenticate(ctx, &pb.AuthenticateRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
		require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), resp.GetSubject().GetId())
	})

	t.Run("service account", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(entities.UserIdentity{
			ID:               "test_access_service_authenticate_service_account",
			Src:              entities.IdentityProviders.IAM,
			IsServiceAccount: true,
		})
		resp, err := accessClient.Authenticate(ctx, &pb.AuthenticateRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
		require.NotZero(t, resp.GetSubject().GetId())
	})

	t.Run("unknown user", func(t *testing.T) {
		ctx := grpcMetadata.AppendToOutgoingContext(context.Background(),
			"authorization", fmt.Sprintf("Bearer %s", testutils.FakeIAMAuthToken(entities.UserIdentity{
				ID:  "unknown",
				Src: entities.IdentityProviders.IAM,
			})))
		_, err := accessClient.Authenticate(ctx, &pb.AuthenticateRequest{})
		require.Error(t, err)
		e, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.FailedPrecondition, e.Code())
	})

	t.Run("anonymous user", func(t *testing.T) {
		ctx := context.Background()
		resp, err := accessClient.Authenticate(ctx, &pb.AuthenticateRequest{})
		require.NotNil(t, resp)
		require.NoError(t, err)
		require.Equal(t, resp.GetSubject().GetId(), "0")
	})
}

func (suite *RwApiTestSuite) TestAccessService_Org() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	bindingsClient := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)
	accessClient := pb.NewAccessServiceClient(suite.grpcClient)

	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgID := profile.Id
	orgSlug := profile.Slug
	orgIAMID := profile.OrgIdentity.Id

	obj := &pb.Object{Identifier: &pb.Object_OrgId{OrgId: orgID}}

	// List
	listRes, err := bindingsClient.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// owner by token
	ctx = testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	resp, err := accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Permission: string(iam.Permissions.OrganizationManagerUsersDelete),
		ResourcePath: &pb.Resource{
			Id:   orgID,
			Type: string(iam.EntityTypes.Organization),
		},
	})
	require.NoError(t, err)
	require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), resp.GetSubject().GetId())

	// owner by subject
	ctx = testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	resp, err = accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Subject: &pb.Subject{
			Type: pb.Subject_USER,
			Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
		},
		Permission: string(iam.Permissions.OrganizationManagerUsersDelete),
		ResourcePath: &pb.Resource{
			Id:   orgID,
			Type: string(iam.EntityTypes.Organization),
		},
	})
	require.NoError(t, err)
	require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), resp.GetSubject().GetId())

	// unknown user
	ctx = testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	resp, err = accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Subject: &pb.Subject{
			Type: pb.Subject_USER,
			Id:   grpc_marshalling.IDInverse(suite.users.Kopatych.ID),
		},
		Permission: string(iam.Permissions.OrganizationManagerUsersDelete),
		ResourcePath: &pb.Resource{
			Id:   orgID,
			Type: string(iam.EntityTypes.Organization),
		},
	})
	require.Nil(t, resp)
	require.Error(t, err)
	e, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, e.Code())

	// repository owner
	err = suite.MembershipRepo.Create(ctx, suite.users.Kopatych.Identity, entities.OrganizationIdentity{
		ID:  profile.OrgIdentity.GetId(),
		Src: entities.IdentityProviders.IAM,
	})
	require.NoError(t, err)

	httpErr := httperrors.APIError{}
	repoDetails := &schemas.RepoDetails{}
	testutils.Expect(suite.client.As(suite.users.Kopatych.Identity).
		SetError(&httpErr).
		SetResult(repoDetails).
		SetBody(&schemas.CreateRepositoryRequest{
			Name:    "repo",
			Slug:    "repo",
			OrgSlug: &orgSlug,
		}).
		Post("/api/v1/repos")).MustBe(t, 201)

	ctx = testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	resp, err = accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Permission: string(iam.Permissions.RepositoriesUpdatePRs),
		ResourcePath: &pb.Resource{
			Id:   string(repoDetails.ID),
			Type: string(iam.EntityTypes.Repository),
		},
	})
	require.NoError(t, err)
	require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Kopatych.ID), resp.GetSubject().GetId())
	require.Len(t, resp.GetFullResourcePaths(), 0)

	// organization owner
	ctx = testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	resp, err = accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Permission: string(iam.Permissions.RepositoriesUpdatePRs),
		ResourcePath: &pb.Resource{
			Id:   string(repoDetails.ID),
			Type: string(iam.EntityTypes.Repository),
		},
		WithFullPath: true,
	})
	require.NoError(t, err)
	require.Equal(t, pb.Subject_USER, resp.GetSubject().GetType())
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), resp.GetSubject().GetId())
	require.Len(t, resp.GetFullResourcePaths(), 2)
	require.Equal(t, string(iam.EntityTypes.Repository), resp.GetFullResourcePaths()[0].Type)
	require.Equal(t, string(repoDetails.ID), resp.GetFullResourcePaths()[0].Id)
	require.Equal(t, string(iam.EntityTypes.Organization), resp.GetFullResourcePaths()[1].Type)
	require.Equal(t, orgIAMID, resp.GetFullResourcePaths()[1].Id)

	//unknown user
	ctx = testutils.AuthorizeGRPC(suite.users.Barash.Identity)
	resp, err = accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Permission: string(iam.Permissions.RepositoriesUpdatePRs),
		ResourcePath: &pb.Resource{
			Id:   string(repoDetails.ID),
			Type: string(iam.EntityTypes.Repository),
		},
	})
	require.Nil(t, resp)
	require.Error(t, err)
	e, ok = status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, e.Code())
}

func (suite *RwApiTestSuite) TestAccessService_AuthenticateUserField() {
	t := suite.T()

	accessClient := pb.NewAccessServiceClient(suite.grpcClient)

	t.Run("user field is populated correctly", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
		resp, err := accessClient.Authenticate(ctx, &pb.AuthenticateRequest{})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.User{}, "public_id"))
	})
}

func (suite *RwApiTestSuite) TestAccessService_AuthorizeUserField() {
	t := suite.T()

	accessClient := pb.NewAccessServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Test Org",
		DisplayName: "Test Org",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))
	orgID := profile.Id

	resp, err := accessClient.Authorize(ctx, &pb.AuthorizeRequest{
		Permission: string(iam.Permissions.OrganizationManagerUsersDelete),
		ResourcePath: &pb.Resource{
			Id:   orgID,
			Type: string(iam.EntityTypes.Organization),
		},
	})
	require.NoError(t, err)

	//yarequire.ProtoDumpFixture(t, resp)
	yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.User{}, "public_id"), protocmp.IgnoreFields(&pb.Resource{}, "id"))
}
