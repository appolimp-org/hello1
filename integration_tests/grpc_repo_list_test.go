package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	"private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) populateRepos(t *testing.T, slugs []string, vis entities.Visibility, orgID uint64, author *entities.User, isMigrating bool, pkArray *[]uint64) {
	for _, slug := range slugs {
		repo, err := suite.Params.RepoService.Create(context.Background(), &interfaces.CreateRepositoryArgs{
			OrgID:         orgID,
			Name:          slug,
			Description:   slug,
			Slug:          slug,
			Visibility:    vis,
			Authenticator: access.StubAuthenticator,
			IsMigrating:   isMigrating,
			IsEmpty:       true,
		}, author)
		require.NoError(suite.T(), err)
		if pkArray != nil {
			*pkArray = append(*pkArray, repo.ID)
		}

		protocol := suite.HTTPSProtocol()
		repoURL := protocol.RepoURL(repo.OrgSlug, repo.Slug)
		suite.makeCommit(t, protocol, repoURL, commitArgs{
			user:       author,
			branch:     "master",
			commitTime: time.Now(),
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListMyRepos() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)
	_, org, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Kopatych, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "corpa",
		Claims:     entities.OrganizationClaims{Name: "A"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	// Krosh is contributor only
	repo1 := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		Name:          "repo1",
		Slug:          "repo1",
		OrgID:         org,
		ProjID:        nil,
		Visibility:    entities.Visibilities.Public,
		Authenticator: suite.getFakeAuthenticator(suite.users.Admin.Identity),
		DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true,
		},
	})
	suite.addRole(t, suite.users.Krosh, repo1, iam.Roles.RepositoriesDeveloper)
	suite.primitivePush(t, suite.users.Krosh, repo1, plumbing.NewBranchReferenceName("main"), false)

	// Krosh is both contributor and creator
	repo2 := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		Name:          "repo2",
		Slug:          "repo2",
		OrgID:         org,
		ProjID:        nil,
		Visibility:    entities.Visibilities.Public,
		Authenticator: suite.getFakeAuthenticator(suite.users.Krosh.Identity),
		DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true,
		},
	})
	require.NoError(t, err)
	suite.addRole(t, suite.users.Krosh, repo2, iam.Roles.RepositoriesDeveloper)
	suite.primitivePush(t, suite.users.Krosh, repo2, plumbing.NewBranchReferenceName("main"), false)

	// Krosh is creator only
	_ = suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		Name:          "repo3",
		Slug:          "repo3",
		OrgID:         org,
		ProjID:        nil,
		Visibility:    entities.Visibilities.Public,
		Authenticator: suite.getFakeAuthenticator(suite.users.Krosh.Identity),
		DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true,
		},
	})
	require.NoError(t, err)

	// Krosh is nobody
	_, err = suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Name:       "repo4",
		Slug:       "repo4",
		OrgID:      org,
		ProjID:     nil,
		CreatedBy:  suite.users.Kopatych.ID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	// Krosh lost his role
	repo5 := suite.makeRepo(suite.users.Kopatych, &interfaces.CreateRepositoryArgs{
		Name:          "repo5",
		Slug:          "repo5",
		OrgID:         org,
		ProjID:        nil,
		Visibility:    entities.Visibilities.Public,
		Authenticator: suite.getFakeAuthenticator(suite.users.Kopatych.Identity),
		DefaultBranch: utils.PtrFromValue(plumbing.Main.Short()),
		ProvisionArgs: &interfaces.RepositoryProvisionArgs{
			AddReadme: true,
		},
	})
	require.NoError(t, err)
	suite.addRole(t, suite.users.Krosh, repo5, iam.Roles.RepositoriesDeveloper)
	suite.primitivePush(t, suite.users.Krosh, repo5, plumbing.NewBranchReferenceName("main"), false)
	removed, err := suite.AccessBindingsService.DeleteBindings(context.Background(), access.StubAuthenticator, []*entities.AccessBinding{
		{Subject: suite.users.Krosh.Subject(), Object: repo5.Object(), Role: iam.Roles.RepositoriesDeveloper},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	resp, err := client.ListMyRepositories(ctx, &pb.ListMyRepositoriesRequest{UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID)})
	require.NoError(t, err)

	//todo: test combined paging
	// yarequire.ProtoDumpFixture(t, resp)
	yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))
}

