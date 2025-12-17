package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIRepos() {
	t := suite.T()
	// test repos
	repos := []*entities.Repository{
		suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
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
		}),
		suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
			OrgID:         suite.orgs.Yandex.ID,
			Name:          "Second repo",
			Slug:          "second-repo",
			Description:   "Second repo description",
			Visibility:    entities.Visibilities.Private,
			DefaultBranch: utils.PtrFromValue(plumbing.Master.Short()),
			Authenticator: suite.getFakeAuthenticator(suite.users.Admin.Identity),
		}),
		suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
			OrgID:         suite.orgs.Smeshariki.ID,
			Name:          "Third repo",
			Slug:          "third-repo",
			Description:   "Third repo description",
			Visibility:    entities.Visibilities.Public,
			DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
			Authenticator: suite.getFakeAuthenticator(suite.users.Admin.Identity),
		}),
	}
	deletedRepo := suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
		OrgID:         suite.orgs.Yandex.ID,
		Name:          "Deleted repo",
		Slug:          "deleted-repo",
		Visibility:    entities.Visibilities.Public,
		Authenticator: suite.getFakeAuthenticator(suite.users.Admin.Identity),
	})
	err := suite.RepoService.DeleteRepository(context.Background(), deletedRepo.ID, suite.users.Admin.ID)
	require.NoError(t, err)

	// fill in counters
	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:      repos[0].ID,
		Title:       "First issue",
		Description: "First issue description",
	})
	suite.primitivePush(t, suite.users.Kopatych, repos[0], plumbing.ReferenceName("refs/heads/test"), true)
	suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repos[0],
		Title:  "First pull request",
		Source: "main",
		Target: "test",
	})

	suite.forkRepo(t, suite.users.Kopatych, grpc_marshalling.IDInverse(repos[0].ID))
	suite.createTag(suite.users.Kopatych, repos[0], plumbing.Main, "tag")

	_ = repos

	suite.addOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex, iam.Roles.RepositoriesMaintainer)

	t.Run("create (defaults), by org slug", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateRepositoryBody{
				Name: "My new repo",
				Slug: "my-new-repo",
			}).
			Post(fmt.Sprintf("/orgs/%s/repos", suite.orgs.Yandex.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/last_updated", "**/id", "template_type")
	})

	t.Run("create (defaults), by org id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateRepositoryBody{
				Name: "My new repo",
				Slug: "my-new-repo-2",
			}).
			Post(fmt.Sprintf("/orgs/id:%s/repos", suite.orgs.Yandex.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/last_updated", "**/id", "template_type")
	})

	t.Run("create with not existing org", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateRepositoryBody{
				Name: "My new repo",
				Slug: "my-new-repo-2",
			}).
			Post("/orgs/not-existing-org/repos")

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	t.Run("create (explicit)", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateRepositoryBody{
				Name:        "My new repo",
				Slug:        "my-new-repo-3",
				Description: "This is my repo",
				Visibility:  pbPub.Repository_public,
				InitSettings: &pbPub.CreateRepositoryBody_InitSettings{
					DefaultBranch: utils.PtrFromValue("default/branch"),
					CreateReadme:  true,
				},
			}).
			Post(fmt.Sprintf("/orgs/%s/repos", suite.orgs.Yandex.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/last_updated", "**/id", "template_type")
	})

	t.Run("get", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s", suite.orgs.Yandex.Slug, repos[0].Slug))
		require.NoError(t, err)

		resp2, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/id:%s", repos[0].UUID.String()))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/last_updated", "**/id", "template_type")
		yarequire.HTTPCompareWithFixture(t, resp2, "**/last_updated", "**/id", "template_type")
	})

	t.Run("get without permission", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Get(fmt.Sprintf("/repos/id:%s", repos[1].UUID.String()))
		require.NoError(t, err)

		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	t.Run("get deleted", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/id:%s", deletedRepo.UUID.String()))
		require.NoError(t, err)

		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/message", "**/request_id")
	})

	t.Run("with options", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
		gitignoreClient := pb.NewGitignorePresetsServiceClient(suite.grpcClient)

		gitignorePresets := []*pb.CreateGitignorePresetRequest{
			{
				Name: "Go",
				Content: `
# some comment
file

# another comment
file*`,
			},
			{
				Name: "Git",
				Content: `
a/*
b/*`,
			},
		}

		gitStringPresets := make([]string, 0)

		for _, preset := range gitignorePresets {
			_, err := gitignoreClient.Create(ctx, preset)
			require.NoError(t, err)
			gitStringPresets = append(gitStringPresets, preset.Name)
		}

		licensePresets := []*pb.CreateLisencePresetRequest{
			{
				Slug: "MIT",
				License: `
License

Some text.
`,
			},
		}

		licensePreset := "MIT"

		licenseClient := pb.NewLicensePresetsServiceClient(suite.grpcClient)
		for _, preset := range licensePresets {
			preset.License = strings.TrimSpace(preset.License)
			_, err := licenseClient.Create(ctx, preset)
			require.NoError(t, err)
		}

		srcYamlTemplate := `
on:
  # Triggers the workflow on push or pull request events but only for the "{{ .DefaultBranch }}" branch
  pull_request:
    - workflows: [simple-workflow]
      filter:
        paths: ["{{ .DefaultBranch }}"]

  push:
    - workflows: [simple-workflow]
      filter:
        branches: ["{{ .DefaultBranch }}"]

workflows:
  simple-workflow:
    tasks:
      - name: simple-task
        cubes:
          - name: simple-cube
            script:
              - echo "It's a simple CI run for {{ .OrgSlug }}/{{ .RepoSlug }}"
`
		suite.SrcYamlTemplatesRepository.Create(ctx, &entities.SrcYamlTemplate{
			Slug:    "simple-workflow",
			Content: srcYamlTemplate,
		})

		err := suite.RepoRepo.UpdateRepository(suite.repos.Alpha.Slug).
			SetTemplateType(&entities.TemplateTypes.Organization).
			Commit(ctx)

		require.NoError(t, err)

		yaml := "simple-workflow"

		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateRepositoryBody{
				Name:        "opt",
				Slug:        "opt",
				Description: "opt",
				Visibility:  pbPub.Repository_public,
				InitSettings: &pbPub.CreateRepositoryBody_InitSettings{
					DefaultBranch:       utils.PtrFromValue("default/branch"),
					CreateReadme:        true,
					GitignorePresets:    gitStringPresets,
					LicenseSlug:         &licensePreset,
					SrcYamlTemplateSlug: &yaml,
				},
				TemplatingOptions: &pbPub.CreateRepositoryBody_TemplatingOptions{
					TemplateId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				},
			}).
			Post(fmt.Sprintf("/orgs/%s/repos", suite.orgs.Yandex.Slug))

		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode())
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/message", "**/request_id", "id", "last_updated", "organization")

		repo, err := suite.RepoRepo.GetRepository(ctx, suite.orgs.Yandex.Slug, "opt")
		require.NoError(t, err)

		require.Equal(t, suite.repos.Alpha.ID, *repo.FromTemplate)
	})
}

