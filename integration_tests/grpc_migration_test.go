package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGrpcMigrationService() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	client := pb.NewMigrationServiceClient(suite.grpcClient)

	tests := []struct {
		name             string
		url              string
		expectedProvider pb.GetProviderFeaturesResponse_Provider
		expectedFeatures []*pb.MigrationFeature
	}{
		{
			name:             "github http URL",
			url:              "https://github.com/foo/bar",
			expectedProvider: pb.GetProviderFeaturesResponse_PROVIDER_GITHUB,
			expectedFeatures: []*pb.MigrationFeature{
				{
					Feature: pb.MigrationFeature_FEATURE_TOKEN_CREDENTIALS,
					Enabled: true,
				},
				{
					Feature: pb.MigrationFeature_FEATURE_USERNAME_PASSWORD_CREDENTIALS,
					Enabled: false,
				},
				{
					Feature: pb.MigrationFeature_FEATURE_MIRROR_REPOSITORY,
					Enabled: false,
				},
			},
		},
		{
			name:             "github ssh URL",
			url:              "git@github.com:divkit/divkit.git",
			expectedProvider: pb.GetProviderFeaturesResponse_PROVIDER_GITHUB,
			expectedFeatures: []*pb.MigrationFeature{
				{
					Feature: pb.MigrationFeature_FEATURE_TOKEN_CREDENTIALS,
					Enabled: true,
				},
				{
					Feature: pb.MigrationFeature_FEATURE_USERNAME_PASSWORD_CREDENTIALS,
					Enabled: false,
				},
				{
					Feature: pb.MigrationFeature_FEATURE_MIRROR_REPOSITORY,
					Enabled: false,
				},
			},
		},
		{
			name:             "gitlab URL",
			url:              "https://gitlab.com/foo/bar",
			expectedProvider: pb.GetProviderFeaturesResponse_PROVIDER_UNKNOWN,
			expectedFeatures: []*pb.MigrationFeature{},
		},
		{
			name:             "empty URL",
			url:              "",
			expectedProvider: pb.GetProviderFeaturesResponse_PROVIDER_UNKNOWN,
			expectedFeatures: []*pb.MigrationFeature{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := client.GetProviderFeatures(ctx, &pb.GetProviderFeaturesRequest{
				Url: tt.url,
			})

			require.NoError(t, err)

			require.Equal(t, tt.expectedProvider, r.Provider)
			yarequire.ProtoElementsMatch(t, tt.expectedFeatures, r.Features)
		})
	}

	t.Run("Validate migration happy path", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
		r, err := client.Validate(ctx, &pb.ValidateMigrationRequest{
			Org: &pb.ValidateMigrationRequest_OrgSlug{OrgSlug: suite.orgs.Yandex.Slug},
			Url: "https://github.com/foo/bar",
			Credentials: &pb.MigrationCredentials{
				Creds: &pb.MigrationCredentials_Token{
					Token: "******************",
				},
			},
		})

		require.NoError(t, err)

		//var metadata pb.OperationMetadata
		//var response pb.ValidateMigrationResponse
		require.False(t, r.GetDone(), "operation is not done")
		//require.NoError(t, r.GetMetadata().UnmarshalTo(&metadata))
		//require.NoError(t, r.GetResponse().UnmarshalTo(&response))

		//require.Equal(t, pb.ValidateMigrationResponse_STATUS_UNSPECIFIED, response.GetStatus())

	})

	t.Run("Validate migration no credentials", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
		_, err := client.Validate(ctx, &pb.ValidateMigrationRequest{
			Org: &pb.ValidateMigrationRequest_OrgSlug{OrgSlug: suite.orgs.Yandex.Slug},
			Url: "https://github.com/foo/bar",
		})

		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestValidationOperationAccess() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	client := pb.NewMigrationServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)

	orgProfile := suite.createOrg(t, suite.users.Kopatych, "migration-test")
	require.NoError(t, suite.OrgRepo.UpdateOrganization(orgProfile.Slug).
		SetVisibility(entities.Visibilities.Private).
		Commit(ctx))

	orgID, err := grpc_marshalling.IDDirect(orgProfile.Id)
	require.NoError(t, err)
	org, err := suite.OrgRepo.GetOrganizationByID(ctx, orgID)
	require.NoError(t, err)

	suite.addOrgRole(t, suite.users.Krosh, org, iam.Roles.RepositoriesDeveloper)

	t.Run("public", func(t *testing.T) {
		resp, err := client.Validate(ctx, &pb.ValidateMigrationRequest{
			Org: &pb.ValidateMigrationRequest_OrgSlug{
				OrgSlug: org.Slug,
			},
			Url:        "https://github.com/example/example",
			Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		})
		require.NoError(t, err)
		opID := resp.Id

		_, err = opClient.Get(ctx, &pb.GetOperationRequest{
			Id: opID,
		})
		require.NoError(t, err)
		_, err = opClient.Get(testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh), &pb.GetOperationRequest{
			Id: opID,
		})
		require.NoError(t, err)
		_, err = opClient.Get(testutils.AuthorizeGRPC(testutils.UserIdentities.Barash), &pb.GetOperationRequest{
			Id: opID,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("private", func(t *testing.T) {
		resp, err := client.Validate(ctx, &pb.ValidateMigrationRequest{
			Org: &pb.ValidateMigrationRequest_OrgSlug{
				OrgSlug: org.Slug,
			},
			Url:        "https://github.com/example/example",
			Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		})
		require.NoError(t, err)
		opID := resp.Id

		_, err = opClient.Get(ctx, &pb.GetOperationRequest{
			Id: opID,
		})
		require.NoError(t, err)
		_, err = opClient.Get(testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh), &pb.GetOperationRequest{
			Id: opID,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
		_, err = opClient.Get(testutils.AuthorizeGRPC(testutils.UserIdentities.Barash), &pb.GetOperationRequest{
			Id: opID,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("old metadata spec", func(t *testing.T) {
		opID, err := suite.OpRepo.Create(ctx, &entities.Operation{
			Type:      entities.OperationTypes.ValidateMigration,
			Status:    entities.OperationStatuses.Scheduled,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			UserID:    suite.users.Kopatych.ID,
			IamObject: entities.IAMObject{
				Type: entities.ObjectTypes.Organization,
				ID:   orgID,
			},
			RawMetadata: []byte(`{"currentStep": "STEP_ISSUES"}`),
		})
		require.NoError(t, err)

		// if error arises in AccessService, it gets pushed through to GRPC response
		_, err = opClient.Get(ctx, &pb.GetOperationRequest{
			Id: opID,
		})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestMigratingRepoAccess() {
	t := suite.T()
	ctx := context.Background()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	orgProfile := suite.createOrg(t, suite.users.Kopatych, "migration-test")
	require.NoError(t, suite.OrgRepo.UpdateOrganization(orgProfile.Slug).
		SetVisibility(entities.Visibilities.Private).
		Commit(ctx))

	orgID, err := grpc_marshalling.IDDirect(orgProfile.Id)
	require.NoError(t, err)
	org, err := suite.OrgRepo.GetOrganizationByID(ctx, orgID)
	require.NoError(t, err)

	suite.addOrgRole(t, suite.users.Pikachu, org, iam.Roles.RepositoriesDeveloper)
	suite.addOrgRole(t, suite.users.Krosh, org, iam.Roles.RepositoriesDeveloper)
	suite.addOrgRole(t, suite.users.Barash, org, iam.Roles.RepositoriesMaintainer)
	suite.addOrgRole(t, suite.users.Slowpoke, org, iam.Roles.RepositoriesAdmin)

	repo, _ := suite.createRepo(t, suite.users.Pikachu, "migrating", pb.ResourceVisibility_RESOURCE_PUBLIC, orgProfile.Id)
	repoID, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)
	require.NoError(t, suite.RepoRepo.
		UpdateRepositoryByID(repoID).
		SetMigrationID(utils.PtrFromValue("stub-operation-id")).
		SetIsMigrating(true).
		Commit(ctx))

	for _, tc := range []struct {
		name         string
		user         *entities.User
		expectedCode codes.Code
	}{
		{
			name:         "org owner",
			user:         suite.users.Kopatych,
			expectedCode: codes.OK,
		},
		{
			name:         "repo owner",
			user:         suite.users.Pikachu,
			expectedCode: codes.OK,
		},
		{
			name:         "dev (no access)",
			user:         suite.users.Krosh,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "maintainer (no access)",
			user:         suite.users.Barash,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "admin",
			user:         suite.users.Slowpoke,
			expectedCode: codes.OK,
		},
		{
			name:         "anonymous",
			user:         nil,
			expectedCode: codes.Unauthenticated,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grpcCtx := ctx
			if tc.user != nil {
				grpcCtx = testutils.AuthorizeGRPC(tc.user.Identity)
			}
			_, err := client.Get(grpcCtx, &pb.GetRepositoryRequest{
				Repo: &pb.GetRepositoryRequest_Id{
					Id: repo.Id,
				},
			})
			yarequire.ProtoStatusEqual(t, tc.expectedCode, err)
		})
	}
}
