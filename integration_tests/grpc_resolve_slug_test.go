package integrationtests

import (
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestSlugServiceResolveSlug() {
	t := suite.T()
	client := pb.NewSlugServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	johnSnow86, err := suite.UserRepo.CreateUser(ctx, entities.User{
		Username: "johnsnow99",
		Identity: entities.UserIdentity{
			ID:  "johnsnow99",
			Src: entities.IdentityProviders.IAM,
		},
		Email:      "johnsnow99@mail.net",
		Visibility: entities.Visibilities.Private,
		Status:     entities.UserStatuses.Active,
	})

	require.NoError(t, err)

	tests := []struct {
		name        string
		slug        string
		wantError   bool
		expectError error
		orgID       *uint64
		userID      *uint64
	}{
		{
			name:      "not found",
			slug:      "saosidjsadoijsdiojsadjoisdasadijo",
			wantError: true,
		},
		{
			name:      "only org",
			slug:      suite.orgs.Yandex.Slug,
			wantError: false,
			orgID:     &suite.orgs.Yandex.ID,
		}, {
			name:      "only user",
			slug:      johnSnow86.Username,
			wantError: false,
			userID:    &johnSnow86.ID,
		}, {
			name:      "user and org",
			slug:      suite.users.Kopatych.Username,
			wantError: false,
			orgID:     suite.users.Kopatych.PersonalOrgID,
			userID:    &suite.users.Kopatych.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &pb.ResolveSlugRequest{
				Slug: tt.slug,
			}
			resp, err := client.ResolveSlug(ctx, request)
			if tt.wantError {
				require.Error(t, err)
				if tt.expectError != nil {
					require.ErrorIs(t, err, tt.expectError)
				}
				return
			}

			require.NoError(t, err)

			if tt.userID != nil {
				require.NotNil(t, resp.UserId)
				UserID := grpc_marshalling.IDInverse(*tt.userID)
				require.Equal(t, UserID, *resp.UserId)
			} else {
				fmt.Printf("RESPONSE: %#v", resp)
				require.Nil(t, resp.UserId)
			}

			if tt.orgID != nil {
				require.NotNil(t, resp.OrganizationId)
				OrgID := grpc_marshalling.IDInverse(*tt.orgID)
				require.Equal(t, OrgID, *resp.OrganizationId)
			} else {
				require.Nil(t, resp.OrganizationId)
			}
		})
	}

}
