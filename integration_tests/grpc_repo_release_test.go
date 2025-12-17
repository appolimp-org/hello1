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
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcReleaseBase() {
	t := suite.T()
	releaseClient := pb.NewReleaseServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)

	componentCreateOpts := &pb.CreateComponentRequest{
		RepoId:    grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		Name:      "frontend",
		TagPrefix: "release-",
	}
	component, err := releaseClient.CreateComponent(ctx, componentCreateOpts)
	require.NoError(t, err)

	var preparedRelease *pb.PrepareReleaseResponse

	revB := entities.NewGitRevisionFromRev(suite.repos.Gitflow.ID, utils.PtrFromValue("master"))
	revA := entities.NewGitRevisionFromRev(suite.repos.Gitflow.ID, utils.PtrFromValue("tag:v1"))

	t.Run("changelog - all", func(t *testing.T) {
		changelog, err := suite.Params.InternalReleaseService.BuildChangelog(ctx,
			testutils.NewStubAuthenticator(&suite.users.Kopatych.Identity),
			nil, revB, &revA,
			interfaces.ReleaseChangelogAll)

		require.NoError(t, err)
		expected := []string{"squashed merge feature B", "Direct commit to master", "merge-commit feature A"}
		actual := strings.Split(changelog, "\n")

		require.Equal(t, len(expected), len(actual), "Changelog: expected (parts) %v, actual %v", expected, actual)

		for i := 0; i < len(expected); i++ {
			require.Contains(t, actual[i], expected[i])
		}
	})

	t.Run("prepare release", func(t *testing.T) {
		resp, err := releaseClient.Prepare(ctx, &pb.PrepareReleaseRequest{
			ComponentId:      component.Component.Id,
			ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_ALL,
			Version:          "0.0.1",
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.PreparedRelease{}, "hash"),
		)

		preparedRelease = resp
	})

	t.Run("list releases - empty", func(t *testing.T) {
		list, err := releaseClient.ListByComponent(ctx, &pb.ListReleasesByComponentRequest{
			ComponentId: component.Component.Id,
		})
		require.NoError(t, err)
		require.Len(t, list.Releases, 0)
	})

	var release *pb.Release

	t.Run("create release", func(t *testing.T) {
		resp, err := releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})
		require.NoError(t, err)

		require.Equal(t, component.Component.Id, resp.Release.GetCompId())
		require.Equal(t, preparedRelease.Release.Hash, resp.Release.Hash)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)

		// check tag
		refRepo := suite.RefRepoFactory.Build(suite.repos.Alpha.ID)
		ref, err := refRepo.GetTag(ctx, preparedRelease.Release.Tag)
		require.NoError(t, err)
		require.NotNil(t, ref)
		require.Equal(t, ref.Hash().String(), preparedRelease.Release.Hash)

		release = resp.Release
	})

	var preparedReleaseWithoutComponent *pb.PrepareReleaseResponse
	t.Run("prepare release without component", func(t *testing.T) {
		resp, err := releaseClient.Prepare(ctx, &pb.PrepareReleaseRequest{
			RepoId:           utils.PtrFromValue(grpc_marshalling.IDInverse(suite.repos.Alpha.ID)),
			ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_ALL,
			Version:          "0.0.1",
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.PreparedRelease{}, "hash"),
		)

		preparedReleaseWithoutComponent = resp
	})

	t.Run("create release without component", func(t *testing.T) {
		resp, err := releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			RepoId:  utils.PtrFromValue(grpc_marshalling.IDInverse(suite.repos.Alpha.ID)),
			Release: preparedReleaseWithoutComponent.Release,
		})
		require.NoError(t, err)

		require.Nil(t, resp.Release.CompId)
		require.Equal(t, preparedRelease.Release.Hash, resp.Release.Hash)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)

		// check tag
		refRepo := suite.RefRepoFactory.Build(suite.repos.Alpha.ID)
		ref, err := refRepo.GetTag(ctx, preparedRelease.Release.Tag)
		require.NoError(t, err)
		require.NotNil(t, ref)
		require.Equal(t, ref.Hash().String(), preparedRelease.Release.Hash)
	})

	t.Run("update release", func(t *testing.T) {
		resp, err := releaseClient.Update(ctx, &pb.UpdateReleaseRequest{
			Id:          release.Id,
			Changelog:   "new changelog",
			Description: "new description",
			Status:      pb.Release_STATUS_DRAFT,
		})
		require.NoError(t, err)

		updatedRelease, err := grpc_marshalling.OperationResponse(resp, &pb.Release{})
		require.NoError(t, err)
		require.NotNil(t, updatedRelease.CompId)
		require.Equal(t, component.Component.Id, *updatedRelease.CompId)
		require.Equal(t, preparedRelease.Release.Hash, updatedRelease.Hash)

		//yarequire.ProtoDumpFixture(t, updatedRelease)
		yarequire.ProtoCompareWithFixture(t, updatedRelease,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)

		release = updatedRelease
	})

	t.Run("list releases by component", func(t *testing.T) {
		resp, err := releaseClient.ListByComponent(ctx, &pb.ListReleasesByComponentRequest{
			ComponentId: component.Component.Id,
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)
	})

	t.Run("list releases by repo", func(t *testing.T) {
		resp, err := releaseClient.ListByRepo(ctx, &pb.ListReleasesByRepoRequest{
			RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)
	})

	t.Run("duplicate prepare", func(t *testing.T) {
		_, err := releaseClient.Prepare(ctx, &pb.PrepareReleaseRequest{
			ComponentId:      component.Component.Id,
			ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_ALL,
			Version:          "0.0.1",
		})

		yarequire.ProtoStatusEqual(t, codes.AlreadyExists, err)
	})

	t.Run("duplicate create", func(t *testing.T) {
		_, err := releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})

		yarequire.ProtoStatusEqual(t, codes.AlreadyExists, err)
	})

	t.Run("get release", func(t *testing.T) {
		resp, err := releaseClient.Get(ctx, &pb.GetReleaseRequest{
			Id: release.Id,
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Release{}, "id", "comp_id", "hash", "created_at", "updated_at", "released_at"),
		)
	})

	t.Run("get not existing release", func(t *testing.T) {
		_, err := releaseClient.Get(ctx, &pb.GetReleaseRequest{
			Id: grpc_marshalling.IDInverse(99999999),
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("delete release", func(t *testing.T) {
		_, err := releaseClient.Delete(ctx, &pb.DeleteReleaseRequest{
			Id: release.Id,
		})
		require.NoError(t, err)

		// check tag presence
		refRepo := suite.RefRepoFactory.Build(suite.repos.Alpha.ID)
		ref, err := refRepo.GetTag(ctx, release.Tag)
		require.NoError(t, err)
		require.NotNil(t, ref)
		require.Equal(t, ref.Hash().String(), release.Hash)

		// list and get
		resp, err := releaseClient.ListByRepo(ctx, &pb.ListReleasesByRepoRequest{
			RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		})
		require.NoError(t, err)
		require.Len(t, resp.Releases, 1)

		_, err = releaseClient.Get(ctx, &pb.GetReleaseRequest{
			Id: release.Id,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("create release again", func(t *testing.T) {
		resp, err := releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})
		require.NoError(t, err)

		require.Equal(t, release.GetCompId(), resp.Release.GetCompId())
		require.Equal(t, preparedRelease.Release.Hash, resp.Release.Hash)
		require.Equal(t, preparedRelease.Release.Tag, resp.Release.Tag)
		require.Equal(t, preparedRelease.Release.Changelog, resp.Release.Changelog)
	})

	t.Run("bump release", func(t *testing.T) {
		req := &pb.PrepareReleaseRequest{
			ComponentId:      component.Component.Id,
			ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_ALL,
			BumpLevel:        pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_PATCH,
		}
		suite.addCommits(t,
			suite.HTTPSProtocol().RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug),
			suite.users.Admin,
			commitsOptions{id: "bump"})
		preparedRelease, err = releaseClient.Prepare(ctx, req)
		require.NoError(t, err)
		require.Equal(t, preparedRelease.PreviousVersion, "0.0.1")

		resp, err := releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})
		require.Equal(t, resp.Release.Version, "0.0.2")

		require.NoError(t, err)

		req.BumpLevel = pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MINOR
		suite.addCommits(t,
			suite.HTTPSProtocol().RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug),
			suite.users.Admin,
			commitsOptions{id: "bump2"})
		preparedRelease, err = releaseClient.Prepare(ctx, req)
		require.NoError(t, err)
		require.Equal(t, preparedRelease.Release.Version, "0.1.0")

		resp, err = releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})
		require.Equal(t, resp.Release.Version, "0.1.0")

		require.NoError(t, err)

		req.BumpLevel = pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MAJOR
		suite.addCommits(t,
			suite.HTTPSProtocol().RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug),
			suite.users.Admin,
			commitsOptions{id: "bump3"})
		preparedRelease, err = releaseClient.Prepare(ctx, req)
		require.NoError(t, err)

		resp, err = releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})

		require.Equal(t, resp.Release.Version, "1.0.0")

		require.NoError(t, err)

		req.BumpLevel = pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MAJOR
		suite.addCommits(t,
			suite.HTTPSProtocol().RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug),
			suite.users.Admin,
			commitsOptions{id: "bump4"},
		)
		preparedRelease, err = releaseClient.Prepare(ctx, req)
		require.NoError(t, err)

		resp, err = releaseClient.Create(ctx, &pb.CreateReleaseRequest{
			ComponentId: component.Component.Id,
			Release:     preparedRelease.Release,
		})

		require.Equal(t, resp.Release.Version, "2.0.0")

		require.NoError(t, err)
	})

	t.Run("bump first", func(t *testing.T) {
		componentCreateOpts := &pb.CreateComponentRequest{
			RepoId:    grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Name:      "backend",
			TagPrefix: "backend-",
		}
		component, err := releaseClient.CreateComponent(ctx, componentCreateOpts)
		require.NoError(t, err)

		req := &pb.PrepareReleaseRequest{
			ComponentId:      component.Component.Id,
			ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_ALL,
			BumpLevel:        pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_PATCH,
		}
		_, err = releaseClient.Prepare(ctx, req)
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})
}

