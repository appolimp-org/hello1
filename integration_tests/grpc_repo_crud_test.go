package integrationtests

import (
	"common/cgit"
	"common/functools"
	"common/grpc/exceptions"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/adapters/opensearch/mappings"
	"gitcore/internal/consts"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/services/quota"
	"gitcore/internal/testutils"
	yautils "gitcore/internal/utils"
	"gitcore/pkg/pagination"
	"os"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	syncer_pb "private_api/generated/yandex/cloud/priv/ide/v1/gitsyncer"
	"slices"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) checkDefaultBranch(repoModel *entities.Repository, branchName string, debug bool) {
	t := suite.T()
	tmpDir := testutils.TempDir(t, "", "")
	repoURL := suite.URL(repoModel)
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
	if debug {
		cg.TracePacket = true
		err := cg.ExecNoCapture("clone", repoURL, ".")
		require.NoError(t, err)
	} else {
		cg.Must(t, "clone", repoURL, ".")
	}

	stdout, _, err := cg.Exec("symbolic-ref", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "refs/heads/"+branchName, strings.Trim(stdout, "\n"))
}

func (suite *RwApiTestSuite) makeRepo(user *entities.User, opts *interfaces.CreateRepositoryArgs) *entities.Repository {
	t := suite.T()

	if opts.Visibility == "" {
		opts.Visibility = entities.Visibilities.Public
	}
	opts.CreatedBy = user.ID

	repo, err := suite.RepoService.Create(context.Background(), opts, user)
	require.NoError(t, err)

	return repo
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoDefaultBranchTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	for _, tc := range []struct {
		name          string
		repoName      string
		defaultBranch string
		expected      string
		expectedError *exceptions.ExceptionTemplate
	}{
		{
			name:          "happy path",
			repoName:      "repo1",
			defaultBranch: "trunk",
			expected:      "trunk",
		},
		{
			name:          "default value",
			repoName:      "repo2",
			defaultBranch: "",
			expected:      "main",
		},
		{
			name:          "bad ref name",
			repoName:      "repo3",
			defaultBranch: "../../etc/passwd",
			expectedError: except.InvalidBranchName,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.Create(ctx, &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
				},
				Slug:          tc.repoName,
				Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
				DefaultBranch: tc.defaultBranch,
				ProvisionOptions: &pb.ProvisionOptions{
					AddReadme: true,
				},
			})
			if tc.expectedError != nil {
				yarequire.ProtoExceptionTemplate(t, err, tc.expectedError)
				return
			}
			require.NoError(t, err)

			repo, err := grpc_marshalling.OperationResponse(res, &pb.Repository{})
			require.NoError(t, err)

			require.Equal(t, tc.expected, repo.DefaultBranch)

			res2, err := client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: repo.Id}})
			require.NoError(t, err)
			require.Equal(t, tc.expected, res2.DefaultBranch)

			id, err := grpc_marshalling.IDDirect(repo.Id)
			require.NoError(t, err)

			repoModel, err := suite.RepoRepo.GetRepositoryByID(ctx, id)
			require.NoError(t, err)

			suite.checkDefaultBranch(repoModel, tc.expected, false)

		})
	}

}

func (suite *RwApiTestSuite) TestGrpcCreateRepoDefaultBranchEmptyRepoTest() {

	suite.T().Skip("unskip with git v2 -- required unborn HEAD feature")

	/*
		"clone: respect remote unborn HEAD", 2021-02-05) introduces
		a new feature (if the remote has an unborn HEAD, e.g. when the remote
		repository is empty, use it as the name of the branch) that only works
		in protocol v2,
	*/

	t := suite.T()

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	res, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
		},
		Slug:          "repo1",
		Description:   "",
		Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
		DefaultBranch: "trunk",
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(res, &pb.Repository{})
	require.NoError(t, err)

	id, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)

	repoModel, err := suite.RepoRepo.GetRepositoryByID(ctx, id)
	require.NoError(t, err)

	tmpDir := testutils.TempDir(t, "", "basic")
	repoURL := suite.URL(repoModel)
	cgit := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
	cgit.TracePacket = true
	err = cgit.ExecNoCapture("clone", repoURL, ".")
	require.NoError(t, err)

	stdout, _, err := cgit.Exec("symbolic-ref", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "refs/heads/trunk", stdout)
}

