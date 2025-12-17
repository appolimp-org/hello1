package integrationtests

import (
	"gitcore/internal/config"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestGrpcGetFeatures() {
	t := suite.T()
	client := pb.NewFeaturesServiceClient(suite.grpcClient)
	user := suite.users.AuthViewer

	resp, err := client.Get(testutils.AuthorizeGRPC(user.Identity), &pb.GetFeaturesRequest{})
	require.NoError(t, err)
	require.NotZero(t, len(resp.FeaturesYaml))

	var features config.FeatureFlagsConfig
	err = yaml.Unmarshal([]byte(resp.FeaturesYaml), &features)
	require.NoError(t, err)
	features.Visibilities.Prepare()

	if len(features.Visibilities.MeForbidden) == 0 {
		features.Visibilities.MeForbidden = nil
	}
	if len(features.Visibilities.RepoForbidden) == 0 {
		features.Visibilities.RepoForbidden = nil
	}
	if len(features.Visibilities.ProjForbidden) == 0 {
		features.Visibilities.ProjForbidden = nil
	}
	if len(features.Visibilities.ForkForbidden) == 0 {
		features.Visibilities.ForkForbidden = nil
	}
	require.Equal(t, suite.cfg.FeatureFlags, features)
}