func (suite *RwApiTestSuite) TestPublicApiDeleteRepo() {
	t := suite.T()
	t.Run("happy path", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			Delete(fmt.Sprintf("/%s/%s", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNoContent)
		resp, err = suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/id:%s", suite.repos.Alpha.UUID.String()))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repo, err := suite.RepoService.Get(ctx, repoID)
	require.NoError(t, err)
	t.Run("dont have permissions for delete repo", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			Delete(fmt.Sprintf("/repos/id:%s", repo.GetUUID().String()))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
		resp, err = suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})
	t.Run("happy path by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			Delete(fmt.Sprintf("/repos/id:%s", repo.GetUUID().String()))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNoContent)
		resp, err = suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})
	t.Run("delete migrated repo", func(t *testing.T) {
		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)
		_, err = suite.MigratedRepoRepo.Create(ctx, &entities.MigratedRepository{
			ID:         repoID,
			URL:        "https://github.com/example/example",
			Domain:     "github.com",
			SyncedRefs: []string{"*"},
			Mirror:     false,
		})
		require.NoError(t, err)
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			Delete(fmt.Sprintf("/%s/%s", repo.OrgSlug, repo.Slug))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNoContent)
		resp, err = suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/id:%s", repo.UUID.String()))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})
}

func (suite *RwApiTestSuite) TestPublicAPIUpdateRepo() {
	t := suite.T()

	protocol := suite.HTTPSProtocol()
	repoURL := protocol.RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:   suite.users.Admin,
		branch: "develop",
		parentBranch: func() string {
			if suite.repos.Alpha.DefaultBranch == nil {
				return ""
			}
			return *suite.repos.Alpha.DefaultBranch
		}(),
	})

	t.Run("happy path", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					Description:   "ikol",
					Visibility:    pbPub.Repository_private,
					DefaultBranch: "develop",
					Links: []*pbPub.Link{
						{
							Link: "https://example.com",
							Type: pbPub.Link_default,
						},
						{
							Link: "qwer@mail.ru",
							Type: pbPub.Link_email,
						},
						{
							Link: "https://poi.us",
							Type: pbPub.Link_homepage,
						},
					},
					TemplateType: pbPub.Repository_organizational,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "id", "name", "last_updated")
	})

	t.Run("happy path by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					Description:   "prokol",
					DefaultBranch: "develop",
					Visibility:    pbPub.Repository_internal,
					Links: []*pbPub.Link{
						{
							Link: "https://example.org",
							Type: pbPub.Link_default,
						},
					},
					TemplateType: pbPub.Repository_not_a_template,
				},
			).
			Patch(fmt.Sprintf("/repos/id:%s", suite.repos.Alpha.GetUUID().String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "id", "name", "last_updated")
	})

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("access denied", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Barash.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					Description: "ikol",
					Visibility:  pbPub.Repository_private,
					Links: []*pbPub.Link{
						{
							Link: "https://example.com",
							Type: pbPub.Link_default,
						},
						{
							Link: "qwer@mail.ru",
							Type: pbPub.Link_email,
						},
						{
							Link: "https://poi.us",
							Type: pbPub.Link_homepage,
						},
					},
					TemplateType: pbPub.Repository_organizational,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "request_id", "message")
	})

	someRepo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		Name:       "f",
		OrgID:      suite.orgs.Yandex.ID,
		Slug:       "p",
		Visibility: entities.Visibilities.Public,
	})

	t.Run("try to make system template in org that can not make system templates", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					TemplateType: pbPub.Repository_system,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", someRepo.OrgSlug, someRepo.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "request_id", "message")
	})

	err := suite.Params.OrgRepo.UpdateOrganizationByID(suite.orgs.Yandex.ID).
		SetCanCreateSystemTemplates(true).
		Commit(ctx)
	require.NoError(t, err)

	t.Run("make system template in org that can make system templates", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					TemplateType: pbPub.Repository_system,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", someRepo.OrgSlug, someRepo.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "default_branch", "description", "id", "is_empty", "links", "name", "slug", "visibility")
	})

	_, orgID, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Barash, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "sourcecraft",
		Claims:     entities.OrganizationClaims{Name: "SourceCraft"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	repoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		OrgID:      orgID,
		Name:       "a",
		Slug:       "fff",
		CreatedBy:  suite.users.Barash.ID,
		Visibility: entities.Visibilities.Internal,
	})

	require.NoError(t, err)
	barashRepo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)
	org, err := suite.OrgService.GetOrganizationByID(ctx, nil, orgID)
	require.NoError(t, err)

	suite.addOrgRole(t, suite.users.Kopatych, org, iam.Roles.OrganizationManagerAdmin)

	t.Run("try to make org template by member with only orgs permissions", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					TemplateType: pbPub.Repository_organizational,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", barashRepo.OrgSlug, barashRepo.Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "default_branch", "description", "id", "is_empty", "links", "name", "slug", "visibility", "template_type")

	})

	suite.addRole(t, suite.users.Krosh, barashRepo, iam.Roles.RepositoriesAdmin)

	t.Run("try to make org template by member with only repo permissions", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					TemplateType: pbPub.Repository_organizational,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", barashRepo.OrgSlug, barashRepo.Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "request_id", "message")
	})

	suite.addRole(t, suite.users.Kopatych, barashRepo, iam.Roles.RepositoriesAdmin)

	t.Run("creating org template", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(
				pbPub.UpdateRepositoryBody{
					TemplateType: pbPub.Repository_organizational,
				},
			).
			Patch(fmt.Sprintf("/%s/%s", barashRepo.OrgSlug, barashRepo.Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "default_branch", "description", "id", "is_empty", "links", "name", "slug", "visibility")
	})
}