func (suite *RwApiTestSuite) TestGrpcForkWithDifferentVisibilities() {
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	t := suite.T()

	type visib struct {
		pb.ProfileVisibility
		pb.ResourceVisibility
	}
	type sourceTarget struct {
		source visib
		target visib
	}
	tests := map[sourceTarget]error{
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
		}: nil,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
		}: nil,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PRIVATE,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
		}: except.InvalidArgument,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_INTERNAL,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
		}: except.InvalidArgument,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PRIVATE,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
		}: except.InvalidArgument,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PRIVATE,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
		}: except.InvalidArgument,
		sourceTarget{
			source: visib{
				pb.ProfileVisibility_PROFILE_PUBLIC,
				pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			target: visib{
				pb.ProfileVisibility_PROFILE_PRIVATE,
				pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
		}: except.InvalidArgument,
	}

	vis := []pb.ProfileVisibility{
		pb.ProfileVisibility_PROFILE_PRIVATE,
		pb.ProfileVisibility_PROFILE_PUBLIC,
	}
	rvis := []pb.ResourceVisibility{
		pb.ResourceVisibility_RESOURCE_PUBLIC,
		pb.ResourceVisibility_RESOURCE_INTERNAL,
		pb.ResourceVisibility_RESOURCE_PRIVATE,
	}

	repos := map[visib]*pb.Repository{}
	orgs := map[pb.ProfileVisibility]*pb.OrgProfile{}

	func() {
		client := pb.NewOrgServiceClient(suite.grpcClient)

		for _, v := range vis {
			s, err := yautils.NormalizeSlug(v.String())
			require.NoError(t, err)

			org, err := client.CreateOrg(ctx,
				&pb.CreateOrgRequest{
					Slug:        s,
					Description: s,
					DisplayName: s,
					Visibility:  v,
				})
			require.NoError(t, err)

			profile := &pb.OrgProfile{}
			require.NoError(t, org.GetResponse().UnmarshalTo(profile))
			orgs[v] = profile

			for _, rv := range rvis {
				client := pb.NewRepoServiceClient(suite.grpcClient)
				rs, err := yautils.NormalizeSlug(rv.String())
				require.NoError(t, err)

				resp, err := client.Create(ctx, &pb.CreateRepositoryRequest{
					Org:         &pb.CreateRepositoryRequest_OrgSlug{OrgSlug: s},
					Slug:        rs,
					Description: rs,
					Visibility:  rv,
				})
				require.NoError(t, err)

				pbr := &pb.Repository{}
				_, err = grpc_marshalling.OperationResponse(resp, pbr)
				require.NoError(t, err)

				repos[visib{v, rv}] = pbr
			}
		}
	}()

	for test, tres := range tests {
		desc := fmt.Sprintf("%s_%s -> %s_%s",
			test.source.ProfileVisibility, test.source.ResourceVisibility,
			test.target.ProfileVisibility, test.target.ResourceVisibility)

		t.Run(desc, func(t *testing.T) {
			parent := repos[test.source]

			fslug, err := yautils.NormalizeSlug(desc)
			require.NoError(t, err)

			client := pb.NewRepoServiceClient(suite.grpcClient)

			prevForks, err := client.ListForks(ctx, &pb.ListForksRequest{
				Id: parent.Id,
			})
			require.NoError(t, err)

			_, err = client.Fork(ctx, &pb.ForkRepositoryRequest{
				Org: &pb.ForkRepositoryRequest_OrgId{
					OrgId: orgs[test.target.ProfileVisibility].Id,
				},
				Slug:         fslug,
				Description:  "",
				ForkOriginId: parent.Id,
			})

			forks, err2 := client.ListForks(ctx, &pb.ListForksRequest{
				Id: parent.Id,
			})
			require.NoError(t, err2)

			if tres == nil {
				require.NoError(t, err)

				require.Equal(t, len(prevForks.Repositories)+1, len(forks.Repositories), "new fork should be created")

			} else {
				var templ *exceptions.ExceptionTemplate
				errors.As(tres, &templ)
				yarequire.ProtoStatusEqual(t, templ.Code, err)

				require.Equal(t, len(prevForks.Repositories), len(forks.Repositories), "no new fork should be created")
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcForkRepoTest() {
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	defautBranch := "main"

	resp, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
		},
		Slug:          "chrome",
		Description:   "",
		DefaultBranch: defautBranch,
		Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)

	forkOriginRepoID := repo.Id

	for i := 1; i <= int(suite.cfg.Forks.MaxForkChainLen)+1; i++ {
		resp, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:         fmt.Sprintf("chrome-fork-level-%d", i),
			Description:  "",
			ForkOriginId: forkOriginRepoID,
		})
		if uint32(i) >= suite.cfg.Forks.MaxForkChainLen {
			yarequire.ProtoStatusEqual(t, except.ForkChainIsTooLong.Code, err)
			continue
		}
		require.NoError(t, err)

		repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
		require.NoError(t, err)
		require.Equal(t, forkOriginRepoID, *repo.ForkOriginId)

		fid, err := grpc_marshalling.IDDirect(repo.Id)
		require.NoError(t, err)

		chain, err := suite.RepoRepo.GetForkOriginsChain(ctx, fid)
		require.NoError(t, err)
		require.Len(t, chain, 1+i)

		foid, err := grpc_marshalling.IDDirect(forkOriginRepoID)
		require.NoError(t, err)

		forks, err := suite.RepoRepo.ListForks(ctx, foid, pagination.Options{}, nil)
		require.NoError(t, err)
		require.Len(t, forks.Result, 1)
		require.Equal(t, fid, forks.Result[0].ID)

		forkOriginRepoID = repo.Id
	}
}

func (suite *RwApiTestSuite) TestGrpcQuotaRepoTest() {
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	orgID := suite.orgs.Yandex.ID

	// quota before
	usage1 := currentStorageUsage(t, suite, orgID)

	defaultBranch := "main"
	resp, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(orgID),
		},
		Slug:          "chrome",
		Description:   "",
		DefaultBranch: defaultBranch,
		Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
		ProvisionOptions: &pb.ProvisionOptions{
			AddReadme: true,
		},
	})
	require.NoError(t, err)
	repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)
	repoID := repo.Id

	usage2 := currentStorageUsage(t, suite, orgID)
	require.Greater(t, usage2, usage1)

	_, err = client.Delete(ctx, &pb.DeleteRepositoryRequest{Id: repoID})
	require.NoError(t, err)

	usage3 := currentStorageUsage(t, suite, orgID)
	require.Equal(t, usage1, usage3)
}

func (suite *RwApiTestSuite) TestGrpcForkQuotaRepoTest() {
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	orgID := suite.orgs.Yandex.ID

	// quota before
	currentUsage := currentStorageUsage(t, suite, orgID)

	// create origin repo
	defaultBranch := "main"
	resp, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(orgID),
		},
		Slug:          "chrome",
		Description:   "",
		DefaultBranch: defaultBranch,
		Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
		ProvisionOptions: &pb.ProvisionOptions{
			AddReadme: true,
		},
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)
	forkOriginRepoID := repo.Id

	// check usage per repo
	usage := currentStorageUsage(t, suite, orgID)
	repoUsage := usage - currentUsage
	currentUsage = usage

	for i := 1; i <= 4; i++ {
		t.Run(fmt.Sprintf("fork %d", i), func(t *testing.T) {
			_, err = client.Fork(ctx, &pb.ForkRepositoryRequest{
				Org: &pb.ForkRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(orgID),
				},
				Slug:         fmt.Sprintf("chrome-fork-level-%d", i),
				Description:  "",
				ForkOriginId: forkOriginRepoID,
			})
			require.NoError(t, err)
			forkOriginRepoID = repo.Id

			usage = currentStorageUsage(t, suite, orgID)
			require.Equal(t, repoUsage, usage-currentUsage)
			currentUsage = usage
		})
	}
}