func (suite *RwApiTestSuite) createComponent(ctx context.Context, repoID uint64, name string, path string, client pb.ReleaseServiceClient) *pb.Component {
	t := suite.T()

	component, err := client.CreateComponent(ctx, &pb.CreateComponentRequest{
		RepoId:    grpc_marshalling.IDInverse(repoID),
		Name:      name,
		TagPrefix: fmt.Sprintf("tag-%s-", name),
		Path:      path,
	})

	require.NoError(t, err)

	return component.Component
}

// deprecated
func (suite *RwApiTestSuite) createRelease(ctx context.Context, componentID string, hash string, version string, client pb.ReleaseServiceClient) *pb.Release {
	t := suite.T()

	release, err := client.Create(ctx, &pb.CreateReleaseRequest{
		Release: &pb.PreparedRelease{
			Version: version,
			Tag:     fmt.Sprintf("%s-%s", componentID, version),
			Hash:    hash,
		},
		ComponentId: componentID,
	})

	require.NoError(t, err)

	return release.Release
}

func (suite *RwApiTestSuite) TestGrpcReleaseFromTag() {
	t := suite.T()
	componentClient := pb.NewReleaseServiceClient(suite.grpcClient)
	releaseClient := pb.NewReleaseServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	suite.addRole(t, suite.users.Krosh, suite.repos.ListTags, iam.Roles.RepositoriesMaintainer)

	componentCreateOpts := &pb.CreateComponentRequest{
		RepoId:    grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
		Name:      "app",
		TagPrefix: "release-",
	}
	component, err := componentClient.CreateComponent(ctx, componentCreateOpts)
	require.NoError(t, err)

	_, err = releaseClient.Create(ctx, &pb.CreateReleaseRequest{
		ComponentId: component.Component.Id,
		Release: &pb.PreparedRelease{
			Version:          "2.0.0",
			Tag:              "v2.main",
			Changelog:        "change log",
			ReuseExistingTag: true,
		},
	})
	require.NoError(t, err)

	list, err := releaseClient.ListByComponent(ctx, &pb.ListReleasesByComponentRequest{
		ComponentId: component.Component.Id,
	})
	require.NoError(t, err)
	require.Len(t, list.Releases, 1)
	require.Equal(t, "78350efe20fc33f940c2894e7c74e15963d7356a", list.Releases[0].Hash)
	require.Equal(t, "2.0.0", list.Releases[0].Version)
}