func (suite *RwApiTestSuite) TestGrpcListRepos() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	_, org1, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Krosh, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "corpa",
		Claims:     entities.OrganizationClaims{Name: "A"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	_, org2, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Admin, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "corpb",
		Claims:     entities.OrganizationClaims{Name: "B"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	kroshOrg, err := suite.Params.OrgService.GetPersonalOrganization(ctx, nil, suite.users.Krosh)
	require.NoError(t, err)

	kopatychOrg, err := suite.Params.OrgService.GetPersonalOrganization(ctx, nil, suite.users.Kopatych)
	require.NoError(t, err)

	resp, err := client.DiscoverAllRepositories(ctx, &pb.DiscoverAllRepositoriesRequest{
		PageSize: utils.PtrFromValue(uint64(100)),
		SortBy: []*pagination.SortOption{
			{
				Column:    "last_updated",
				Direction: pagination.SortOption_ASC,
			},
		},
	})
	require.NoError(t, err)
	// All repos in Krosh's discovery
	kroshDiscoverRepos := functools.Map(resp.Repositories, (*pb.Repository).GetSlug)

	t.Run("discover by last_updated is sorted", func(t *testing.T) {
		for i := 1; i < len(resp.Repositories); i++ {
			require.LessOrEqual(t, resp.Repositories[i-1].LastUpdated.AsTime(), resp.Repositories[i].LastUpdated.AsTime())
		}
	})

	populateRepos := func(slugs []string, visibility entities.Visibility, orgID uint64, user *entities.User, isMigrating bool, pkArray *[]uint64) {
		kroshIsAdmin := user.ID == suite.users.Krosh.ID || orgID == kroshOrg.ID || orgID == org1
		if visibility == entities.Visibilities.Public && (kroshIsAdmin || !isMigrating) {
			kroshDiscoverRepos = append(kroshDiscoverRepos, slugs...)
		}
		suite.populateRepos(t, slugs, visibility, orgID, user, isMigrating, pkArray)
	}

	populateRepos([]string{"kroshprivate1", "kroshprivate2"}, entities.Visibilities.Private, kroshOrg.ID, suite.users.Krosh, false, nil)
	populateRepos([]string{"kroshpub1", "kroshpub2", "kroshpub3"}, entities.Visibilities.Public, kroshOrg.ID, suite.users.Krosh, false, nil)
	populateRepos([]string{"kroshmigrating1"}, entities.Visibilities.Private, kroshOrg.ID, suite.users.Krosh, true, nil)
	populateRepos([]string{"kroshmigrating2"}, entities.Visibilities.Public, kroshOrg.ID, suite.users.Krosh, true, nil)

	populateRepos([]string{"kop1", "kop2", "kop3", "kop4"}, entities.Visibilities.Public, kopatychOrg.ID, suite.users.Kopatych, false, nil)
	populateRepos([]string{"kopmigrating1"}, entities.Visibilities.Public, kopatychOrg.ID, suite.users.Kopatych, true, nil)
	populateRepos([]string{"kop5", "kop6", "kop7"}, entities.Visibilities.Private, kopatychOrg.ID, suite.users.Kopatych, false, nil)
	populateRepos([]string{"kopmigrating2"}, entities.Visibilities.Private, kopatychOrg.ID, suite.users.Kopatych, true, nil)
	populateRepos([]string{"kopmigrating3"}, entities.Visibilities.Private, kopatychOrg.ID, suite.users.Kopatych, true, nil)

	populateRepos([]string{"a", "b", "c"}, entities.Visibilities.Public, org1, suite.users.Admin, false, nil)
	populateRepos([]string{"x", "y", "z"}, entities.Visibilities.Private, org1, suite.users.Admin, false, nil)
	populateRepos([]string{"org1migrating1"}, entities.Visibilities.Public, org1, suite.users.Admin, true, nil)
	populateRepos([]string{"org1migrating2"}, entities.Visibilities.Private, org1, suite.users.Admin, true, nil)

	populateRepos([]string{"a", "b", "c"}, entities.Visibilities.Public, org2, suite.users.Admin, false, nil)
	populateRepos([]string{"org2migrating1"}, entities.Visibilities.Public, org2, suite.users.Admin, true, nil)
	populateRepos([]string{"x", "z"}, entities.Visibilities.Private, org2, suite.users.Admin, false, nil)
	populateRepos([]string{"org2migrating2"}, entities.Visibilities.Private, org2, suite.users.Admin, true, nil)

	_, orgCheckDefaultSort, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Krosh, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "check-default-sort",
		Claims:     entities.OrganizationClaims{Name: "CheckDefaultSort"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)
	populateRepos([]string{"sourcecraft-ui", "appsec"}, entities.Visibilities.Public, orgCheckDefaultSort, suite.users.Admin, false, nil)
	populateRepos([]string{"sourcecraft"}, entities.Visibilities.Private, orgCheckDefaultSort, suite.users.Admin, false, nil)

	slices.Reverse(kroshDiscoverRepos)

	type testcase struct {
		name          string
		userID        uint64
		orgID         uint64
		slug          string
		query         string
		expectedSlugs []string
		sortBy        []*pagination.SortOption
	}

	tcsCheckVisibility := []testcase{
		{
			name:          "listUserRepos - can see all of his own repos",
			userID:        suite.users.Krosh.ID,
			expectedSlugs: []string{"kroshmigrating1", "kroshmigrating2", "kroshprivate1", "kroshprivate2", "kroshpub1", "kroshpub2", "kroshpub3"},
		},
		{
			name:          "listUserRepos - public repos of another user",
			userID:        suite.users.Kopatych.ID,
			expectedSlugs: []string{"kop1", "kop2", "kop3", "kop4"},
		},
		{
			name:          "listOrgRepos - admin",
			orgID:         org1,
			expectedSlugs: []string{"org1migrating1", "org1migrating2", "a", "b", "c", "x", "y", "z"},
		},
		{
			name:          "listOrgRepos - non-member",
			orgID:         org2,
			expectedSlugs: []string{"a", "b", "c"},
		},
		{
			name:          "listOrgRepos by slug - admin",
			slug:          "corpa",
			expectedSlugs: []string{"org1migrating1", "org1migrating2", "a", "b", "c", "x", "y", "z"},
		},
		{
			name:          "listOrgRepos by slug - non-member",
			slug:          "corpb",
			expectedSlugs: []string{"a", "b", "c"},
		},
		{
			name: "discover (by last_updated)",
			sortBy: []*pagination.SortOption{
				{
					Column:    "last_updated",
					Direction: pagination.SortOption_DESC,
				},
			},
			expectedSlugs: kroshDiscoverRepos,
		},
		{
			name:   "listUserRepos (by last_updated) - public repos of another user",
			userID: suite.users.Kopatych.ID,
			sortBy: []*pagination.SortOption{
				{
					Column:    "last_updated",
					Direction: pagination.SortOption_DESC,
				},
			},
			expectedSlugs: []string{"kop4", "kop3", "kop2", "kop1"},
		},
		{
			name:  "listOrgRepos (by last_updated) - admin",
			orgID: org1,
			sortBy: []*pagination.SortOption{
				{
					Column:    "last_updated",
					Direction: pagination.SortOption_DESC,
				},
			},
			expectedSlugs: []string{"org1migrating2", "org1migrating1", "z", "y", "x", "c", "b", "a"},
		},
	}
	checkResult := func(t *testing.T, tc testcase) *pb.ListRepositoriesResponse {
		var err error
		var resp *pb.ListRepositoriesResponse

		if tc.userID != 0 {
			resp, err = client.ListUserRepositories(ctx, &pb.ListUserRepositoriesRequest{UserId: grpc_marshalling.IDInverse(tc.userID), SortBy: tc.sortBy, Query: &tc.query})
		} else if tc.orgID != 0 {
			resp, err = client.ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{OrgId: grpc_marshalling.IDInverse(tc.orgID), SortBy: tc.sortBy, Query: &tc.query})
		} else if tc.slug != "" {
			resp, err = client.ListOrgRepositoriesBySlug(ctx, &pb.ListOrgRepositoriesBySlugRequest{Slug: tc.slug, SortBy: tc.sortBy, Query: &tc.query})
		} else {
			// page size 100 guarantees listing all repos
			resp, err = client.DiscoverAllRepositories(ctx, &pb.DiscoverAllRepositoriesRequest{SortBy: tc.sortBy, PageSize: utils.PtrFromValue(uint64(100))})
		}
		require.NoError(t, err)

		actualSlugs := functools.Map(resp.Repositories, func(r *pb.Repository) string {
			return r.Slug
		})

		errMsg := fmt.Sprintf("actual repos: %s", strings.Join(actualSlugs, ", "))

		require.EqualValues(t, tc.expectedSlugs, actualSlugs, errMsg)
		return resp
	}
	for _, tc := range tcsCheckVisibility {
		t.Run(tc.name, func(t *testing.T) {
			resp := checkResult(t, tc)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))
		})

	}

	tcsCheckFilterAndOrder := []testcase{
		{
			name:          "sort affects migrating repos - by slug ASC",
			userID:        suite.users.Krosh.ID,
			expectedSlugs: []string{"kroshmigrating1", "kroshmigrating2", "kroshprivate1", "kroshprivate2", "kroshpub1", "kroshpub2", "kroshpub3"},
			sortBy:        []*pagination.SortOption{{Column: "repo_slug", Direction: pagination.SortOption_ASC}},
		},
		{
			name:          "sort affects migrating repos - by slug DESC",
			userID:        suite.users.Krosh.ID,
			expectedSlugs: []string{"kroshmigrating2", "kroshmigrating1", "kroshpub3", "kroshpub2", "kroshpub1", "kroshprivate2", "kroshprivate1"},
			sortBy:        []*pagination.SortOption{{Column: "repo_slug", Direction: pagination.SortOption_DESC}},
		},
		{
			name:          "listOrgRepos - default sort is by slug",
			orgID:         orgCheckDefaultSort,
			expectedSlugs: []string{"appsec", "sourcecraft", "sourcecraft-ui"},
		},
		{
			name:          "listOrgRepos sort by visibility - default sort is by slug",
			orgID:         orgCheckDefaultSort,
			expectedSlugs: []string{"appsec", "sourcecraft-ui", "sourcecraft"},
			sortBy:        []*pagination.SortOption{{Column: "visibility", Direction: pagination.SortOption_ASC}},
		},
		{
			name:          "listOrgRepos with query - default sort is by slug",
			orgID:         orgCheckDefaultSort,
			query:         "sourcecraft",
			expectedSlugs: []string{"sourcecraft", "sourcecraft-ui"},
		},
		{
			name:          "listOrgRepos with query",
			orgID:         kroshOrg.ID,
			query:         "migrating",
			expectedSlugs: []string{"kroshmigrating1", "kroshmigrating2"},
		},
		{
			name:          "listOrgRepos by slug with query",
			slug:          kroshOrg.Slug,
			query:         "migrating",
			expectedSlugs: []string{"kroshmigrating1", "kroshmigrating2"},
		},
		{
			name:          "listOrgRepos with query - non-member",
			orgID:         kopatychOrg.ID,
			query:         "migrating",
			expectedSlugs: []string{},
		},
		{
			name:          "listUserRepos with query",
			userID:        suite.users.Krosh.ID,
			query:         "migrating",
			expectedSlugs: []string{"kroshmigrating1", "kroshmigrating2"},
		},
	}
	for _, tc := range tcsCheckFilterAndOrder {
		t.Run(tc.name, func(t *testing.T) {
			checkResult(t, tc)
		})
	}

	// test pagination
	for pageSize := uint64(1); pageSize <= 10; pageSize++ {
		for _, tc := range append(tcsCheckVisibility, tcsCheckFilterAndOrder...) {
			t.Run(tc.name+fmt.Sprintf(", pageSize: %d", pageSize), func(t *testing.T) {
				resultSlugs := make([]string, 0)
				nextPageToken := ""

				for {
					var err error
					var resp *pb.ListRepositoriesResponse

					if tc.userID != 0 {
						resp, err = client.ListUserRepositories(ctx, &pb.ListUserRepositoriesRequest{
							UserId:    grpc_marshalling.IDInverse(tc.userID),
							SortBy:    tc.sortBy,
							PageSize:  &pageSize,
							PageToken: &nextPageToken,
							Query:     &tc.query,
						})
					} else if tc.orgID != 0 {
						resp, err = client.ListOrgRepositories(ctx, &pb.ListOrgRepositoriesRequest{
							OrgId:     grpc_marshalling.IDInverse(tc.orgID),
							SortBy:    tc.sortBy,
							PageSize:  &pageSize,
							PageToken: &nextPageToken,
							Query:     &tc.query,
						})
					} else if tc.slug != "" {
						resp, err = client.ListOrgRepositoriesBySlug(ctx, &pb.ListOrgRepositoriesBySlugRequest{
							Slug:      tc.slug,
							SortBy:    tc.sortBy,
							PageSize:  &pageSize,
							PageToken: &nextPageToken,
							Query:     &tc.query,
						})
					} else {
						resp, err = client.DiscoverAllRepositories(ctx, &pb.DiscoverAllRepositoriesRequest{
							SortBy:    tc.sortBy,
							PageSize:  &pageSize,
							PageToken: &nextPageToken,
						})
					}

					require.NoError(t, err)

					slugs := functools.Map(resp.Repositories, func(r *pb.Repository) string {
						return r.Slug
					})
					resultSlugs = append(resultSlugs, slugs...)

					nextPageToken = resp.NextPageToken
					if nextPageToken == "" {
						break
					}
				}

				require.EqualValues(t, tc.expectedSlugs, resultSlugs)
			})
		}
	}

}