func currentStorageUsage(t *testing.T, suite *RwApiTestSuite, orgID uint64) uint64 {
	ctx := context.Background()

	totalObjectUsage, err := suite.quotaCalculator.ObjectsSize(ctx, orgID, entities.Visibilities.Public)
	require.NoError(t, err)

	objectUsageQuota, err := suite.quotaService.Get(ctx, orgID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	require.EqualValues(t, totalObjectUsage, objectUsageQuota.Usage)

	return uint64(objectUsageQuota.Usage)
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	projYandex42, err := suite.MakeProject(suite.orgs.Yandex42.Slug, "badproj", entities.Visibilities.Public)
	require.NoError(t, err)

	err = suite.AccessBindingsService.CreateBindings(context.Background(), access.NullAuthenticator, []*entities.AccessBinding{{
		Subject: suite.users.Admin.Subject(),
		Object: entities.IAMObject{
			ID:   projYandex42,
			Type: entities.ObjectTypes.Project,
		},
		Role: iam.Roles.Admin,
	}})
	require.NoError(t, err)

	projYandex, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	projYandex42Str := grpc_marshalling.IDInverse(projYandex42)
	projYandexStr := grpc_marshalling.IDInverse(projYandex)

	tt := map[string]struct {
		request         *pb.CreateRepositoryRequest
		user            *entities.User
		verify          func(*testing.T, *entities.Repository)
		verifyException func(*testing.T, error)
		expectedStatus  codes.Code
	}{
		"happy path": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo",
				ProjectId:   &projYandexStr,
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			verify: func(t *testing.T, repository *entities.Repository) {
				require.Equal(t, "foo", repository.Slug)
				require.Equal(t, entities.Visibilities.Private, repository.Visibility)
				require.Equal(t, suite.orgs.Yandex.ID, repository.OrgID)

				// check owner
				abs, err := suite.AccessBindingsService.GetAllObjectBindings(context.Background(), access.NullAuthenticator, []entities.IAMObject{
					repository.Object(),
				}, pagination.Options{})
				require.NoError(t, err)
				require.Equal(t, 1, len(abs.Result))
				require.Equal(t, suite.users.Admin.Subject(), abs.Result[0].Subject)
				require.Equal(t, iam.Roles.RepositoriesAdmin, abs.Result[0].Role)
			},
		},
		"sourcecraft": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug: ".sourcecraft",
			},
			verify: func(t *testing.T, repository *entities.Repository) {
				require.Equal(t, ".sourcecraft", repository.Slug)
			},
		},
		"access denied": {
			user: suite.users.Krosh,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo2",
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			expectedStatus: codes.PermissionDenied,
		},
		"slug invalid": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "repos",
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			expectedStatus: codes.FailedPrecondition,
			verifyException: func(t *testing.T, err error) {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, st.Message(), "slug \"repos\" is not available, please pick another one")
			},
		},
		"slug conflict": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        suite.repos.Alpha.Name,
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			expectedStatus: codes.FailedPrecondition,
			verifyException: func(t *testing.T, err error) {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, st.Message(), "slug \"alpha\" is not available, please pick another one")
			},
		},
		"project-repo mismatch": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        suite.repos.Alpha.Name,
				ProjectId:   &projYandex42Str,
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			expectedStatus: except.ProjectOrgMismatch.Code,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.Create(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				if tc.verifyException != nil {
					tc.verifyException(t, err)
				}
				return
			}

			repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
			require.NoError(t, err)

			if tc.verify != nil {
				pk, err := grpc_marshalling.IDDirect(repo.Id)
				require.NoError(t, err)
				repo, err := suite.RepoRepo.GetRepositoryByID(ctx, pk)
				require.NoError(t, err)
				tc.verify(t, repo)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoWithDefaultQuota() {
	t := suite.T()
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()
	defaultCreateRepoQuota := int(quota.UnittestDefaults[entities.Quotas.RepositoriesCount])
	defaultCreateRepoPrivateQuota := int(quota.UnittestDefaults[entities.Quotas.RepositoriesPrivateCount])
	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("CreateRepoWithDefaultQuota", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		for i := 0; i < defaultCreateRepoQuota; i++ {
			request := &pb.CreateRepositoryRequest{
				Org:  &pb.CreateRepositoryRequest_OrgId{OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID)},
				Slug: suite.mustGenUniqueSlug(), Description: "",
				Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			}

			response, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
			require.NoError(t, err)
		}

		for i := 0; i < defaultCreateRepoPrivateQuota; i++ {
			request := &pb.CreateRepositoryRequest{
				Org:  &pb.CreateRepositoryRequest_OrgId{OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID)},
				Slug: suite.mustGenUniqueSlug(), Description: "",
				Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
			}

			response, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
			require.NoError(t, err)
		}

		request := &pb.CreateRepositoryRequest{
			Org:  &pb.CreateRepositoryRequest_OrgId{OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID)},
			Slug: suite.mustGenUniqueSlug(), Description: "",
			Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		}

		response, err := client.Create(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.ResourceExhausted, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)

		request = &pb.CreateRepositoryRequest{
			Org:  &pb.CreateRepositoryRequest_OrgId{OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID)},
			Slug: suite.mustGenUniqueSlug(), Description: "",
			Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		}

		response, err = client.Create(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.ResourceExhausted, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoFork() {
	t := suite.T()
	const customCreateRepoQuota = 10
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()
	cancel := suite.setQuotaLimit(t, suite.orgs.Yango.ID, entities.Quotas.RepositoriesCount, customCreateRepoQuota)
	defer cancel()

	client := pb.NewRepoServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	request := &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
		},
		Slug:        "origin-for-fork-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		Description: "",
		Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
	}

	response, err := client.Create(ctx, request)
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	createdRepo, err := grpc_marshalling.OperationResponse(response, &pb.Repository{})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	repoID, err := grpc_marshalling.IDDirect(createdRepo.Id)
	require.NoError(t, err)

	// fill profile
	links := []*entities.Link{
		{
			Type: entities.LinkTypes.Default,
			Link: "https://ya.ru",
		},
	}
	_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
		Id:         grpc_marshalling.IDInverse(repoID),
		Links:      grpc_marshalling.Entity.ToPrivateAPI.Links(links),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"links"}},
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	// Fill with data
	tmpDir := t.TempDir()
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Admin.Identity))
	cg.Must(t, "clone", testutils.GetDataPath("basic.git"), ".")

	prodSlug := suite.mustGenUniqueSlug()
	cg.Must(t, "remote", "add", prodSlug, suite.HTTPSProtocol().RepoURL(createdRepo.OrgSlug, createdRepo.Slug))
	cg.Must(t, "push", prodSlug, "--all")

	parentRepoID := createdRepo.Id

	t.Run("HappyPath", func(t *testing.T) {
		pinfo, err := suite.MetaDataRepoFactory.Build(repoID).GetTotalPacksInfo(ctx, []uint64{repoID})
		require.NoError(t, err)

		forkOp, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			ForkOriginId:      parentRepoID,
			Slug:              suite.mustGenUniqueSlug(),
			DefaultBranchOnly: true,
			Description:       "",
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		forkRepo, err := grpc_marshalling.OperationResponse(forkOp, &pb.Repository{})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		forkID, err := grpc_marshalling.IDDirect(forkRepo.Id)
		require.NoError(t, err)

		pinfo2, err := suite.MetaDataRepoFactory.Build(repoID).GetTotalPacksInfo(ctx, []uint64{forkID, repoID})
		require.NoError(t, err)
		require.Equal(t, pinfo.TotalBlobsSize, pinfo2.TotalBlobsSize)
		require.Equal(t, pinfo.TotalTextsSize, pinfo2.TotalTextsSize)
		require.Equal(t, pinfo.TotalPackfilesSize, pinfo2.TotalPackfilesSize)
		require.Equal(t, pinfo.TotalOwnPackfilesSize, pinfo2.TotalOwnPackfilesSize)

		pinfo3, err := suite.MetaDataRepoFactory.Build(forkID).GetTotalPacksInfo(ctx, []uint64{forkID, repoID})
		require.NoError(t, err)
		require.Equal(t, pinfo.TotalPackfilesSize, pinfo3.TotalPackfilesSize)
		require.Zero(t, pinfo3.TotalOwnPackfilesSize)

		origStats, err := suite.RepoStatsRepo.Get(ctx, repoID)
		require.NoError(t, err)
		origRepoFlavors := entities.ForEachFlavor(origStats.RepoFlavors, func(s string) string {
			return s
		})
		slices.Sort(origRepoFlavors)
		forkStats, err := suite.RepoStatsRepo.Get(ctx, forkID)
		require.NoError(t, err)
		forkRepoFlavors := entities.ForEachFlavor(forkStats.RepoFlavors, func(s string) string {
			return s
		})
		slices.Sort(forkRepoFlavors)
		require.Equal(t, origRepoFlavors, forkRepoFlavors)
		require.Equal(t, origStats.Flavors, forkStats.Flavors)

		require.NotNil(t, forkRepo.ForkOriginId)
		require.Equal(t, parentRepoID, *forkRepo.ForkOriginId)

		// yarequire.ProtoDumpFixture(t, forkRepo)
		yarequire.ProtoCompareWithFixture(t, forkRepo,
			protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "fork_origin_id", "slug", "clone_url", "last_updated"),
		)

		t.Run("ForksChain", func(t *testing.T) {
			forksChain, err := client.ForkChainInfo(ctx, &pb.ForkChainRequest{
				RepoId: forkRepo.Id,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.Len(t, forksChain.ForkParents, 1)

			parent := forksChain.ForkParents[0]

			// yarequire.ProtoDumpFixture(t, parent)
			yarequire.ProtoCompareWithFixture(t, parent,
				protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "fork_origin_id", "slug", "clone_url", "last_updated"),
			)
		})

		t.Run("Branches", func(t *testing.T) {
			t.Skip("Branches are not copied yet")

			branches, err := client.ListBranches(ctx, &pb.ListBranchesRequest{
				Id: forkRepo.Id,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.Len(t, branches.Branches, 1)
		})

		t.Run("Clone", func(t *testing.T) {
			// clone and check repo content
			tmpDir := testutils.TempDir(t, "", "basic")

			cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Admin.Identity))
			cgAdmin.Must(t, "clone", suite.HTTPSProtocol().RepoURL(forkRepo.OrgSlug, forkRepo.Slug))
		})
	})

	t.Run("CloneAllBranches", func(t *testing.T) {
		forkOp, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			ForkOriginId:      parentRepoID,
			Slug:              suite.mustGenUniqueSlug(),
			DefaultBranchOnly: false,
			Description:       "",
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		forkRepo, err := grpc_marshalling.OperationResponse(forkOp, &pb.Repository{})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		forkID, err := grpc_marshalling.IDDirect(forkRepo.Id)
		require.NoError(t, err)
		_ = forkID

		require.NotNil(t, forkRepo.ForkOriginId)
		require.Equal(t, parentRepoID, *forkRepo.ForkOriginId)

		// yarequire.ProtoDumpFixture(t, forkRepo)
		yarequire.ProtoCompareWithFixture(t, forkRepo,
			protocmp.IgnoreFields(&pb.Repository{}, "id", "uuid", "fork_origin_id", "slug", "clone_url", "last_updated"),
		)
	})

	t.Run("InvalidSlug", func(t *testing.T) {
		_, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			ForkOriginId: parentRepoID,
			Slug:         "repos",
			Description:  "",
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("SlugClash", func(t *testing.T) {
		_, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			ForkOriginId: parentRepoID,
			Slug:         request.Slug,
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("ForkOriginIsNotFound", func(t *testing.T) {
		_, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			Slug: suite.mustGenUniqueSlug(),
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})

	t.Run("ForkOriginIsNotFound2", func(t *testing.T) {
		_, err := client.Fork(ctx, &pb.ForkRepositoryRequest{
			Org: &pb.ForkRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			ForkOriginId: "234242421123",
			Slug:         suite.mustGenUniqueSlug(),
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoWithCustomQuota() {
	t := suite.T()
	const customCreateRepoQuota = 10
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()
	cancel := suite.setQuotaLimit(t, suite.orgs.Yango.ID, entities.Quotas.RepositoriesCount, customCreateRepoQuota)
	defer cancel()

	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("CreateRepoWithCustomQuota", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		for i := 0; i < customCreateRepoQuota; i++ {
			request := &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
				},
				Slug:        suite.mustGenUniqueSlug(),
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			}

			response, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
			require.NoError(t, err)
		}

		request := &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			Slug:        suite.mustGenUniqueSlug(),
			Description: "",
			Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
		}

		response, err := client.Create(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.ResourceExhausted, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoWithEmptyCustomQuota() {
	t := suite.T()
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()
	suite.resetQuotaLimit(t, suite.orgs.Yango.ID, entities.Quotas.RepositoriesCount)

	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("CreateRepoWithEmptyCustomQuota", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		defaultQuota := int(quota.UnittestDefaults[entities.Quotas.RepositoriesCount])
		for i := 0; i < defaultQuota; i++ {
			request := &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
				},
				Slug:        suite.mustGenUniqueSlug(),
				Description: "",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			}

			response, err := client.Create(ctx, request)
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
			require.NoError(t, err)
		}

		request := &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			Slug:        suite.mustGenUniqueSlug(),
			Description: "",
			Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
		}

		response, err := client.Create(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.ResourceExhausted, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoFromTemplate() {
	t := suite.T()

	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("unsuccesfull try to make repo from not a template", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		_, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			Slug:       "pi",
			Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			TemplatingOptions: &pb.TemplatingOptions{
				TemplateId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
		})

		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	err := suite.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
		SetTemplateType(&entities.TemplateTypes.Organization).
		Commit(context.Background())
	require.NoError(t, err)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("creating repo from template", func(t *testing.T) {

		request := &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yango.ID),
			},
			Slug:        suite.mustGenUniqueSlug(),
			Description: "",
			Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			TemplatingOptions: &pb.TemplatingOptions{
				TemplateId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
		}

		response, err := client.Create(ctx, request)
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)
	})
	org, err := suite.OrgService.GetOrganizationByID(ctx, nil, suite.repos.Alpha.OrgID)
	require.NoError(t, err)

	t.Run("create repo from template by member of org", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		suite.OrgService.AddUser(ctx, nil, org.Identity, suite.users.Kopatych.Identity)

		response, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(org.ID),
			},
			Slug:       "pi",
			Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			TemplatingOptions: &pb.TemplatingOptions{
				TemplateId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
		})

		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = grpc_marshalling.OperationResponse(response, &pb.Repository{})
		require.NoError(t, err)
	})

	suite.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
		SetTemplateType(&entities.TemplateTypes.System).
		Commit(ctx)

	t.Run("unsuccesfull creating repo from system template belonging to org by not a member of org", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)

		_, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(org.ID),
			},
			Slug:       "pi",
			Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			TemplatingOptions: &pb.TemplatingOptions{
				TemplateId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			},
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcGetRepoTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("happy path - by ID", func(t *testing.T) {
		repo, err := client.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_Id{Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID)},
		})
		require.NoError(t, err)
		require.Equal(t, suite.repos.Alpha.Name, repo.Name)
	})

	t.Run("happy path - by slug", func(t *testing.T) {
		repo, err := client.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_FullSlug{
				FullSlug: &pb.RepositoryFullSlug{
					OrgSlug:  "yandex",
					RepoSlug: "alpha",
				},
			},
		})
		require.NoError(t, err)
		require.Equal(t, suite.repos.Alpha.Name, repo.Name)
	})

	t.Run("happy path - by uuid", func(t *testing.T) {
		repo, err := client.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_Uuid{
				Uuid: suite.repos.Alpha.UUID.String(),
			},
		})
		require.NoError(t, err)
		require.Equal(t, suite.repos.Alpha.Name, repo.Name)
	})
}

