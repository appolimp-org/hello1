package integrationtests

import (
	commongrpc "common/grpc/exceptions"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	commonlocks "gitcore/internal/locks/common"

	"testing"

	cockroacherr "github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestSlugServiceFindAvailableSlug() {
	t := suite.T()

	ctx := context.Background()

	tests := []struct {
		name         string
		slug         string
		expectedSlug string
		wantNew      bool
	}{
		{
			name:         "available",
			slug:         "123456-9872137982189-213987",
			expectedSlug: "123456-9872137982189-213987",
		},
		{
			name:         "org",
			slug:         suite.orgs.Smeshariki.Slug,
			expectedSlug: suite.orgs.Smeshariki.Slug + "-1",
		},
		{
			name:         "user",
			slug:         suite.users.Kopatych.Username,
			expectedSlug: suite.users.Kopatych.Username + "-1",
		},
		{
			name:         "repo slug is in different namespace",
			slug:         suite.repos.Alpha.Slug,
			expectedSlug: suite.repos.Alpha.Slug,
		},
		{
			name:         "normalization",
			slug:         "котик пушистый",
			expectedSlug: "kotik-pushistyi",
		},

		{
			name:         "blacklisted",
			slug:         "create",
			expectedSlug: "*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			slug, unlock, err := suite.SlugService.FindNamespaceAvailableSlug(ctx, test.slug)
			require.NoError(t, err)

			defer unlock()
			if test.expectedSlug == "*" {
				require.NotEqual(t, test.slug, slug)
			} else {
				require.Equal(t, test.expectedSlug, slug)
			}

		})
	}
}

func (suite *RwApiTestSuite) TestSlugServiceFindAvailableSlug_Locking() {
	s := suite.orgs.Smeshariki.Slug
	for _, slugName := range []string{s + "-1", s + "-2", s + "-3"} {
		unlock, err := commonlocks.Slug.TryLock(context.Background(), suite.Params.LockService, slugName)
		require.NoError(suite.T(), err)

		//goland:noinspection GoDeferInLoop
		defer unlock() // not a leak
	}

	slug, unlock, err := suite.SlugService.FindNamespaceAvailableSlug(context.Background(), suite.orgs.Smeshariki.Slug)
	require.NoError(suite.T(), err)

	defer unlock()
	require.Equal(suite.T(), s+"-4", slug)
}

func (suite *RwApiTestSuite) TestSlugServiceSlugLock_Locking() {
	s := "super-nice-slug"
	unlock, err := commonlocks.Slug.TryLock(context.Background(), suite.Params.LockService, s)
	require.NoError(suite.T(), err)
	defer unlock()

	_, err = suite.SlugService.LockNamespaceSlug(context.Background(), nil, s)
	require.ErrorIs(suite.T(), err, except.SlugAlreadyOccupied)
}