func (suite *RwApiTestSuite) TestGrpcListTemplates() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	org, orgSourceCraft, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Admin, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "sourcecraft",
		Claims:     entities.OrganizationClaims{Name: "SourceCraft"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	var orgSystemTemplates []uint64

	err = suite.Params.OrgRepo.UpdateOrganizationByID(orgSourceCraft).
		SetCanCreateSystemTemplates(true).
		Commit(ctx)
	require.NoError(t, err)

	suite.populateRepos(t, []string{"go", "java", "cloud"}, entities.Visibilities.Public, orgSourceCraft, suite.users.Krosh, false, &orgSystemTemplates)
	for _, tID := range orgSystemTemplates {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.System).
			Commit(ctx)
		require.NoError(t, err)
	}

	err = suite.OrgService.AddUser(ctx, nil, *org, suite.users.Krosh.Identity)
	require.NoError(t, err)

	var orgTemplatesPrivate []uint64
	suite.populateRepos(t, []string{"o", "dev", "aysya"}, entities.Visibilities.Private, orgSourceCraft, suite.users.Krosh, false, &orgTemplatesPrivate)
	for _, tID := range orgTemplatesPrivate {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.Organization).
			Commit(ctx)
		require.NoError(t, err)
	}

	var orgTemplatesPublic []uint64
	suite.populateRepos(t, []string{"githab", "boysya", "souscrafta"}, entities.Visibilities.Public, orgSourceCraft, suite.users.Krosh, false, &orgTemplatesPublic)
	for _, tID := range orgTemplatesPublic {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.Organization).
			Commit(ctx)
		require.NoError(t, err)
	}

	ctx = testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	var yandexOrgTemplatesPublic []uint64
	suite.populateRepos(t, []string{"aaa", "bbb", "vvv"}, entities.Visibilities.Public, suite.orgs.Yandex.ID, suite.users.Admin, false, &yandexOrgTemplatesPublic)
	for _, tID := range yandexOrgTemplatesPublic {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.Organization).
			Commit(ctx)
		require.NoError(t, err)
	}

	err = suite.Params.OrgRepo.UpdateOrganizationByID(suite.orgs.Smeshariki.ID).
		SetCanCreateSystemTemplates(true).
		Commit(ctx)
	require.NoError(t, err)

	var smesharikiSystemTemplates []uint64
	suite.populateRepos(t, []string{"ccc", "ppp", "kkk"}, entities.Visibilities.Public, suite.orgs.Smeshariki.ID, suite.users.Admin, false, &smesharikiSystemTemplates)
	for _, tID := range smesharikiSystemTemplates {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.System).
			Commit(ctx)
		require.NoError(t, err)
	}

	client := pb.NewRepoServiceClient(suite.grpcClient)
	orgStringID := grpc_marshalling.IDInverse(orgSourceCraft)

	fakeOrgID := "12345"
	tcs := []struct {
		name         string
		expectedPks  []uint64
		req          *pb.ListTemplatesRequest
		ctx          context.Context
		expectedCode codes.Code
	}{
		{
			name:         "list only system templates",
			expectedPks:  append(orgSystemTemplates, smesharikiSystemTemplates...),
			req:          &pb.ListTemplatesRequest{},
			ctx:          testutils.AuthorizeGRPC(suite.users.Krosh.Identity),
			expectedCode: codes.OK,
		},
		{
			name:         "member of org can get all templates",
			expectedPks:  append(smesharikiSystemTemplates, append(orgSystemTemplates, append(orgTemplatesPublic, orgTemplatesPrivate...)...)...),
			req:          &pb.ListTemplatesRequest{OrgId: &orgStringID},
			ctx:          testutils.AuthorizeGRPC(suite.users.Krosh.Identity),
			expectedCode: codes.OK,
		},
		{

			name:         "not a member of org can get only public and system templates",
			expectedPks:  append(smesharikiSystemTemplates, append(orgSystemTemplates, orgTemplatesPublic...)...),
			req:          &pb.ListTemplatesRequest{OrgId: &orgStringID},
			ctx:          testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
			expectedCode: codes.OK,
		},
		{
			name:         "get fake org id",
			expectedPks:  append(orgSystemTemplates, smesharikiSystemTemplates...),
			req:          &pb.ListTemplatesRequest{OrgId: &fakeOrgID},
			ctx:          testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
			expectedCode: codes.OK,
		},
		{
			name:         "get org template without org id",
			expectedPks:  []uint64{},
			req:          &pb.ListTemplatesRequest{TemplateType: pb.RepoTemplate_TEMPLATE_ORGANIZATIONAL},
			ctx:          testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "get system template without org id",
			expectedPks:  append(orgSystemTemplates, smesharikiSystemTemplates...),
			req:          &pb.ListTemplatesRequest{TemplateType: pb.RepoTemplate_TEMPLATE_SYSTEM},
			ctx:          testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			expectedCode: codes.OK,
		},
		{
			name:         "get org templates in org",
			expectedPks:  append(orgTemplatesPrivate, orgTemplatesPublic...),
			req:          &pb.ListTemplatesRequest{TemplateType: pb.RepoTemplate_TEMPLATE_ORGANIZATIONAL, OrgId: &orgStringID},
			ctx:          testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			expectedCode: codes.OK,
		},
		{
			name:         "get system templates with org id",
			expectedPks:  append(orgSystemTemplates, smesharikiSystemTemplates...),
			req:          &pb.ListTemplatesRequest{TemplateType: pb.RepoTemplate_TEMPLATE_SYSTEM, OrgId: &orgStringID},
			ctx:          testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			expectedCode: codes.OK,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			var resp *pb.ListTemplatesResponse

			resp, err = client.ListTemplates(tc.ctx, tc.req)

			if tc.expectedCode != codes.OK {
				yarequire.ProtoStatusEqual(t, tc.expectedCode, err)
				return
			}

			actual, err := functools.MapWithError(resp.Repositories, func(r *pb.Repository) (uint64, error) {
				return grpc_marshalling.IDDirect(r.Id)
			})
			yarequire.ProtoStatusEqual(t, tc.expectedCode, err)
			require.ElementsMatch(t, tc.expectedPks, actual)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))
		})

	}
}