func (suite *RwApiTestSuite) TestGrpcGetBulkRepoTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	repos := []*entities.Repository{
		suite.repos.Alpha,
		suite.repos.AuthRepoPublic,
		suite.repos.AuthRepoPrivate,
		suite.repos.TreeDiff,
	}

	t.Run("happy path", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
		response, err := client.GetBulk(ctx, &pb.GetBulkRepositoriesRequest{
			Ids: functools.Map(repos, func(repo *entities.Repository) string {
				return grpc_marshalling.IDInverse(repo.ID)
			}),
		})
		require.NoError(t, err)

		repoNames := functools.Map(response.Repos, func(repo *pb.Repository) string {
			return repo.Name
		})
		for _, repo := range repos {
			require.Contains(t, repoNames, repo.Name)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		user := suite.users.Krosh

		ctx := testutils.AuthorizeGRPC(user.Identity)
		response, err := client.GetBulk(ctx, &pb.GetBulkRepositoriesRequest{
			Ids: functools.Map(repos, func(repo *entities.Repository) string {
				return grpc_marshalling.IDInverse(repo.ID)
			}),
		})
		require.NoError(t, err)

		repoNames := functools.Map(response.Repos, func(repo *pb.Repository) string {
			return repo.Name
		})
		require.Contains(t, repoNames, suite.repos.Alpha.Name)
		require.Contains(t, repoNames, suite.repos.AuthRepoPublic.Name)
		require.Contains(t, repoNames, suite.repos.TreeDiff.Name)
		require.NotContains(t, repoNames, suite.repos.AuthRepoPrivate.Name)
	})
}

