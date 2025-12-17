package integrationtests

import (
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/marshalling"
	"gitcore/internal/testutils"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestMeService_ResolveOrgDepsBulk() {
	t := suite.T()
	ctx := context.Background()

	tests := []struct {
		Name          string
		Organizations []*entities.Organization
		OrgDeps       map[entities.OrganizationIdentity]*marshalling.OrgDeps
	}{
		{
			Name:          "Empty",
			Organizations: []*entities.Organization{},
			OrgDeps:       map[entities.OrganizationIdentity]*marshalling.OrgDeps{},
		},
		{
			Name: "Sme",
			Organizations: []*entities.Organization{
				suite.orgs.Smeshariki,
			},
			OrgDeps: map[entities.OrganizationIdentity]*marshalling.OrgDeps{
				suite.orgs.Smeshariki.Identity: {
					Org:       suite.orgs.Smeshariki,
					OrgClaims: &suite.orgs.Smeshariki.Claims,
				},
			},
		},
		{
			Name: "Sme and Ya",
			Organizations: []*entities.Organization{
				suite.orgs.Smeshariki,
				suite.orgs.Yandex,
			},
			OrgDeps: map[entities.OrganizationIdentity]*marshalling.OrgDeps{
				suite.orgs.Smeshariki.Identity: {
					Org:       suite.orgs.Smeshariki,
					OrgClaims: &suite.orgs.Smeshariki.Claims,
				}, suite.orgs.Yandex.Identity: {
					Org:       suite.orgs.Yandex,
					OrgClaims: &suite.orgs.Yandex.Claims,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			authenticator := testutils.NewStubAuthenticator(&suite.users.Krosh.Identity)

			resp, err := suite.OrgService.ResolveOrgDepsBulk(ctx, authenticator, tt.Organizations)
			require.NoError(t, err)
			require.Equal(t, tt.OrgDeps, resp)
		})
	}

}