func (suite *RwApiTestSuite) TestGrpcSuggestTemplates() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	_, orgSourceCraft, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Admin, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "sourcecraft",
		Claims:     entities.OrganizationClaims{Name: "SourceCraft"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	var orgTemplates []uint64

	err = suite.Params.OrgRepo.UpdateOrganizationByID(orgSourceCraft).
		SetCanCreateSystemTemplates(true).
		Commit(ctx)
	require.NoError(t, err)

	suite.populateRepos(t, []string{"go", "java", "cloud", "goose", "gorm"}, entities.Visibilities.Public, orgSourceCraft, suite.users.Krosh, false, &orgTemplates)
	for _, tID := range orgTemplates {
		err = suite.RepoRepo.UpdateRepositoryByID(tID).
			SetTemplateType(&entities.TemplateTypes.System).
			Commit(ctx)
		require.NoError(t, err)
	}

	client := pb.NewRepoServiceClient(suite.grpcClient)

	tcs := []struct {
		name        string
		query       string
		expectedPks []uint64
	}{
		{
			name:        "suggest Templates",
			query:       "go",
			expectedPks: []uint64{orgTemplates[0], orgTemplates[3], orgTemplates[4]},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			var resp *pb.SuggestTemplateResponse

			resp, err = client.SuggestTemplate(ctx, &pb.SuggestTemplateRequest{
				Query: tc.query,
			})
			require.NoError(t, err)

			actual, err := functools.MapWithError(resp.Repositories, func(r *pb.Repository) (uint64, error) {
				return grpc_marshalling.IDDirect(r.Id)
			})
			require.NoError(t, err)
			require.ElementsMatch(t, tc.expectedPks, actual)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcList_NoUnfinishedMigrationsPanic() {
	t := suite.T()
	user := suite.users.Kopatych

	org := suite.createOrg(t, user, "yandex-abcd")

	repo1, _ := suite.createRepo(t, user, "repo-1", pb.ResourceVisibility_RESOURCE_PUBLIC, org.Id)
	repo2, _ := suite.createRepo(t, user, "repo-2-migrating", pb.ResourceVisibility_RESOURCE_PUBLIC, org.Id)
	_, _ = suite.createRepo(t, user, "repo-3-private", pb.ResourceVisibility_RESOURCE_PRIVATE, org.Id)
	repo4, _ := suite.createRepo(t, user, "repo-4-migrating-private", pb.ResourceVisibility_RESOURCE_PRIVATE, org.Id)

	repo2ID, err := grpc.ParseID(repo2.Id)
	require.NoError(t, err)
	err = suite.RepoRepo.UpdateRepositoryByID(repo2ID).
		SetMigrationID(utils.PtrFromValue("finished-2")).
		Commit(context.Background())
	require.NoError(t, err)

	repo4ID, err := grpc.ParseID(repo4.Id)
	require.NoError(t, err)
	err = suite.RepoRepo.UpdateRepositoryByID(repo4ID).
		SetMigrationID(utils.PtrFromValue("unfinished-4")).
		SetIsMigrating(true).
		Commit(context.Background())
	require.NoError(t, err)

	// anonymous
	repos, err := pb.NewRepoServiceClient(suite.grpcClient).ListOrgRepositories(context.Background(), &pb.ListOrgRepositoriesRequest{
		OrgId:    org.Id,
		PageSize: utils.PtrFromValue(uint64(10)),
	})
	require.NoError(t, err)
	require.Len(t, repos.Repositories, 2)
	require.Equal(t, repo1.Id, repos.Repositories[0].Id)
	require.Equal(t, repo2.Id, repos.Repositories[1].Id)
}

func (suite *RwApiTestSuite) TestGrpcDiscoverRepositories() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	require.NoError(t, suite.RatingRepo.Adjust(ctx, suite.repos.BranchPolicy.ID, 10))
	require.NoError(t, suite.RatingRepo.Adjust(ctx, suite.repos.Crisscross.ID, 20))
	require.NoError(t, suite.RatingRepo.Adjust(ctx, suite.repos.History.ID, 3))

	var nextPageToken string
	t.Run("page one", func(t *testing.T) {
		resp, err := client.DiscoverAllRepositories(ctx, &pb.DiscoverAllRepositoriesRequest{})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))

		require.NotEmpty(t, resp.GetNextPageToken())
		nextPageToken = resp.GetNextPageToken()
	})

	t.Run("page two", func(t *testing.T) {
		resp, err := client.DiscoverAllRepositories(ctx, &pb.DiscoverAllRepositoriesRequest{PageToken: &nextPageToken})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "logo", "last_updated"))
	})
}