func (suite *RwApiTestSuite) TestGrpcUpdateRepoTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("happy path", func(t *testing.T) {
		links := []*pb.Link{
			{
				Link: "https://ya.ru",
				Type: pb.Link_DEFAULT,
			},
			{
				Link: "https://src.yandex.ru",
				Type: pb.Link_HOMEPAGE,
			},
		}

		resp, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:          grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Description: "bbb",
			Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			Links:       links,
			UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description", "visibility", "links"}},
		})

		require.NoError(t, err)

		repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
		require.NoError(t, err)

		require.Equal(t, "bbb", repo.Description)
		require.Equal(t, pb.ResourceVisibility_RESOURCE_PUBLIC, repo.Visibility)
		yarequire.ProtoEqualList(t, links, repo.Links)

		resp, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:          grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Description: "ccc",
			Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description"}},
		})
		require.NoError(t, err)

		repo, err = grpc_marshalling.OperationResponse(resp, &pb.Repository{})
		require.NoError(t, err)

		require.Equal(t, "ccc", repo.Description)
	})

	t.Run("change default branch - non existent branch", func(t *testing.T) {
		_, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:            grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			DefaultBranch: "something-out-of-the-world",
			UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"default_branch"}},
		})

		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("change default branch - happy path", func(t *testing.T) {
		_, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:            grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			DefaultBranch: "branch",
			UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"default_branch"}},
		})

		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})

	t.Run("update images", func(t *testing.T) {
		upload1 := suite.uploadPic(suite.users.Admin.Identity)
		upload2 := suite.uploadPic(suite.users.Admin.Identity)
		upload3 := suite.uploadPic(suite.users.Kopatych.Identity)

		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		repo, err := client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: grpc_marshalling.IDInverse(repoID)}})
		require.NoError(t, err)
		require.Nil(t, repo.Logo)
		// set logo
		op, err := client.UpdateImage(ctx, &pb.UpdateRepoImageRequest{
			Id:        grpc_marshalling.IDInverse(repoID),
			UploadKey: upload1.Key,
			Image:     pb.UpdateRepoImageRequest_LOGO,
		})
		require.NoError(t, err)
		repo = testutils.UnmarshalGrpcResult[*pb.Repository](t, op)
		require.NotNil(t, repo.Logo)
		logo1 := repo.GetLogo().GetUrl()

		// update logo
		op, err = client.UpdateImage(ctx, &pb.UpdateRepoImageRequest{
			Id:        grpc_marshalling.IDInverse(repoID),
			UploadKey: upload2.Key,
			Image:     pb.UpdateRepoImageRequest_LOGO,
		})
		require.NoError(t, err)
		repo = testutils.UnmarshalGrpcResult[*pb.Repository](t, op)

		logo2 := repo.GetLogo().GetUrl()
		require.NotEqual(t, logo1, logo2)

		// reuse upload is impossible
		_, err = client.UpdateImage(ctx, &pb.UpdateRepoImageRequest{
			Id:        grpc_marshalling.IDInverse(repoID),
			UploadKey: upload2.Key,
			Image:     pb.UpdateRepoImageRequest_LOGO,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)

		// use of another user's upload is impossible
		_, err = client.UpdateImage(ctx, &pb.UpdateRepoImageRequest{
			Id:        grpc_marshalling.IDInverse(repoID),
			UploadKey: upload3.Key,
			Image:     pb.UpdateRepoImageRequest_LOGO,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	suite.T().Run("update images attachments", func(t *testing.T) {
		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		repo, err := client.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_Id{Id: grpc_marshalling.IDInverse(repoID)},
		})
		require.NoError(t, err)
		require.Nil(t, repo.Logo)

		logo := suite.UploadAttachment(suite.users.Admin.Identity, "img.png", "repoLogo")
		_, err = client.UpdateImageAttachment(ctx, testutils.FieldMask(&pb.UpdateRepoImageAttachmentRequest{
			Id:        grpc_marshalling.IDInverse(repoID),
			AttachId:  logo.ID,
			ImageType: pb.UpdateRepoImageAttachmentRequest_IMAGE_TYPE_LOGO,
		}))
		require.NoError(t, err)

		updatedRepo, err := client.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_Id{Id: grpc_marshalling.IDInverse(repoID)},
		})
		require.NoError(t, err)
		require.NotNil(t, updatedRepo.Logo)
		require.NotEqual(t, "", updatedRepo.Logo.Url)
	})

	suite.T().Run("change slug", func(t *testing.T) {
		// clash of new slug
		_, err := client.UpdateSlug(ctx, &pb.UpdateRepoSlugRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
			Slug: suite.repos.Alpha.Slug,
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		st := status.Convert(err)
		require.Equal(t, "slug \"alpha\" is not available, please pick another one", st.Message())

		// invalid slug
		_, err = client.UpdateSlug(ctx, &pb.UpdateRepoSlugRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
			Slug: "projects",
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		st = status.Convert(err)
		require.Equal(t, "slug \"projects\" is not available, please pick another one", st.Message())

		// happy path
		resp, err := client.UpdateSlug(ctx, &pb.UpdateRepoSlugRequest{
			Id:   grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
			Slug: "newslug",
		})
		require.NoError(t, err)

		repo := testutils.UnmarshalGrpcResult[*pb.Repository](t, resp)
		require.Equal(t, "newslug", repo.Slug)

	})

	t.Run("update links - invalid url", func(t *testing.T) {
		links := []*pb.Link{
			{
				Link: "https://ya.ru",
				Type: pb.Link_DEFAULT,
			},
			{
				Link: "httpp://src.yandex.ru",
				Type: pb.Link_HOMEPAGE,
			},
		}

		_, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:         grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Links:      links,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"links"}},
		})

		yarequire.ProtoStatusInvalidArgument(t, err, "links[1]", "URI")
	})

	t.Run("update as nil template", func(t *testing.T) {

		_, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			TemplateType: pb.RepoTemplate_TEMPLATE_NOT_A_TEMPLATE,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})

	t.Run("try to update to system template", func(t *testing.T) {
		suite.Params.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
			SetTemplateType(&entities.TemplateTypes.Organization).Commit(ctx)

		kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

		_, err := client.Update(kroshCtx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			TemplateType: pb.RepoTemplate_TEMPLATE_SYSTEM,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	orgIdentity, orgID, err := suite.Params.OrgService.CreateOrganization(ctx, nil, suite.users.Admin, entities.IdentityProviders.SelfHosted, entities.Organization{
		Slug:       "sourcecraft",
		Claims:     entities.OrganizationClaims{Name: "SourceCraft"},
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	err = suite.Params.OrgRepo.UpdateOrganizationByID(orgID).
		SetCanCreateSystemTemplates(true).
		Commit(ctx)
	require.NoError(t, err)

	repoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		OrgID:      orgID,
		Name:       "a",
		Slug:       "fff",
		CreatedBy:  suite.users.Admin.ID,
		Visibility: entities.Visibilities.Internal,
	})
	require.NoError(t, err)
	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)
	org, err := suite.OrgService.GetOrganizationByID(ctx, nil, orgID)
	require.NoError(t, err)

	t.Run("member of org without org manager permissions trying to update to a system template", func(t *testing.T) {
		err = suite.OrgService.AddUser(ctx, nil, *orgIdentity, suite.users.Kopatych.Identity)
		require.NoError(t, err)
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(repoID),
			TemplateType: pb.RepoTemplate_TEMPLATE_SYSTEM,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("update to system template in org that can make system templates by user with org manager permissions", func(t *testing.T) {

		_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(repoID),
			TemplateType: pb.RepoTemplate_TEMPLATE_SYSTEM,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})

	t.Run("not a member of org try to update to a org template", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(repoID),
			TemplateType: pb.RepoTemplate_TEMPLATE_ORGANIZATIONAL,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("member of org update to a org template", func(t *testing.T) {
		err = suite.OrgService.AddUser(ctx, nil, *orgIdentity, suite.users.Kopatych.Identity)
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.Admin)
		suite.addOrgRole(t, suite.users.Kopatych, org, iam.Roles.OrganizationManagerAdmin)
		require.NoError(t, err)
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
			Id:           grpc_marshalling.IDInverse(repoID),
			TemplateType: pb.RepoTemplate_TEMPLATE_ORGANIZATIONAL,
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"template_type"}},
		})

		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcDeleteRepoTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	ctxKrosh := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	t.Run("access denied", func(t *testing.T) {
		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		_, err := client.Delete(ctxKrosh, &pb.DeleteRepositoryRequest{Id: grpc_marshalling.IDInverse(repoID)})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("happy path", func(t *testing.T) {
		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		pbRepoID := grpc_marshalling.IDInverse(repoID)

		suite.ideService.ClearSentEvents()

		resp, err := client.Delete(ctx, &pb.DeleteRepositoryRequest{Id: pbRepoID})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		meta, err := grpc_marshalling.OperationMetadata(resp, &pb.DeleteRepositoryMetadata{})
		require.NoError(t, err)
		require.Equal(t, pbRepoID, meta.Id)

		_, err = client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: pbRepoID}})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)

		_, err = client.Delete(ctx, &pb.DeleteRepositoryRequest{Id: pbRepoID})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoDeleteEvent)

		evnts := suite.ideService.SentEvents()
		require.Equal(t, 1, len(evnts))
		event := evnts[0].Event

		require.Equal(t, syncer_pb.Event_DELETE_EVENT, event.EventType)
		require.Equal(t, repoID, event.Repository.Id)
	})

	t.Run("delete children entities", func(t *testing.T) {
		suite.RestoreOpensearch()

		repoID1, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		repoID2, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

		repo1, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID1)
		require.NoError(t, err)

		repo2, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID2)
		require.NoError(t, err)

		for _, repo := range []*entities.Repository{repo1, repo2} {
			cg, tmpDir := suite.initCGit(suite.users.Admin, repo.FullSlug())
			cg.Must(t, "checkout", "-b", "master")
			commit(t, &cg, path.Join(tmpDir, repo.Slug, "file"), "content")
			cg.Must(t, "push", "--set-upstream", "origin", "master")

			cg.Must(t, "checkout", "-b", "branch")
			commit(t, &cg, path.Join(tmpDir, repo.Slug, "file2"), "content")
			cg.Must(t, "push", "--set-upstream", "origin", "branch")
		}

		issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID1,
			Title:      "issue1",
			Visibility: entities.IssueVisibilities.Public,
		})
		issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID1,
			Title:      "issue2",
			Visibility: entities.IssueVisibilities.Private,
		})
		issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID2,
			Title:      "issue3",
			Visibility: entities.IssueVisibilities.Public,
		})
		suite.makePullRequest(suite.users.Admin, &makePrOptions{
			Repo:  repo1,
			Title: "pr1",
		})
		suite.makePullRequest(suite.users.Admin, &makePrOptions{
			Repo:  repo2,
			Title: "pr2",
		})

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		require.NoError(t, suite.OpensearchKit.RefreshIndex(ctx))

		// check index contains issues and prs
		hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
		require.NoError(t, err)
		require.Equal(t, 3, len(hits))

		hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.PullRequestMappingType)
		require.NoError(t, err)
		require.Equal(t, 2, len(hits))

		_, err = client.Delete(ctx, &pb.DeleteRepositoryRequest{Id: grpc_marshalling.IDInverse(repoID1)})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.DeleteRepoDeps)

		_, err = client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: grpc_marshalling.IDInverse(repoID1)}})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)

		// check that issues in repo1 (issue1, issue2) deleted
		_, err = suite.IssueRepo.Get(context.Background(), issue1.ID)
		require.Error(t, err)
		_, err = suite.IssueRepo.Get(context.Background(), issue2.ID)
		require.Error(t, err)

		// check that issues in repo2 (issue3) not deleted
		_, err = suite.IssueRepo.Get(context.Background(), issue3.ID)
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchReindexWorkflow)
		suite.OpensearchKit.RefreshIndex(ctx)

		// check index does not contain deleted issues and prs
		hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
		require.NoError(t, err)
		require.Equal(t, 1, len(hits))

		hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.PullRequestMappingType)
		require.NoError(t, err)
		require.Equal(t, 1, len(hits))
	})

	t.Run("delete migrated repo", func(t *testing.T) {
		repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
		pbRepoID := grpc_marshalling.IDInverse(repoID)

		_, err := suite.MigratedRepoRepo.Create(ctx, &entities.MigratedRepository{
			ID:         repoID,
			URL:        "https://github.com/example/example",
			Domain:     "github.com",
			SyncedRefs: []string{"*"},
			Mirror:     false,
		})
		require.NoError(t, err)

		_, err = client.Delete(ctx, &pb.DeleteRepositoryRequest{Id: pbRepoID})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcCreateRepoTest_ProvisionOptions() {
	t := suite.T()

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

	for _, preset := range gitignorePresets {
		_, err := gitignoreClient.Create(ctx, preset)
		require.NoError(t, err)
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

	ctx = testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("incorrect gitignore preset", func(t *testing.T) {
		_, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:        "foo",
			Description: "My awesome repo",
			Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			ProvisionOptions: &pb.ProvisionOptions{
				GitignorePresets: []string{
					"non-existing-preset",
				},
				AddReadme: true,
			},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("incorrect license preset", func(t *testing.T) {
		_, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:        "foo",
			Description: "My awesome repo",
			Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			ProvisionOptions: &pb.ProvisionOptions{
				LicenseSlug: utils.PtrFromValue("non-existing-preset"),
				AddReadme:   true,
			},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("incorrect yaml template", func(t *testing.T) {
		_, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:        "foo",
			Description: "My awesome repo",
			Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			ProvisionOptions: &pb.ProvisionOptions{
				SrcYamlTemplateSlug: utils.PtrFromValue("non-existing-template"),
			},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("happy path", func(t *testing.T) {
		resp, err := client.Create(ctx, &pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug:        "foo",
			Description: "My awesome repo",
			Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			ProvisionOptions: &pb.ProvisionOptions{
				GitignorePresets: []string{
					gitignorePresets[0].Name, gitignorePresets[1].Name,
				},
				LicenseSlug:         &licensePresets[0].Slug,
				SrcYamlTemplateSlug: utils.PtrFromValue("simple-workflow"),
				AddReadme:           true,
			},
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		repoPb, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
		require.NoError(t, err)

		pk, err := grpc_marshalling.IDDirect(repoPb.Id)
		require.NoError(t, err)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, pk)
		require.NoError(t, err)

		require.Equal(t, "foo", repo.Slug)
		require.Equal(t, entities.Visibilities.Private, repo.Visibility)
		require.Equal(t, suite.orgs.Yandex.ID, repo.OrgID)

		// clone and check repo content
		tmpDir := testutils.TempDir(t, "", "basic")
		repoURL := suite.URL(repo)

		cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Admin.Identity))
		cgAdmin.Must(t, "clone", repoURL, repo.Name)

		entries, err := os.ReadDir(path.Join(tmpDir, repo.Name))
		require.NoError(t, err)
		require.Equal(t, 5, len(entries))

		expectedReadme := `# foo

My awesome repo
`
		content, err := os.ReadFile(path.Join(tmpDir, repo.Name, consts.Readme))
		require.NoError(t, err)

		require.Equal(t, expectedReadme, string(content))

		expectedGitignore := `### Go ###

# some comment
file

# another comment
file*

### Git ###

a/*
b/*
`
		content, err = os.ReadFile(path.Join(tmpDir, repo.Name, consts.Gitignore))
		require.NoError(t, err)

		require.Equal(t, expectedGitignore, string(content))

		expectedLicense := licensePresets[0].License
		content, err = os.ReadFile(path.Join(tmpDir, repo.Name, consts.License))
		require.NoError(t, err)

		require.Equal(t, expectedLicense, string(content))

		expectedSrcYaml := `
on:
  # Triggers the workflow on push or pull request events but only for the "main" branch
  pull_request:
    - workflows: [simple-workflow]
      filter:
        paths: ["main"]

  push:
    - workflows: [simple-workflow]
      filter:
        branches: ["main"]

workflows:
  simple-workflow:
    tasks:
      - name: simple-task
        cubes:
          - name: simple-cube
            script:
              - echo "It's a simple CI run for yandex/foo"
`
		content, err = os.ReadFile(path.Join(tmpDir, repo.Name, oyaml.CIPath))
		require.NoError(t, err)

		require.Equal(t, expectedSrcYaml, string(content))
	})
}

func (suite *RwApiTestSuite) TestGrpcRepoHandler_DeleteBranch() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// default branch
	_, err := client.CreateBranch(ctx, &pb.CreateBranchRequest{
		Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		Name: "default-branch",
	})
	require.NoError(t, err)
	_, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
		Id:            grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		DefaultBranch: "default-branch",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"default_branch"}},
	})
	require.NoError(t, err)

	// ordinary branch
	_, err = client.CreateBranch(ctx, &pb.CreateBranchRequest{
		Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		Name: "ordinary-branch",
	})
	require.NoError(t, err)

	t.Run("admin can't delete default branch", func(t *testing.T) {
		_, err := client.DeleteBranch(testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			&pb.DeleteBranchRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Name: "default-branch",
			},
		)
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("developer can delete ordinary branch", func(t *testing.T) {
		suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
		_, err := client.DeleteBranch(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
			&pb.DeleteBranchRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Name: "ordinary-branch",
			},
		)
		require.NoError(t, err)

		res, err := client.ListBranches(ctx, &pb.ListBranchesRequest{
			Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		})
		require.NoError(t, err)

		branches := functools.Map(res.Branches, func(branch *pb.Branch) string { return branch.Name })
		require.NotContains(t, branches, "ordinary-branch")
	})

	t.Run("unknown_branch", func(t *testing.T) {
		_, err := client.DeleteBranch(testutils.AuthorizeGRPC(suite.users.Admin.Identity),
			&pb.DeleteBranchRequest{
				Id:   grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Name: "unknown-branch",
			},
		)
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}