func (suite *RwApiTestSuite) TestSlugServiceLockSlug() {
	t := suite.T()

	ctx := context.Background()

	tests := []struct {
		name    string
		slug    string
		wantErr error
	}{
		{
			name: "available",
			slug: "123456-9872137982189-213987",
		},
		{
			name:    "org",
			slug:    suite.orgs.Smeshariki.Slug,
			wantErr: except.SlugAlreadyOccupied,
		},
		{
			name:    "user",
			slug:    suite.users.Kopatych.Username,
			wantErr: except.SlugAlreadyOccupied,
		},
		{
			name: "repo slug is in different namespace",
			slug: suite.repos.Alpha.Slug,
		},
		{
			name:    "normalization",
			slug:    "котик пушистый",
			wantErr: except.SlugNotNormalized,
		},
		{
			name:    "blacklisted",
			slug:    "create",
			wantErr: except.SlugIsNotAvailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unlock, err := suite.SlugService.LockNamespaceSlug(ctx, nil, test.slug)
			if unlock != nil {
				defer unlock()
			}
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestSlugServiceFindAvailableLabelSlug() {
	t := suite.T()

	ctx := context.Background()

	repoID1, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	repoID2, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// Create existing labels
	suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID1,
		Name:   "Bug",
		Slug:   utils.PtrFromValue("bug"),
	})
	suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID2,
		Name:   "Feature",
		Slug:   utils.PtrFromValue("feature"),
	})

	tests := []struct {
		name         string
		slug         string
		repoID       uint64
		expectedSlug string
	}{
		{
			name:         "available",
			slug:         "123456-9872137982189-213987",
			repoID:       repoID1,
			expectedSlug: "123456-9872137982189-213987",
		},
		{
			name:         "existing label in repo",
			slug:         "bug",
			repoID:       repoID1,
			expectedSlug: "bug-1",
		},
		{
			name:         "label in different repo",
			slug:         "bug",
			repoID:       repoID2,
			expectedSlug: "bug",
		},
		{
			name:         "normalization",
			slug:         "котик пушистый",
			repoID:       repoID1,
			expectedSlug: "kotik-pushistyi",
		},
		{
			name:         "invalid slug",
			slug:         "!@#$%^&*()",
			repoID:       repoID1,
			expectedSlug: "*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			slug, unlock, err := suite.SlugService.FindLabelAvailableSlug(ctx, test.repoID, test.slug)
			require.NoError(t, err)

			defer unlock()
			if test.expectedSlug == "*" {
				require.NotEqual(t, test.slug, slug)
			} else {
				require.Equal(t, test.expectedSlug, slug)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestSlugServiceFindAvailableLabelSlug_Locking() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	baseSlug := "bug"

	// Create existing labels
	suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Slug:   utils.PtrFromValue(baseSlug),
	})

	for _, slugName := range []string{baseSlug + "-1", baseSlug + "-2", baseSlug + "-3"} {
		unlock, err := commonlocks.LabelSlug.TryLock(context.Background(), suite.Params.LockService, entities.LabelSlugIdentity{
			RepoID: repoID,
			Slug:   slugName,
		})
		require.NoError(t, err)

		defer unlock()
	}

	slug, unlock, err := suite.SlugService.FindLabelAvailableSlug(context.Background(), repoID, baseSlug)
	require.NoError(t, err)

	defer unlock()
	require.Equal(t, baseSlug+"-4", slug)
}

func (suite *RwApiTestSuite) TestSlugServiceLabelSlugLock_Locking() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	slug := "super-nice-slug"

	unlock, err := commonlocks.LabelSlug.TryLock(context.Background(), suite.Params.LockService, entities.LabelSlugIdentity{
		RepoID: repoID,
		Slug:   slug,
	})
	require.NoError(t, err)
	defer unlock()

	_, err = suite.SlugService.LockLabelSlug(context.Background(), repoID, slug)
	require.ErrorIs(t, err, except.SlugAlreadyOccupied)
}

