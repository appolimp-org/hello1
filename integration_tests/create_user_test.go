package integrationtests

import (
	"context"
	"gitcore/internal/entities"
	userservice "gitcore/internal/services/user"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestCreateUser() {
	//  OO-3436

	t := suite.T()

	// org share same slugspace with users, so here we will have a clash:

	_, _, err := suite.OrgService.CreateOrganization(context.Background(),
		nil,
		suite.users.Barash,
		entities.IdentityProviders.SelfHosted,
		entities.Organization{
			Slug:       "linus",
			Visibility: entities.Visibilities.Public,
			Claims:     entities.OrganizationClaims{Name: "Linus"},
		})
	require.NoError(t, err)

	newUser := entities.UserIdentity{
		ID:  "linus",
		Src: entities.IdentityProviders.IAM,
	}

	ctx := testutils.AuthorizeGRPC(newUser)
	c := pb.NewMeServiceClient(suite.grpcClient)

	r, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})

	require.NoError(t, err)
	require.Equal(t, "Linus", r.Profile.DisplayName)
	require.Equal(t, "linus-1", r.Profile.Username) // here we should have unique hash
	require.Equal(t, pb.ProfileVisibility_PROFILE_PUBLIC, r.Profile.Visibility)

	suite.WaitForWorkflows(suite.T(), userservice.CreatePersonalOrgWorkflowType)

	user, err := suite.UserRepo.GetUser(ctx, newUser)
	require.NoError(t, err)

	_, err = suite.OrgService.GetPersonalOrganization(ctx, nil, user)
	require.NoError(t, err)
}

func (suite *RwApiTestSuite) TestCreateUserWithSlugBlacklistedByRegex() {
	t := suite.T()

	bannedRegex := ".*very-bad-slug-999.*"
	bannedSlug := "very-bad-slug-999"

	_, _, err := suite.OrgService.CreateOrganization(context.Background(),
		nil,
		suite.users.Barash,
		entities.IdentityProviders.SelfHosted,
		entities.Organization{
			Slug:       "my-org",
			Visibility: entities.Visibilities.Public,
			Claims:     entities.OrganizationClaims{Name: "my-org"},
		})
	require.NoError(t, err)

	newUser := entities.UserIdentity{
		ID:  bannedSlug,
		Src: entities.IdentityProviders.IAM,
	}

	ctx := testutils.AuthorizeGRPC(newUser)
	c := pb.NewMeServiceClient(suite.grpcClient)

	_, err = c.GetProfile(ctx, &pb.GetMyProfileRequest{})
	require.NoError(t, err)

	suite.WaitForWorkflows(suite.T(), userservice.CreatePersonalOrgWorkflowType)

	user, err := suite.UserRepo.GetUser(ctx, newUser)
	require.NoError(t, err)
	require.NotRegexp(t, bannedRegex, user.Username)
	require.NotContains(t, bannedSlug, user.Username)

	_, err = suite.OrgService.GetPersonalOrganization(ctx, nil, user)
	require.NoError(t, err)
}