func (suite *RwApiTestSuite) TestGrpcReleasePrepare() {
	repo := suite.repos.Alpha
	dirRepo := suite.repos.Dir
	user := suite.users.Krosh

	t := suite.T()

	releaseClient := pb.NewReleaseServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	suite.addRole(t, user, repo, iam.Roles.RepositoriesMaintainer)
	suite.addRole(t, user, dirRepo, iam.Roles.RepositoriesMaintainer)

	frontComponent := suite.createComponent(ctx, repo.ID, "frontend", "", releaseClient)
	backComponent := suite.createComponent(ctx, repo.ID, "backend", "", releaseClient)
	ciComponent := suite.createComponent(ctx, repo.ID, "ci", "", releaseClient)
	IdeComponent := suite.createComponent(ctx, repo.ID, "ide", "", releaseClient)

	noDirComponent := suite.createComponent(ctx, dirRepo.ID, "no-dir", "", releaseClient)
	dirComponent := suite.createComponent(ctx, dirRepo.ID, "dir", "dir", releaseClient)

	suite.createRelease(ctx, backComponent.Id, "b029517f6300c2da0f4b651b8642506cd6aaf45d", "0.0.1-rc", releaseClient)
	suite.createRelease(ctx, backComponent.Id, "b8e471f58bcbca63b07bda20e428190409c2db47", "0.0.1", releaseClient)

	suite.createRelease(ctx, ciComponent.Id, "b029517f6300c2da0f4b651b8642506cd6aaf45d", "0.0.1", releaseClient)

	suite.createRelease(ctx, IdeComponent.Id, "b029517f6300c2da0f4b651b8642506cd6aaf45d", "0.0.1-rc", releaseClient)

	tests := []struct {
		Name        string
		Req         *pb.PrepareReleaseRequest
		WantErrCode codes.Code
	}{
		{
			Name: "initial release",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: frontComponent.Id,
				Revision:    "b029517f6300c2da0f4b651b8642506cd6aaf45d",
				Version:     "0.0.1",
			},
		},
		{
			Name: "bump_patch",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: backComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_PATCH,
			},
		}, {
			Name: "bump_minor",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: backComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MINOR,
			},
		},
		{
			Name: "bump_major",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: backComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MAJOR,
			},
		}, {
			Name: "bump_no_prev_release",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: frontComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_PATCH,
			},
			WantErrCode: codes.FailedPrecondition,
		},
		{
			Name: "bump_prerelease_version",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: IdeComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_PATCH,
			},
		},
		{
			Name: "bump_prerelease_version_minor",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: IdeComponent.Id,
				Revision:    "35e85108805c84807bc66a02d91535e1e24b38b9",
				BumpLevel:   pb.ReleaseBumpLevel_RELEASE_BUMP_LEVEL_MINOR,
			},
		},
		{
			Name: "use_head",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: frontComponent.Id,
				Version:     "0.0.1",
			},
		},
		{
			Name: "no_changelog",
			Req: &pb.PrepareReleaseRequest{
				ComponentId:      frontComponent.Id,
				Version:          "0.0.1",
				ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_NONE,
			},
		},
		{
			Name: "merge_only",
			Req: &pb.PrepareReleaseRequest{
				ComponentId:      frontComponent.Id,
				Version:          "0.0.1",
				ChangelogCommits: pb.ReleaseChangelogCommits_RELEASE_CHANGELOG_MERGE,
			},
		},
		{
			Name: "no_dir",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: noDirComponent.Id,
				Version:     "0.0.1",
			},
		},
		{
			Name: "dir",
			Req: &pb.PrepareReleaseRequest{
				ComponentId: dirComponent.Id,
				Version:     "0.0.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			resp, err := releaseClient.Prepare(ctx, tt.Req)
			if tt.WantErrCode == codes.OK {
				require.NoError(t, err)
			} else {
				yarequire.ProtoStatusEqual(t, tt.WantErrCode, err)
				return
			}

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
