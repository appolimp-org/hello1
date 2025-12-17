package integrationtests

import (
	"common/services/quota"
	"context"
	"gitcore/internal/entities"
	quota_service "gitcore/internal/services/quota"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestQuota() {
	t := suite.T()
	quotaOrg := suite.ExternalOrganizationFixture(Create, "quotaorg", "yc.quota-org.yandex")
	orgID := quotaOrg.Identity.ID

	t.Run("get", func(t *testing.T) {
		ctx := context.Background()
		const customCreateRepoQuota = math.MaxInt64
		cancel := suite.setQuotaLimit(t, quotaOrg.ID, entities.Quotas.RepositoriesPrivateCount, customCreateRepoQuota)
		defer cancel()

		quotasInitial, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)

		// Create one repo
		suite.ImportRepo(quotaOrg, "listtree", "generated/listtree", nil)

		quotas, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)

		require.EqualValues(t, sumQuotaLimits(quotasInitial, []quota.QuotaLimit{
			{
				ID:    string(entities.Quotas.ObjectStoragePrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ObjectStoragePrivateSize]),
				Usage: 823,
			},
			{
				ID:    string(entities.Quotas.RepositoriesPrivateCount),
				Limit: customCreateRepoQuota,
				Usage: 1,
			},
		}), quotas)
	})

	t.Run("get with empty override limit", func(t *testing.T) {
		ctx := context.Background()

		suite.resetQuotaLimit(t, quotaOrg.ID, entities.Quotas.RepositoriesPrivateCount)

		quotasInitial, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)

		// Create one repo
		suite.ImportRepo(quotaOrg, "listbranches", "generated/listbranches", nil)

		quotas, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)

		require.EqualValues(t, sumQuotaLimits(quotasInitial, []quota.QuotaLimit{
			{
				ID:    string(entities.Quotas.ObjectStoragePrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ObjectStoragePrivateSize]),
				Usage: 548,
			},
			{
				ID:    string(entities.Quotas.RepositoriesPrivateCount),
				Limit: 20, // default
				Usage: 1,
			},
		}), quotas)
	})

	t.Run("get defaults", func(t *testing.T) {
		ctx := context.Background()
		quotas, err := suite.QuotaService.GetDefaults(ctx)
		require.NoError(t, err)

		require.EqualValues(t, []quota.QuotaLimit{
			{
				ID:    string(entities.Quotas.RepositoriesCount),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.RepositoriesCount]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.ObjectStorageSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ObjectStorageSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.PackFileSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.PackFileSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.LFSStorageSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.LFSStorageSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.PackFilePrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.PackFilePrivateSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.ObjectStoragePrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ObjectStoragePrivateSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.LFSStoragePrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.LFSStoragePrivateSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.RepositoriesPrivateCount),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.RepositoriesPrivateCount]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.ReleaseAssetsSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ReleaseAssetsSize]),
				Usage: 0,
			},
			{
				ID:    string(entities.Quotas.ReleaseAssetsPrivateSize),
				Limit: float64(quota_service.UnittestDefaults[entities.Quotas.ReleaseAssetsPrivateSize]),
				Usage: 0,
			},
		}, quotas)
	})

	t.Run("update", func(t *testing.T) {
		ctx := context.Background()
		const customCreateRepoQuota = 15

		cancel := suite.setQuotaLimit(t, quotaOrg.ID, entities.Quotas.RepositoriesCount, customCreateRepoQuota)
		defer cancel()

		quotasInitial, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)

		// update limit
		updatedCreateRepoQuota := float64(25)
		updateOp, err := suite.QuotaService.Update(ctx, orgID, quota.QuotaUpdateReq{
			ID:    string(entities.Quotas.RepositoriesCount),
			Limit: updatedCreateRepoQuota,
		})
		require.NoError(t, err)
		require.Equal(t, updateOp.Description, "update quota")

		// get update quota
		quotas, err := suite.QuotaService.Get(ctx, orgID)
		require.NoError(t, err)
		require.EqualValues(t, sumQuotaLimits(quotasInitial, []quota.QuotaLimit{
			{
				ID:    string(entities.Quotas.RepositoriesCount),
				Limit: updatedCreateRepoQuota,
				Usage: 0,
			},
		}), quotas)
	})
}

func sumQuotaLimits(a, b []quota.QuotaLimit) []quota.QuotaLimit {
	quotaMap := make(map[string]quota.QuotaLimit)
	for _, q := range a {
		quotaMap[q.ID] = q
	}

	for _, q := range b {
		if existing, ok := quotaMap[q.ID]; ok {
			quotaMap[q.ID] = quota.QuotaLimit{
				ID:    q.ID,
				Limit: q.Limit,
				Usage: q.Usage + existing.Usage,
			}
		} else {
			quotaMap[q.ID] = q
		}
	}

	result := make([]quota.QuotaLimit, 0, len(quotaMap))
	for _, q := range quotaMap {
		result = append(result, q)
	}

	slices.SortFunc(result, func(a, b quota.QuotaLimit) int {
		return strings.Compare(a.ID, b.ID)
	})

	return result
}