func (suite *RwApiTestSuite) TestSlugServiceLockLabelSlug() {
	t := suite.T()
	ctx := context.Background()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// Create existing label
	suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Slug:   utils.PtrFromValue("bug"),
	})

	tests := []struct {
		name    string
		slug    string
		repoID  uint64
		wantErr error
	}{
		{
			name:   "available",
			slug:   "123456-9872137982189-213987",
			repoID: repoID,
		},
		{
			name:    "existing label in repo",
			slug:    "bug",
			repoID:  repoID,
			wantErr: except.SlugAlreadyOccupied,
		},
		{
			name:    "normalization",
			slug:    "котик пушистый",
			repoID:  repoID,
			wantErr: except.SlugNotNormalized,
		},
		{
			name:    "invalid slug",
			slug:    "!@#$%^&*()",
			repoID:  repoID,
			wantErr: except.SlugNotNormalized,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unlock, err := suite.SlugService.LockLabelSlug(ctx, test.repoID, test.slug)
			if unlock != nil {
				defer unlock()
			}
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestSlugServiceLabelSlugDifferentRepos() {
	t := suite.T()

	repoID1, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	repoID2, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// Check that the same slug can be used in different repos
	unlock1, err := suite.SlugService.LockLabelSlug(context.Background(), repoID1, "bug")
	require.NoError(t, err)
	defer unlock1()

	unlock2, err := suite.SlugService.LockLabelSlug(context.Background(), repoID2, "bug")
	require.NoError(t, err)
	defer unlock2()

	// Try to lock same slug in same repo
	_, err = suite.SlugService.LockLabelSlug(context.Background(), repoID2, "bug")
	require.ErrorIs(t, err, except.SlugAlreadyOccupied)
}

func (suite *RwApiTestSuite) TestResolveSlug() {
	t := suite.T()
	ctx := context.Background()

	require.NotNil(t, suite.users.Kopatych.PersonalOrgID)

	johnSnow86, err := suite.UserRepo.CreateUser(ctx, entities.User{
		Username: "johnsnow89",
		Identity: entities.UserIdentity{
			ID:  "johnsnow89",
			Src: entities.IdentityProviders.IAM,
		},
		Email:      "johnsnow89@mail.net",
		Visibility: entities.Visibilities.Private,
		Status:     entities.UserStatuses.Active,
	})

	require.NoError(t, err)

	kopatychPersOrg, err := suite.OrgRepo.GetOrganizationByID(ctx, *suite.users.Kopatych.PersonalOrgID)

	require.NoError(t, err)

	tests := []struct {
		name     string
		slug     string
		org      *entities.Organization
		user     *entities.User
		wantCode codes.Code
	}{
		{
			name:     "not found",
			slug:     "123456-9872137982189-213987",
			wantCode: codes.NotFound,
		},
		{
			name: "org",
			slug: suite.orgs.Smeshariki.Slug,
			org:  suite.orgs.Smeshariki,
		},
		{
			name: "user",
			slug: johnSnow86.Username,
			user: johnSnow86,
		},
		{
			name: "user with personal org",
			slug: suite.users.Kopatych.Username,
			user: suite.users.Kopatych,
			org:  kopatychPersOrg,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, org, err := suite.SlugService.ResolveNamespaceSlug(ctx, test.slug)

			if test.wantCode == codes.OK {
				require.NoError(t, err)
			} else {
				err = cockroacherr.Cause(err)
				exception, ok := err.(*commongrpc.Exception)
				require.True(t, ok)
				require.Equal(t, test.wantCode, exception.Code)
				return
			}

			if test.user != nil {
				require.NotNil(t, user)
				require.Equal(t, test.user.ID, user.ID)
			} else {
				require.Nil(t, user)
			}

			if test.org != nil {
				require.NotNil(t, org)
				require.Equal(t, test.org.ID, org.ID)
			} else {
				require.Nil(t, org)
			}
		})
	}
}

func (suite *RepoApiTestSuite) TestSlugServiceCheckRepoSlugAvailability() {
	t := suite.T()

	ctx := context.Background()

	tests := []struct {
		name      string
		orgID     uint64
		slug      string
		wantCode  codes.Code
		avaliable bool
	}{
		{
			name:      "avaliable",
			orgID:     suite.repos.Alpha.OrgID,
			slug:      "123456-9872137982189-213987",
			avaliable: true,
		}, {
			name:  "blacklisted",
			orgID: suite.repos.Alpha.OrgID,
			slug:  "overview",
		}, {
			name:  "occupied",
			orgID: suite.repos.Alpha.OrgID,
			slug:  suite.repos.Alpha.Slug,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok, err := suite.SlugService.CheckRepoSlugAvailability(ctx, test.orgID, test.slug)

			if test.wantCode == codes.OK {
				require.NoError(t, err)
				require.Equal(t, test.avaliable, ok)
			} else {
				err = cockroacherr.Cause(err)
				exception, ok := err.(*commongrpc.Exception)
				require.True(t, ok)
				require.Equal(t, test.wantCode, exception.Code)
				return
			}
		})
	}
}
