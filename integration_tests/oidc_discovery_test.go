package integrationtests

import (
	"common/utils"
	"context"
	"encoding/json"
	"fmt"
	"gitcore/internal/access/common"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"net/url"
	"testing"
	"time"
)

func testJoinPath(t *testing.T, baseString string, elem ...string) string {
	r, err := url.JoinPath(baseString, elem...)
	require.NoError(t, err)
	return r
}

func (suite *RepoApiTestSuite) TestOIDCDiscovery() {
	t := suite.T()
	issuer := suite.cfg.OIDC.Issuer

	for i, client := range []*testutils.HTTPTestClient{suite.gwClient, suite.client} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			resp, err := client.Anonymous().
				Get(testJoinPath(t, "/oidc", oidc.DiscoveryEndpoint))

			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode())
			require.NotEmpty(t, resp.Body())

			var conf oidc.DiscoveryConfiguration
			err = json.Unmarshal(resp.Body(), &conf)
			require.NoError(t, err)
			require.Equal(t, conf.Issuer, issuer)
		})
	}

}

func (suite *RwApiTestSuite) TestOIDCJwks() {
	for i, client := range []*testutils.HTTPTestClient{suite.gwClient, suite.client} {
		suite.Run(fmt.Sprintf("-%d", i), func() {
			t := suite.T()
			keySet := getCerts(t, client)
			require.Len(t, keySet.Keys, 0)

			// create first certificate
			_, _, err := suite.PatService.CreateJWT(
				context.Background(),
				suite.users.Pikachu,
				nil,
				entities.PAT{
					PATParams: entities.PATParams{
						Name:        "test",
						Description: "test",
						ExpiresAt:   utils.PtrFromValue(time.Now().Add(time.Second)),
						IsService:   false,
					},
				},
			)
			require.NoError(t, err)

			keySet = getCerts(t, client)
			require.Len(t, keySet.Keys, 1)
		})
	}
}

func getCerts(t *testing.T, client *testutils.HTTPTestClient) jose.JSONWebKeySet {
	resp, err := client.Anonymous().
		Get(testJoinPath(t, "/oidc", common.JWKSEndpoint))

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	require.NotEmpty(t, resp.Body())

	var keySet jose.JSONWebKeySet
	err = json.Unmarshal(resp.Body(), &keySet)
	require.NoError(t, err)
	return keySet
}
