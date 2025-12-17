package integrationtests

import (
	commongrpc "common/grpc"
	"common/grpc/middleware"
	"common/logging"
	"context"
	"gitcore/internal/config"
	"gitcore/internal/grpcserver"
	"gitcore/internal/httpserver"
	"gitcore/internal/interfaces"
	"gitcore/internal/migrations/migrator"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"google.golang.org/grpc"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type RepoApiTestSuite struct { //nolint:revive
	IntegrationTestSuite
}

func (suite *RepoApiTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer()
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(Create)
	suite.SetupOrganizations(Create)
	suite.SetupSmallRepos(Create)
	suite.SetupTemporalAttempts(1)
	// TODO: dump fixtures for faster load times; right now moved SetupLargeRepos to separate suite LargeRepoApiTestSuite
	// suite.SetupLargeRepos()
}

func (suite *RepoApiTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

type DevEnvTestSuite struct {
	IntegrationTestSuite
}

func (suite *DevEnvTestSuite) SetupSuite() {
	suite.SetupServerWithConfigOpts(config.UnittestAppConfigOptions{
		EnvironmentType: config.Development,
	})
	suite.SetupUsers(0)
}

func (suite *DevEnvTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllCronjobs()
	suite.CancelAllWorkflows()
}

type LargeRepoApiTestSuite struct { //nolint:revive
	IntegrationTestSuite
}

func (suite *LargeRepoApiTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer()
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(0)
	suite.SetupOrganizations(0)
	suite.SetupSmallRepos(Create)
	suite.SetupLargeRepos()
	suite.SetupTemporalAttempts(1)
}

func (suite *LargeRepoApiTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

// тесты на API методы которые не должны вызывать загрузку паков
type NonPackApiTestSuite struct { //nolint:revive
	IntegrationTestSuite
}

func (suite *NonPackApiTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer()
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(0)
	suite.orgs.Yandex = suite.OrganizationFixture(0, "yandex", suite.users.Admin, nil)
	suite.SetupSmallRepos(Create)
	suite.PackCacheDiag.ResetTracking()
	suite.SetupTemporalAttempts(1)
}

func (suite *NonPackApiTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

func (suite *NonPackApiTestSuite) BeforeTest(_, _ string) {
	suite.PackCacheDiag.ResetTracking()
}

func (suite *NonPackApiTestSuite) AfterTest(_, _ string) {
	// На данный момент нам сложно гарантировать такое, т.к. операции которые делаются через temporal
	// после загрузки репы - грузят пакфайлы
	// require.False(suite.T(), suite.PackCache.HasDownloaded(), "this API operation must work without downloading")
}

type additionalFxDeps struct {
	fx.In
	HouseKeepers []interfaces.HousekeepingMethod `group:"housekeeping"`
}

// тесты которые меняют состояние репозитория, после каждого теста происходит резет БД и хранилища
// checkpoint создается в /tmp после первого запуска
type RwApiTestSuite struct { //nolint:revive
	IntegrationTestSuite

	hasCheckpoint bool
	fixtureLoaded bool
	alwaysReset   bool

	additionalFxDeps
}

func (suite *RwApiTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer(fx.Populate(&suite.additionalFxDeps))
	suite.SetupTemporalAttempts(1)

	hasDb, err := suite.DbKit.TemplateExists()
	require.NoError(suite.T(), err)

	hasS3, err := suite.S3Kit.CheckpointExists(backupName)
	require.NoError(suite.T(), err)

	hasOpensearch, err := suite.OpensearchKit.CheckpointExists(context.Background(), backupName)
	require.NoError(suite.T(), err)

	suite.hasCheckpoint = hasDb && hasS3 && hasOpensearch // set to false if you want to tinker with fixtures
	suite.alwaysReset = os.Getenv("NO_FIXTURE_DUMP") != ""
}

const backupName = "integration_tests"

// setupFixtures can either
// - Create all models procedurally
// - Or just load from db models and assign them to suite
func (suite *RwApiTestSuite) setupFixtures(op LoadOp) {
	suite.ResetCounters()
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(op)
	suite.SetupOrganizations(op)
	suite.SetupSmallRepos(op)
	suite.SetupAchievements(op)

	suite.SetupAuthFixtures(op)
	//suite.SetupBugRepoductionRepo(op)
}

func (suite *RwApiTestSuite) saveCheckpoint() {
	err := suite.DbKit.RecordTemplate()
	require.NoError(suite.T(), err, "backup failed")
	err = suite.OpensearchKit.RecreateIndices(context.Background())
	require.NoError(suite.T(), err, "backup failed")

	suite.setupFixtures(Create)

	err = suite.DbKit.CreateDBFromTemplate(context.Background())
	require.NoError(suite.T(), err, "create db failed")

	err = suite.S3Kit.SaveCheckpoint(context.Background(), backupName)
	require.NoError(suite.T(), err, "backup failed")
	err = suite.OpensearchKit.SaveCheckpoint(context.Background(), backupName)
	require.NoError(suite.T(), err, "backup failed")

	suite.hasCheckpoint = true
	suite.fixtureLoaded = true
}

func (suite *RwApiTestSuite) CleanupState() {
	suite.CancelAllCronjobs()
	suite.CancelAllWorkflows()
	require.NoError(suite.T(), suite.DbKit.DropDB(context.Background()))
}

func (suite *RwApiTestSuite) RollbackState() {
	t := suite.T()

	if !suite.hasCheckpoint || suite.alwaysReset {
		logging.Info(nil, "No fixtures found, loading manually")
		suite.saveCheckpoint()
		return
	}

	tic := time.Now()
	err := suite.DbKit.CreateDBFromTemplate(context.Background())
	require.NoError(t, err, "backup failed")
	toc0 := time.Now()
	if !suite.fixtureLoaded {
		// пока что я не восстанавливаю состояние до чекпойнта каждый раз,
		// только загружаю если его не было до прогона тестов
		err = suite.S3Kit.Restore(context.Background(), backupName)
		require.NoError(t, err, "backup failed")

		err = suite.OpensearchKit.Restore(context.Background(), backupName)
		require.NoError(t, err, "backup failed")
	}

	toc1 := time.Now()

	suite.fixtureLoaded = true
	suite.setupFixtures(LoadOnly) // load fixtures to suite.users.*, ....
	toc2 := time.Now()

	logging.Info(nil, "Fixture restore took %v (%v db; %v s3; %v loading)", toc2.Sub(tic), toc0.Sub(tic), toc1.Sub(toc0), toc2.Sub(toc1))
}

func (suite *RwApiTestSuite) BeforeTest(_, _ string) {
	suite.RollbackState()
	require.NoError(suite.T(), suite.ClearNotifyMessages())
	require.NoError(suite.T(), suite.ClearWebSocketRequests())

	suite.RestartAllCronjobs()
	suite.OpensearchBackendProxy.MakeAvaliable()
	suite.StubClaimService.ResetClaims()
}

func (suite *RwApiTestSuite) AfterTest(_, _ string) {
	suite.CleanupState()
}

func (suite *RwApiTestSuite) SetupSubTest() {
	suite.RollbackState()
	suite.RestartAllCronjobs()
}

func (suite *RwApiTestSuite) TearDownSubTest() {
	suite.CleanupState()
}

func (suite *RwApiTestSuite) RestoreOpensearch() {
	err := suite.OpensearchKit.Restore(context.Background(), backupName)
	require.NoError(suite.T(), err)
}

type MigrationTestSuite struct { //nolint:revive
	IntegrationTestSuite
	Migrator migrator.RecorderMigrator
}

func (suite *MigrationTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer(
		fx.Provide(migrator.NewMigrator), // as-is
		fx.Decorate(
			fx.Annotate(
				migrator.NewRecorderMigrator,
				fx.As(new(interfaces.Migrator)),
				fx.As(new(migrator.RecorderMigrator)),
			),
		),
		fx.Populate(&suite.Migrator),
	)
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(0)
	suite.SetupOrganizations(0)
	suite.SetupTemporalAttempts(1)
}

func (suite *MigrationTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllCronjobs()
	suite.CancelAllWorkflows()
}

type RoutesTestSuite struct {
	grpc.ServiceRegistrar
	IntegrationTestSuite
	server   *grpc.Server
	services []interfaces.GrpcService
	descs    []*grpc.ServiceDesc

	httpServers []*echo.Echo
}

func (suite *RoutesTestSuite) SetupSuite() {
	suite.SetupServer(
		fx.Decorate(
			fx.Annotate(
				func(params grpcserver.Params) *grpc.Server {
					for _, service := range params.Services {
						service.RegisterService(suite)
					}
					suite.server = grpcserver.NewGrpcServer(params)
					return suite.server
				},
			)),
		fx.Decorate(
			fx.Annotate(
				middleware.NewEmptyMiddleware,
				fx.As(new(commongrpc.ServerMiddleware)),
				fx.ResultTags(`name:"valium"`),
			),
		),
		fx.Invoke(
			suite.getAllHTTPServers,
		),
	)

	require.Equal(suite.T(), len(suite.services), len(suite.descs))
}

func (suite *RoutesTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

func (suite *RoutesTestSuite) RegisterService(desc *grpc.ServiceDesc, impl any) {
	service, ok := impl.(interfaces.GrpcService)
	require.True(suite.T(), ok)
	suite.services = append(suite.services, service)
	suite.descs = append(suite.descs, desc)
}

func (suite *RoutesTestSuite) getAllHTTPServers(
	api *httpserver.APIHTTPServer,
	int *httpserver.InternalGitHTTPServer,
	ext *httpserver.ExternalGitHTTPServer,
) {
	suite.httpServers = append(suite.httpServers, api.Server)
	suite.httpServers = append(suite.httpServers, int.Server)
	suite.httpServers = append(suite.httpServers, ext.Server)
}

func TestRepoApiTestSuite(t *testing.T) {
	suite.Run(t, new(RepoApiTestSuite))
}

func TestDevEnvTestSuite(t *testing.T) {
	suite.Run(t, new(DevEnvTestSuite))
}

func TestLargeRepoApiTestSuite(t *testing.T) {
	suite.Run(t, new(LargeRepoApiTestSuite))
}

func TestNonPackApiTestSuite(t *testing.T) {
	suite.Run(t, new(NonPackApiTestSuite))
}

func TestRoutesTestSuite(t *testing.T) {
	suite.Run(t, new(RoutesTestSuite))
}

func TestRwApiTestSuite(t *testing.T) {
	suite.Run(t, new(RwApiTestSuite))
}

func TestMigrationTestSuite(t *testing.T) {
	suite.Run(t, new(MigrationTestSuite))
}
