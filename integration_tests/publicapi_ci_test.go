package integrationtests

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"

	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	pb_pub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
)

func (suite *RwApiTestSuite) TestPublicAPICI() {
	t := suite.T()

	repo := suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
		OrgID:         suite.orgs.Yandex.ID,
		Name:          "First repo",
		Slug:          "first-repo",
		Description:   "First repo description",
		Visibility:    entities.Visibilities.Public,
		DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
		Authenticator: suite.getFakeAuthenticator(suite.users.Admin.Identity),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true,
		},
		Profile: entities.RepositoryProfile{
			Links: []*entities.Link{
				{
					Type: entities.LinkTypes.Default,
					Link: "https://first.link",
				},
			},
		},
	})
	suite.addDefaultOYaml(repo, "main", oyaml.CIPath)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	suite.OrgService.AddUser(ctx, nil, suite.orgs.Yandex.Identity, suite.users.Kopatych.Identity)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.Admin)
	suite.addOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex, iam.Roles.OrganizationManagerOrganizationsOwner)

	workflowName := "target-workflow"

	t.Run("succesfull ci run", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pb_pub.RunCIBody{
				Revision:         "main",
				WorkflowRevision: "main",
				Input: &pb_pub.WorkflowInput{
					Values: []*pb_pub.RunWorkflowsBody_WorkflowData_InputValue{
						{
							Name:  "iop",
							Value: "poi",
						},
					},
				},
			}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				suite.orgs.Yandex.Slug,
				repo.Slug,
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "flux_id", "flux_public_id")
	})

	t.Run("succesfull ci run with default values", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				suite.orgs.Yandex.Slug,
				repo.Slug,
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "flux_id", "flux_public_id")
	})

	t.Run("succesfull ci run with default values by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"/repos/id:%s/ci_workflows/%s/trigger",
				repo.UUID.String(),
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "flux_id", "flux_public_id")
	})

	t.Run("try to start ci without permissions", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				suite.orgs.Yandex.Slug,
				repo.Slug,
				workflowName,
			))
		require.NoError(t, err)
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "request_id")
	})

	t.Run("invalid arguments", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				"////asf//sdg/ajrgbqerkjwgb",
				"w/dbg/sdbg/s/d/b/g/ljbd",
				"ipoopuopjpjpijpihpihp",
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "flux_id", "flux_public_id", "request_id")
	})

	emptyRepo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		Name:       "f",
		OrgID:      suite.orgs.Yandex.ID,
		Slug:       "p",
		Visibility: entities.Visibilities.Public,
		IsEmpty:    true,
	})

	t.Run("try to start workflow in empty repo", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				suite.orgs.Yandex.Slug,
				emptyRepo.Slug,
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "message", "request_id")
	})

	err := suite.RepoRepo.UpdateRepository(suite.repos.Alpha.Slug).SetTemplateType(&entities.TemplateTypes.System).Commit(ctx)
	require.NoError(t, err)

	suite.addDefaultOYaml(suite.repos.Alpha, "master", oyaml.CIPath)

	t.Run("try to start workflow in template repo", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pb_pub.RunCIBody{}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				suite.repos.Alpha.OrgSlug,
				suite.repos.Alpha.Slug,
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "message", "request_id")
	})

	anotherRepo, _ := suite.createRepo(t, suite.users.Admin, "ghj", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc_marshalling.IDInverse(suite.orgs.Yandex.ID))
	cmts := suite.addCommits(t, anotherRepo.CloneUrl.Https, suite.users.Admin, commitsOptions{id: "original1"})
	entityAnotherRepo, err := suite.RepoService.GetBySlugs(testutils.AuthorizeGRPC(suite.users.Admin.Identity), anotherRepo.OrgSlug, anotherRepo.Slug)
	require.NoError(t, err)
	suite.addDefaultOYaml(entityAnotherRepo, *entityAnotherRepo.DefaultBranch, oyaml.CIPath)
	require.NotZero(t, len(cmts))

	t.Run("try to start workflow with commit", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(&pb_pub.RunCIBody{
				Revision: cmts[0].String(),
			}).
			Post(fmt.Sprintf(
				"%s/%s/ci_workflows/%s/trigger",
				anotherRepo.OrgSlug,
				anotherRepo.Slug,
				workflowName,
			))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "message", "request_id")
	})
}
