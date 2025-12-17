package integrationtests

import (
	"gitcore/internal/entities"
	userservice "gitcore/internal/services/user"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpc_status "google.golang.org/grpc/status"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"sync"
	"testing"
)

func (suite *RwApiTestSuite) TestSignup() {
	newUser := entities.UserIdentity{
		ID:  "new_user_me_service_test_sign_up",
		Src: entities.IdentityProviders.IAM,
	}

	ctx := testutils.AuthorizeGRPC(newUser)
	c := pb.NewMeServiceClient(suite.grpcClient)

	suite.T().Run("User is not registered yet => failed precondition on any handle except for GetProfile", func(t *testing.T) {
		_, err := pb.NewRepoServiceClient(suite.grpcClient).ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{
			OrgId: entities.Uint64EntityID(suite.orgs.Yandex.ID).String(),
		})
		require.Error(t, err)
		status, ok := grpc_status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.FailedPrecondition, status.Code())

		_, err = pb.NewUserServiceClient(suite.grpcClient).GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Id{Id: entities.Uint64EntityID(suite.users.Kopatych.ID).String()},
		})
		require.Error(t, err)
		status, ok = grpc_status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.FailedPrecondition, status.Code())
	})

	suite.T().Run("MeService/GetProfile signs user up", func(t *testing.T) {
		resp, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.AccessStatus_ACCESS_STATUS_PERSONAL_ORG_CREATING, resp.AccessStatus)
		resp, err = c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.AccessStatus_ACCESS_STATUS_PERSONAL_ORG_CREATING, resp.AccessStatus)

		suite.WaitForWorkflows(t, userservice.CreatePersonalOrgWorkflowType)
		resp, err = c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Contains(t,
			[]pb.AccessStatus{pb.AccessStatus_ACCESS_STATUS_NEW, pb.AccessStatus_ACCESS_STATUS_HAS_ACCESS},
			resp.AccessStatus,
		)
	})

	suite.T().Run("User is registered => we get no error", func(t *testing.T) {
		_, err := pb.NewRepoServiceClient(suite.grpcClient).ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{
			OrgId: entities.Uint64EntityID(suite.orgs.Yandex.ID).String(),
		})
		require.NoError(t, err)

		_, err = pb.NewUserServiceClient(suite.grpcClient).GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Id{Id: entities.Uint64EntityID(suite.users.PinPublic.ID).String()},
		})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestSignup_ParallelRequests() {
	newUser := entities.UserIdentity{
		ID:  "new_user_me_service_test_sign_up_parallel",
		Src: entities.IdentityProviders.IAM,
	}

	ctx := testutils.AuthorizeGRPC(newUser)
	c := pb.NewMeServiceClient(suite.grpcClient)

	suite.T().Run("User does not have orgs", func(t *testing.T) {
		orgs, err := suite.OrgService.ListOrganizations(ctx, testutils.NewStubAuthenticator(&newUser), newUser)
		require.NoError(t, err)
		require.Empty(t, orgs)
	})

	suite.T().Run("MeService/GetProfile handles parallel requests without duplicating orgs", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
				require.NoError(t, err)
				require.Contains(t,
					[]pb.AccessStatus{
						pb.AccessStatus_ACCESS_STATUS_NEW,
						pb.AccessStatus_ACCESS_STATUS_HAS_ACCESS,
						pb.AccessStatus_ACCESS_STATUS_PERSONAL_ORG_CREATING,
					},
					resp.AccessStatus,
				)
			}()
		}
		wg.Wait()
		suite.WaitForWorkflows(t, userservice.CreatePersonalOrgWorkflowType)

		orgs, err := suite.OrgService.ListOrganizations(ctx, testutils.NewStubAuthenticator(&newUser), newUser)
		require.NoError(t, err)
		require.Len(t, orgs, 1)

		resp, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Contains(t,
			[]pb.AccessStatus{pb.AccessStatus_ACCESS_STATUS_NEW, pb.AccessStatus_ACCESS_STATUS_HAS_ACCESS},
			resp.AccessStatus,
		)
	})

	suite.T().Run("User only has one org", func(t *testing.T) {
		orgs, err := suite.OrgService.ListOrganizations(ctx, testutils.NewStubAuthenticator(&newUser), newUser)
		require.NoError(t, err)
		require.Len(t, orgs, 1)
	})
}

func (suite *RwApiTestSuite) TestSignup_ServiceAccount() {
	newUser := entities.UserIdentity{
		ID:               "new_user_me_service_test_sign_up_service_account",
		Src:              entities.IdentityProviders.IAM,
		IsServiceAccount: true,
	}

	ctx := testutils.AuthorizeGRPC(newUser)

	suite.T().Run("Service account sings up on an unrelated handle", func(t *testing.T) {
		_, err := pb.NewRepoServiceClient(suite.grpcClient).ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{
			OrgId: entities.Uint64EntityID(suite.orgs.Yandex.ID).String(),
		})
		require.NoError(t, err)
		status, ok := grpc_status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.OK, status.Code())
		user, err := suite.UserRepo.GetUser(ctx, newUser)
		require.NoError(t, err)
		require.Equal(t, newUser.ID, user.Identity.ID)
		require.Equal(t, newUser.Src, user.Identity.Src)
	})
}
