package integrationtests

import (
	"bytes"
	"common/cgit"
	commongrpc "common/grpc"
	"common/logging"
	"common/pkgenerator"
	commonpg "common/postgres"
	yaredis "common/redis"
	"common/services/quota"
	"common/temporal"
	"common/temporalutils"
	commontestutils "common/testutils"
	"common/testutils/update"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	accesscommon "gitcore/internal/access/common"
	"gitcore/internal/access/stubs"
	"gitcore/internal/app"
	echo_auth "gitcore/internal/auth/echo"
	grpc_auth "gitcore/internal/auth/grpc"
	yaconfig "gitcore/internal/config"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/integration_tests/routes"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/services/access/authtypes"
	"gitcore/internal/services/billing/cloud"
	quota_service "gitcore/internal/services/quota"
	"gitcore/internal/services/scheduler"
	userservice "gitcore/internal/services/user"
	"gitcore/internal/testutils"
	"gitcore/internal/testutils/packfile_importer"
	"gitcore/pkg/perfcheck"
	"net/http"
	"os"
	"os/exec"
	"path"
	pb_ci "private_api/generated/yandex/cloud/priv/ci/v1"
	"sync"
	"testing"

	http2 "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-resty/resty/v2"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cockroachdb/errors"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"google.golang.org/grpc"
)

type sshKeyKey struct {
	typ  string
	user entities.UserIdentity
}

type Params struct {
	fx.In

	InviteRepo             interfaces.InviteRepository
	InvitationService      interfaces.InvitationService
	MembershipRepo         interfaces.MembershipRepository
	AccessBindingsService  interfaces.AccessBindingsService
	ClaimService           interfaces.ClaimService
	InternalReleaseService interfaces.InternalReleaseService
	ReleaseService         interfaces.ReleaseService

	CodeExplanationService    interfaces.CodeExplanationService
	CodeExplanationRepository interfaces.CodeExplanationRepository

	AuditService    interfaces.AuditEventsService
	AuditEventsRepo interfaces.AuditEventsRepository

	SubscriptionService   interfaces.SubscriptionService
	RevRepo               interfaces.RevisionRepository
	RevSyncer             interfaces.RevisionSyncer
	OpRepo                interfaces.OperationRepository
	OpService             interfaces.OperationService
	PublicIDService       interfaces.PublicIDService
	MigratorService       interfaces.MigratorService
	MigrationService      interfaces.MigrationService
	SlugService           interfaces.SlugService
	StorageBackend        interfaces.StorageBackend
	AchievementRepository interfaces.AchievementRepository

	OrgRepo           interfaces.OrganizationRepository
	OrgService        interfaces.OrganizationService
	SelfHostedOrgRepo interfaces.SelfHostedOrganizationRepository
	OrgConfigService  interfaces.OrganizationConfigService

	RepoRepo                  interfaces.RepositoryRepository
	RepoRepoWithDeleted       interfaces.RepositoryRepositoryWithSoftDeleted
	RepoService               interfaces.RepositoryService
	RepoContributorRepo       interfaces.RepoContributorRepository
	BranchPolicyBypassService interfaces.BranchPolicyBypassService
	Caas                      interfaces.ConfigAsCodeService

	SrcYamlTemplatesRepository interfaces.SrcYamlTemplatesRepository

	PullRequestRepo           interfaces.PullRequestRepository
	PullRequestRepoFactory    interfaces.BoundPullRequestRepositoryFactory
	PullRequestService        interfaces.PullRequestService
	PullRequestCommentService interfaces.PullRequestCommentService
	PullRequestFeedRepo       interfaces.FeedRepository
	NeuroReviewService        interfaces.NeuroReviewService

	UserRepo    interfaces.UserRepository
	UserService interfaces.UserService
	PatRepo     interfaces.PATRepository
	PatService  interfaces.PATService
	interfaces.PublicSSHKeyService

	UserRelevantReposRepository interfaces.UserRelevantReposRepository

	IssueRepo           interfaces.IssueRepository
	IssueService        interfaces.IssueService
	IssueLinkRepo       interfaces.IssueLinkRepository
	IssueLinkService    interfaces.IssueLinkService
	IssueCommentRepo    interfaces.IssueCommentRepository
	IssueCommentService interfaces.IssueCommentService
	IssueFeedRepo       interfaces.IssueFeedRepository
	MilestoneRepo       interfaces.MilestoneRepository
	MilestoneService    interfaces.MilestoneService
	LabelRepo           interfaces.LabelRepository
	LabelService        interfaces.LabelService
	AttachmentRepo      interfaces.AttachmentRepository
	AttachmentService   interfaces.AttachmentService
	UploadService       interfaces.UploadService

	RepoStatsRepo  interfaces.RepoStatsRepository
	RefRepoFactory interfaces.ReferenceRepositoryFactory
	GitcoreClient  interfaces.GitcoreClient

	PackCache                interfaces.PackCache
	PackCacheDiag            interfaces.PackCacheDiagnostics
	PackSessionFactory       interfaces.PackSessionFactory
	GitFSFactory             interfaces.GitFSFactory
	CGitRepoFactory          interfaces.CGitRepoFactory
	MetaDataRepoFactory      interfaces.MetadataRepositoryFactory
	CommitGraphRepoFactory   interfaces.CommitGraphRepositoryFactory
	RepackerFactory          interfaces.RepackerFactory
	PackfileProcessorFactory interfaces.PackfileProcessorFactory
	QuotaDefaultRepo         interfaces.QuotaDefaultRepository
	IndexRepositoryFactory   interfaces.IndexRepositoryFactory
	LFSRepoFactory           interfaces.LFSObjectRepositoryFactory
	LfsObjectStorageFactory  interfaces.LfsObjectStorageFactory

	AppsecService           interfaces.AppsecService
	MigratedRepoRepo        interfaces.MigratedRepositoryRepository
	MigratedPullRequestRepo interfaces.MigratedPullRequestRepository

	RatingReactionsRepo    interfaces.RatingReactionsRepository
	RatingReactionsService interfaces.RatingReactionsService
	RatingRepo             interfaces.RatingRepository

	WebhookRepo    interfaces.WebhookRepository
	WebhookLogRepo interfaces.WebhookLogRepository
	WebhookService interfaces.WebhookService

	S3Client *s3.Client

	UserSearchManager interfaces.UserSearchManager
	UserSearchService interfaces.UserIndexService
	IndexServices     []interfaces.IndexService `group:"indexService"`

	DbKit         interfaces.UnittestDBKit
	S3Kit         interfaces.UnittestS3Kit
	OpensearchKit interfaces.UnittestOpensearchKit

	OpensearchBackendProxy interfaces.UnittestSearchBackendProxy

	StubClaimService *stubs.StubClaimService

	QuotaService     quota.Client
	Scheduler        interfaces.SchedulerService
	CronjobProviders app.CronjobProviders

	SideEffectChain interfaces.SideEffectChain
	GormDB          commonpg.GormDbFactory
	Pool            *commonpg.ConnectionPool
	TxManager       interfaces.TransactionManager
	LockService     interfaces.LockService

	Fail2Ban            interfaces.Fail2Ban
	HousekeepingMethods []interfaces.HousekeepingMethod `group:"housekeeping"`

	UserUploadService interfaces.UserUploadService

	RestorableConversationsRepository interfaces.RestorableConversationsRepository

	MergerV2Factory interfaces.MergerV2Factory
	CommitService   interfaces.CommitService
}

// web-server and all necessary clients
type IntegrationTestSuite struct {
	suite.Suite

	client      *testutils.HTTPTestClient
	gwClient    *testutils.HTTPTestClient
	pagesClient *testutils.HTTPTestClient
	gitClient   *testutils.HTTPTestClient
	grpcClient  *grpc.ClientConn

	gitHost    string
	sshHost    string
	sshKeysDir string
	sshKeys    map[sshKeyKey]string
	sshKeyLock sync.Mutex

	defaultSSHUser *entities.User

	app *fxtest.App

	// world

	achievements Achievements
	repos        Repos
	reposPk      uint64

	users   Users
	usersPk uint64 // user PK counter, used for creating/loading fixtures for consistent PK

	orgs   Organizations
	orgsPk uint64 // org PK counter, used for creating/loading fixtures for consistent PK

	cfg    *yaconfig.AppConfig
	Params // To load dependencies from fx

	ideService            interfaces.IDEServiceUnitTestInstrumentation
	ciService             interfaces.CIServiceUnitTestInstrumentation
	accessBindingsService interfaces.AccessBindingsTestInstrumentation
	secretService         interfaces.SecretServiceTestInstrumentation
	fluxService           pb_ci.FluxServiceClient

	jwtService              interfaces.JwtService
	selfHostedOrgRepo       interfaces.SelfHostedOrganizationRepository
	notifyBackend           interfaces.UnittestNotifyBackend
	webSocketBackend        interfaces.UnittestWebSocketBackend
	repoQuotaChecker        interfaces.UnittestQuotaChecker
	quotaService            interfaces.QuotaService
	quotaCalculator         interfaces.QuotaCalculator
	billingGrantsBackend    interfaces.BillingGrantsBackend
	AuthMatrixUsers         []*entities.User
	AllAuthRepos            []*entities.Repository
	packfileImporterFactory packfile_importer.PackfileImporterFactory
}

func KnownSSHKeyTypes() []string {
	return []string{"rsa", "ed25519", "ecdsa"}
}

func (suite *IntegrationTestSuite) GetSSHRepoURL(orgSlug string, repoSlug string) string {
	return fmt.Sprintf("%s%s/%s.git", suite.sshHost, orgSlug, repoSlug)
}

func (suite *IntegrationTestSuite) GetSSHRepoURLByRepoID(repoID uint64) string {
	return fmt.Sprintf("%s%d.git", suite.sshHost, repoID)
}

func (suite *IntegrationTestSuite) TearDownSuite() {
	require.NoError(suite.T(), os.RemoveAll(suite.sshKeysDir))
	require.NoError(suite.T(), suite.ClearNotifyMessages())
	require.NoError(suite.T(), suite.ClearWebSocketRequests())
}

func (suite *IntegrationTestSuite) MustReturnDefaultSSHUser(ctx context.Context) *entities.User {
	if suite.defaultSSHUser != nil {
		return suite.defaultSSHUser
	}

	// Create user
	user, err := suite.UserRepo.CreateUser(ctx, entities.User{
		Email:      "example@mail",
		Username:   "1234567890",
		Visibility: entities.Visibilities.Public,
		Identity: entities.UserIdentity{
			ID:  "1234567890",
			Src: entities.IdentityProviders.IAM,
		},
	})
	require.NoError(suite.T(), err)
	suite.defaultSSHUser = user

	return user
}

type Protocol struct {
	Name        string
	Host        string
	PrepareCGit func(workdir *testutils.Workdir, identity entities.UserIdentity) cgit.CGit
}

func (p Protocol) RepoURL(orgSlug string, repoSlug string) string {
	return fmt.Sprintf("%s%s/%s.git", p.Host, orgSlug, repoSlug)
}

func (p Protocol) RepoURLByID(repoID uint64) string {
	return fmt.Sprintf("%s%d.git", p.Host, repoID)
}

func (suite *IntegrationTestSuite) HTTPSProtocol() Protocol {
	return Protocol{
		Name: "https",
		Host: suite.gitHost,
		PrepareCGit: func(workdir *testutils.Workdir, identity entities.UserIdentity) cgit.CGit {
			return workdir.CGit().WithAuthToken(testutils.FakeIAMAuthToken(identity))
		},
	}
}

func (suite *IntegrationTestSuite) SSHProtocol() Protocol {
	return Protocol{
		Name: "ssh",
		Host: suite.sshHost,
		PrepareCGit: func(workdir *testutils.Workdir, identity entities.UserIdentity) cgit.CGit {
			keyType := KnownSSHKeyTypes()[0]
			keyPath := suite.GetSSHKey(keyType, identity)

			return workdir.CGit().
				WithSSHKeyPath(keyPath)
		},
	}
}

func (suite *IntegrationTestSuite) GetSSHKey(keyType string, identity entities.UserIdentity) string {
	// Protect ssh keys generation with lock
	suite.sshKeyLock.Lock()
	defer suite.sshKeyLock.Unlock()

	ctx := context.Background()

	keyKey := sshKeyKey{keyType, identity}
	key, exists := suite.sshKeys[keyKey]
	if exists {
		return key
	}

	t := suite.T()

	if suite.sshKeysDir == "" {
		var err error
		suite.sshKeysDir, err = os.MkdirTemp("", "publickeys")
		require.NoError(t, err)
	}
	sshKey := path.Join(suite.sshKeysDir, identity.ID+"_"+keyType)

	user, err := suite.UserRepo.GetUser(ctx, identity)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ssh-keygen", "-t", keyType, "-m", "pem", "-f", sshKey, "-C", user.Email, "-N", "")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	require.NoError(t, err, "OUT:\n%s\nERR:\n%s\n", stdout.String(), stderr.String())

	suite.sshKeys[keyKey] = sshKey

	err = os.Chmod(sshKey, 0600) // ssh needs these attributes
	require.NoError(t, err)

	pubSSHKeyBytes, err := os.ReadFile(sshKey + ".pub")
	require.NoError(t, err)

	sshKeyID, err := suite.PublicSSHKeyService.Create(ctx, user.ID, interfaces.CreatePublicSSHKeyArgs{
		Name:    "SSH key " + keyType,
		Content: string(pubSSHKeyBytes),
	})
	require.NoError(t, err)
	require.NotZero(t, sshKeyID)

	return sshKey
}

func (suite *IntegrationTestSuite) SetupFlags() {
	// keep
}

func (suite *IntegrationTestSuite) SetupTemporalAttempts(n int32) {
	temporal.MaximumAttempts = n
}

func (suite *IntegrationTestSuite) SetupServer(overrides ...fx.Option) {
	var configOpts yaconfig.UnittestAppConfigOptions
	configOpts.IAMMode = yaconfig.CloudIAM
	configOpts.EnvironmentType = yaconfig.Production

	suite.SetupServerWithConfigOpts(configOpts, overrides...)
}

func (suite *IntegrationTestSuite) SetupServerWithConfigOpts(configOpts yaconfig.UnittestAppConfigOptions, overrides ...fx.Option) {
	perfcheck.PauseAllocCount()

	// MachineID generation for sonyflake requires podIP
	suite.T().Setenv("podIP", "192.168.0.1")

	var cfg *yaconfig.AppConfig
	var s3Client *s3.Client
	var migrator interfaces.MigratorService
	var ci interfaces.CIService
	var ide interfaces.IDEService
	var secrets interfaces.SecretService

	options := []fx.Option{
		NewApp(suite.T(), configOpts),
		fx.Populate(&cfg),
		fx.Populate(&s3Client),
		fx.Populate(&migrator),
		fx.Populate(&suite.Params),
		fx.Populate(&ide),
		fx.Populate(&ci),
		fx.Populate(&suite.accessBindingsService),
		fx.Populate(&secrets),
		fx.Populate(&suite.fluxService),
		fx.Populate(&suite.jwtService),
		fx.Populate(&suite.selfHostedOrgRepo),
		fx.Populate(&suite.notifyBackend),
		fx.Populate(&suite.webSocketBackend),
		fx.Populate(&suite.repoQuotaChecker),
		fx.Populate(&suite.quotaService),
		fx.Populate(&suite.quotaCalculator),
		fx.Populate(&suite.billingGrantsBackend),
		// We want to use packfile importer
		fx.Options(packfile_importer.Module),
		fx.Populate(&suite.packfileImporterFactory),
		fx.Decorate(
			fx.Annotate(
				func(params authtypes.Params) interfaces.AccessService {
					real, err := access.NewAccessService(params)
					require.NoError(suite.T(), err)
					real.DisableCacheForUnittests()
					return access.NewStubAccessService(real, newStubAccessServiceVisitor(suite.T()))
				},
				fx.As(new(interfaces.AccessService)),
			),
			fx.Annotate(
				newStubAuthMiddleware,
				fx.As(new(commongrpc.ServerMiddleware)),
				fx.ResultTags(`name:"auth"`),
			),
			fx.Annotate(
				newEchoAuthMiddleware,
				fx.ResultTags(`name:"auth"`),
			),
			fx.Annotate(
				cloud.NewStubBillingBackend,
				fx.As(new(interfaces.BillingGrantsBackend)),
			),
		),
	}
	options = append(options, overrides...)
	suite.app = fxtest.New(
		suite.T(),
		options...,
	)

	suite.app.RequireStart()
	suite.ciService = ci.(interfaces.CIServiceUnitTestInstrumentation)
	suite.ideService = ide.(interfaces.IDEServiceUnitTestInstrumentation)
	suite.secretService = secrets.(interfaces.SecretServiceTestInstrumentation)

	suite.sshKeys = make(map[sshKeyKey]string)
	suite.gitHost = fmt.Sprintf("%s://%s/", cfg.Platform.HTTPProtocol, cfg.Platform.InternalGitHost)
	suite.sshHost = fmt.Sprintf("ssh://git@%s/", cfg.Platform.InternalSSHHost)

	suite.cfg = cfg
	err := suite.cfg.Validate()
	require.NoError(suite.T(), err)
	suite.client = testutils.MakeClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.Platform.Port))
	suite.gwClient = testutils.MakeClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.PublicAPI.Port))
	suite.pagesClient = testutils.MakeClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.Pages.Port))
	suite.pagesClient.SetRedirectPolicy(resty.NoRedirectPolicy())
	suite.gitClient = testutils.MakeClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.Platform.GitPort))
	suite.grpcClient = commontestutils.MakeGrpcClient(suite.T(), cfg.Platform.GrpcPort)
	// housekeeping
	yaredis.SetupUnittestHousekeeping(suite.T(), cfg.Redis)

	suite.T().Cleanup(func() {
		suite.app.RequireStop()
	})
}

func (suite *IntegrationTestSuite) NotifyMessages(ctx context.Context) []*entities.CloudNotifyRequest {
	if suite.notifyBackend != nil {
		return suite.notifyBackend.History(ctx)
	}
	return nil
}

func (suite *IntegrationTestSuite) ClearNotifyMessages() error {
	if suite.notifyBackend != nil {
		ctx := context.Background()
		return suite.notifyBackend.Reset(ctx)
	}
	return nil
}

func (suite *IntegrationTestSuite) WebSocketRequests() []entities.WsUnittestBackendRequest {
	if suite.webSocketBackend != nil {
		return suite.webSocketBackend.History()
	}
	return nil
}

func (suite *IntegrationTestSuite) ClearWebSocketRequests() error {
	if suite.webSocketBackend != nil {
		return suite.webSocketBackend.Reset()
	}
	return nil
}

func (suite *IntegrationTestSuite) RestartAllCronjobs() {
	for _, provider := range suite.CronjobProviders.Providers {
		require.NoError(suite.T(), provider.RegisterCronjobs(context.Background(), suite.cfg))
	}
}

func (suite *IntegrationTestSuite) CancelAllCronjobs() {
	if suite.Scheduler == nil {
		return
	}
	s := suite.Scheduler.(*scheduler.SchedulerService)
	if s == nil {
		return
	}

	ctx := context.Background()
	err := s.CancelAllCronjobs(ctx)
	if err != nil {
		logging.Info(ctx, "failed to cancel some cronjobs: %+v", err)
	}
}

func (suite *IntegrationTestSuite) CancelAllWorkflows() {
	if suite.Scheduler == nil {
		return
	}
	s := suite.Scheduler.(*scheduler.SchedulerService)
	if s == nil {
		return
	}

	ctx := context.Background()
	err := s.CancelAllWorkflows(ctx)
	if err != nil {
		logging.Info(ctx, "failed to cancel some workflows: %+v", err)
	}
}

func (suite *IntegrationTestSuite) WaitForWorkflows(t testing.TB, tt temporalutils.WorkflowType) {
	require.NotNil(t, suite.Scheduler)

	s, ok := suite.Scheduler.(*scheduler.SchedulerService)
	require.True(t, ok)

	err := s.WaitWorkflowByType(context.Background(), tt)
	require.NoError(t, err)
}

func (suite *IntegrationTestSuite) WaitForCronJob(t testing.TB, scheduleID entities.CronJobID, tt temporalutils.WorkflowType) {
	require.NotNil(t, suite.Scheduler)

	s, ok := suite.Scheduler.(*scheduler.SchedulerService)
	require.True(t, ok)

	require.NoError(t, s.TriggerCronjob(context.Background(), scheduleID))

	err := s.WaitWorkflowByType(context.Background(), tt)
	require.NoError(t, err)
}

type Users struct {
	Kopatych     *entities.User
	Krosh        *entities.User
	Barash       *entities.User
	Pikachu      *entities.User
	Raichu       *entities.User
	Slowpoke     *entities.User
	Admin        *entities.User
	PinPublic    *entities.User
	PinInternal  *entities.User
	BiBiPublic   *entities.User
	BiBiInternal *entities.User
	Internal     *entities.User
	Stub         *entities.User
	Habrotracker *entities.User // some nobody SA whose PAT are used as API keys

	// --- those are used in auth tests and have appropriate roles in AuthRepoPublic ---
	AuthViewer      *entities.User
	AuthContributor *entities.User
	AuthDeveloper   *entities.User
	AuthMaintainer  *entities.User
	AuthAdmin       *entities.User
	AuthOwner       *entities.User
	AuthNobody      *entities.User
	AuthMember      *entities.User
}

type Organizations struct {
	Yandex      *entities.Organization
	Yango       *entities.Organization
	Yandex42    *entities.Organization
	Smeshariki  *entities.Organization
	Pokemon     *entities.Organization
	AuthSandbox *entities.Organization
}

type Achievements struct {
	First      *entities.Achievement
	Second     *entities.Achievement
	Rare       *entities.Achievement
	Variants   *entities.Achievement
	Generative *entities.Achievement
}

type Repos struct {
	Alpha           *entities.Repository
	History         *entities.Repository
	MergeHistory    *entities.Repository
	ListTree        *entities.Repository
	ListBranches    *entities.Repository
	ListTags        *entities.Repository
	PathInfo        *entities.Repository
	TreeDiff        *entities.Repository
	Submodule       *entities.Repository
	SubmoduleParent *entities.Repository
	Blame           *entities.Repository
	ThreeDot        *entities.Repository
	SymLink         *entities.Repository
	PrValidation    *entities.Repository
	BigDiff         *entities.Repository
	DifferentDiffs  *entities.Repository
	Crisscross      *entities.Repository
	Index           *entities.Repository
	MergeBase       *entities.Repository
	UtfAbuse        *entities.Repository
	Dir             *entities.Repository
	BranchPolicy    *entities.Repository
	GithubMigrated  *entities.Repository
	Gitflow         *entities.Repository

	Repro            *entities.Repository // спец репозиторий для репродукции багов
	AuthRepoPublic   *entities.Repository
	AuthRepoPrivate  *entities.Repository
	AuthRepoInternal *entities.Repository
	MockMigration    *entities.Repository

	AlphaFork *entities.Repository
}

func (suite *IntegrationTestSuite) SetupOrganizations(op LoadOp) {
	suite.orgs.Yandex = suite.OrganizationFixture(op, "yandex", suite.users.Admin, nil)
	suite.orgs.Yango = suite.OrganizationFixture(op, "yango", suite.users.Admin, nil)
	suite.orgs.Yandex42 = suite.ExternalOrganizationFixture(op, "yandex42", "yc.organization-manager.yandex")

	suite.orgs.Smeshariki = suite.OrganizationFixture(op, "sme", suite.users.Kopatych, []*entities.User{
		suite.users.Kopatych, suite.users.Krosh, suite.users.Barash,
	})
	suite.orgs.Pokemon = suite.OrganizationFixture(op, "pok", suite.users.Pikachu, []*entities.User{
		suite.users.Pikachu,
		suite.users.Raichu,
		suite.users.Slowpoke,
	})
}

type LoadOp int

type RepoFixtureOption interface {
	ApplyPushOptions(opts *git.PushOptions)
}

type repoFixtureOptionPushCustomRefSpecs struct {
	refSpecs []config.RefSpec
}

func (o repoFixtureOptionPushCustomRefSpecs) ApplyPushOptions(opts *git.PushOptions) {
	opts.RefSpecs = o.refSpecs
}

func WithCustomPushRefSpecs(refSpecs []config.RefSpec) RepoFixtureOption {
	return repoFixtureOptionPushCustomRefSpecs{refSpecs: refSpecs}
}

const (
	Create   LoadOp = 0
	LoadOnly LoadOp = 1
)

func (suite *IntegrationTestSuite) AllAuthUsersWithRoles() map[*entities.User]*iam.Role {
	return map[*entities.User]*iam.Role{
		suite.users.AuthAdmin:       &iam.Roles.RepositoriesAdmin,
		suite.users.AuthDeveloper:   &iam.Roles.RepositoriesDeveloper,
		suite.users.AuthContributor: &iam.Roles.RepositoriesContributor,
		suite.users.AuthMaintainer:  &iam.Roles.RepositoriesMaintainer,
		suite.users.AuthOwner:       &iam.Roles.RepositoriesDeveloper,
		suite.users.AuthViewer:      &iam.Roles.RepositoriesViewer,
		suite.users.AuthMember:      nil,
		suite.users.AuthNobody:      nil,
	}
}

func (suite *IntegrationTestSuite) SetupAuthFixtures(op LoadOp) {
	// orgs collect

	desc := []struct {
		userPtr **entities.User
		org     bool
		id      string
		role    *iam.Role
	}{
		{userPtr: &suite.users.AuthAdmin, id: "a.admin", role: &iam.Roles.RepositoriesAdmin, org: true},
		{userPtr: &suite.users.AuthDeveloper, id: "a.developer", role: &iam.Roles.RepositoriesDeveloper, org: true},
		{userPtr: &suite.users.AuthContributor, id: "a.contributor", role: &iam.Roles.RepositoriesContributor, org: true},
		{userPtr: &suite.users.AuthMaintainer, id: "a.maintainer", role: &iam.Roles.RepositoriesMaintainer, org: true},
		{userPtr: &suite.users.AuthOwner, id: "a.owner", role: &iam.Roles.RepositoriesDeveloper, org: true},
		{userPtr: &suite.users.AuthViewer, id: "a.viewer", role: &iam.Roles.RepositoriesViewer, org: true},
		{userPtr: &suite.users.AuthMember, id: "a.member", role: nil, org: true},
		{userPtr: &suite.users.AuthNobody, id: "a.nobody", role: nil},
	}

	var authMatrixUsers []*entities.User
	var orgMembers []*entities.User

	for _, b := range desc {
		*b.userPtr = suite.UserFixture(entities.UserIdentity{ID: b.id, Src: entities.IdentityProviders.IAM}, entities.Visibilities.Private)
		authMatrixUsers = append(authMatrixUsers, *b.userPtr)
		if b.org {
			orgMembers = append(orgMembers, *b.userPtr)
		}
	}

	suite.AuthMatrixUsers = authMatrixUsers

	suite.orgs.AuthSandbox = suite.OrganizationFixture(op, "aaa", suite.users.AuthAdmin, orgMembers)

	suite.repos.AuthRepoPublic = suite.RepoFixture(op, suite.orgs.AuthSandbox, "aaa-public", testutils.BasicRepo, &entities.Visibilities.Public)
	suite.repos.AuthRepoPrivate = suite.RepoFixture(op, suite.orgs.AuthSandbox, "aaa-private", testutils.BasicRepo, &entities.Visibilities.Private)
	suite.repos.AuthRepoInternal = suite.RepoFixture(op, suite.orgs.AuthSandbox, "aaa-internal", testutils.BasicRepo, &entities.Visibilities.Internal)

	suite.AllAuthRepos = []*entities.Repository{suite.repos.AuthRepoPrivate, suite.repos.AuthRepoPublic, suite.repos.AuthRepoInternal}

	for _, b := range desc {
		if b.role == nil {
			continue
		}
		suite.addRole(suite.T(), *b.userPtr, suite.repos.AuthRepoPublic, *b.role)
		suite.addRole(suite.T(), *b.userPtr, suite.repos.AuthRepoPrivate, *b.role)
		suite.addRole(suite.T(), *b.userPtr, suite.repos.AuthRepoInternal, *b.role)
	}
}

func (suite *IntegrationTestSuite) SetupUsers(LoadOp) {
	suite.users.Kopatych = suite.UserFixture(testutils.UserIdentities.Kopatych, entities.Visibilities.Private)
	suite.users.Krosh = suite.UserFixture(testutils.UserIdentities.Krosh, entities.Visibilities.Private)
	suite.users.Slowpoke = suite.UserFixture(testutils.UserIdentities.Slowpoke, entities.Visibilities.Private)
	suite.users.Raichu = suite.UserFixture(testutils.UserIdentities.Raichu, entities.Visibilities.Private)
	suite.users.Pikachu = suite.UserFixture(testutils.UserIdentities.Pikachu, entities.Visibilities.Private)
	suite.users.Barash = suite.UserFixture(testutils.UserIdentities.Barash, entities.Visibilities.Private)
	suite.users.Admin = suite.UserFixture(testutils.UserIdentities.Admin, entities.Visibilities.Private)
	suite.users.PinPublic = suite.UserFixture(testutils.UserIdentities.PinPublic, entities.Visibilities.Public)
	suite.users.PinInternal = suite.UserFixture(testutils.UserIdentities.PinInternal, entities.Visibilities.Internal)
	suite.users.BiBiPublic = suite.UserFixture(testutils.UserIdentities.BiBiPublic, entities.Visibilities.Public)
	suite.users.BiBiInternal = suite.UserFixture(testutils.UserIdentities.BiBiInternal, entities.Visibilities.Internal)

	suite.users.Internal = suite.UserFixture(testutils.UserIdentities.InternalUser, entities.Visibilities.Internal)
	suite.users.Stub = suite.UserFixture(testutils.StubUserIdentity, entities.Visibilities.Internal)
	suite.users.Habrotracker = suite.UserFixture(testutils.UserIdentities.Habrotracker, entities.Visibilities.Internal)
}

func (suite *IntegrationTestSuite) SetupAchievements(op LoadOp) {
	suite.achievements.First = &entities.Achievement{
		ID:          100100,
		Slug:        "first-achievement",
		Category:    entities.AchievementCategories.Code,
		Title:       entities.LocalizedString{Ru: "Первый достижение", En: "First achievement"},
		Description: entities.LocalizedString{Ru: "Какое-то описание", En: "Some description"},

		MaxLevel: 1,
		IsHidden: false,
		Properties: entities.AchievementProperties{
			Type:   entities.AchievementTypes.Static,
			Images: []string{"first-achievement.png"},
		},
	}

	suite.achievements.Second = &entities.Achievement{
		ID:          100102,
		Slug:        "levellable-achievement",
		Category:    entities.AchievementCategories.Community,
		Title:       entities.LocalizedString{Ru: "Ачивка с уровнями", En: "Levellable achievement"},
		Description: entities.LocalizedString{Ru: "Какое-то описание", En: "Some description"},

		MaxLevel: 999,
		IsHidden: false,
		Properties: entities.AchievementProperties{
			Type:   entities.AchievementTypes.Static,
			Images: []string{"levellable-achievement.png"},
		},
	}

	suite.achievements.Rare = &entities.Achievement{
		ID:          100103,
		Slug:        "rare-achievement",
		Category:    entities.AchievementCategories.Expert,
		Title:       entities.LocalizedString{Ru: "Редкое достижение", En: "Rare achievement"},
		Description: entities.LocalizedString{Ru: "Какое-то описание", En: "Some description"},

		MaxLevel: 1,
		IsHidden: true,
		Properties: entities.AchievementProperties{
			Type:   entities.AchievementTypes.Static,
			Images: []string{"rare-achievement.png"},
		},
	}

	suite.achievements.Variants = &entities.Achievement{
		ID:          100104,
		Slug:        "variants-achievement",
		Category:    entities.AchievementCategories.Expert,
		Title:       entities.LocalizedString{Ru: "Много картинок", En: "Multiple images"},
		Description: entities.LocalizedString{Ru: "Какое-то описание", En: "Some description"},

		MaxLevel: 1,
		IsHidden: false,
		Properties: entities.AchievementProperties{
			Type: entities.AchievementTypes.Static,
			Images: []string{
				"https://storage.yandex.ru/nice-achievement-1.png",
				"https://storage.yandex.ru/nice-achievement-2.png",
				"https://storage.yandex.ru/nice-achievement-3.png",
				"https://storage.yandex.ru/nice-achievement-4.png",
				"https://storage.yandex.ru/nice-achievement-5.png"},
		},
	}

	suite.achievements.Generative = &entities.Achievement{
		ID:          100105,
		Slug:        "generative-achievement",
		Category:    entities.AchievementCategories.Expert,
		Title:       entities.LocalizedString{Ru: "Генеративное достижение", En: "Generative achievement"},
		Description: entities.LocalizedString{Ru: "Какое-то описание", En: "Some description"},

		MaxLevel: 1,
		IsHidden: false,
		Properties: entities.AchievementProperties{
			Type:      entities.AchievementTypes.Generative,
			Generator: "mandelbrot",
		},
	}

	if op == LoadOnly {
		return
	}

	_, _, err := suite.Params.AchievementRepository.SyncAchievements(context.Background(), []*entities.Achievement{
		suite.achievements.First,
		suite.achievements.Second,
		suite.achievements.Rare,
		suite.achievements.Variants,
		suite.achievements.Generative,
	}, false)
	if err != nil {
		suite.T().Fatalf("Failed to sync achievements: %+v", err)
	}

}

func (suite *IntegrationTestSuite) SetupSmallRepos(op LoadOp) {
	suite.repos.Alpha = suite.RepoFixture(op, suite.orgs.Yandex, "alpha", testutils.BasicRepo, nil)
	suite.repos.History = suite.RepoFixture(op, suite.orgs.Yandex, "history", "history.git", nil)
	suite.repos.MergeHistory = suite.RepoFixture(op, suite.orgs.Yandex, "mergehistory", "generated/mergehistory", nil)
	suite.repos.ListTree = suite.RepoFixture(op, suite.orgs.Yandex, "listtree", "generated/listtree", nil)
	suite.repos.ListBranches = suite.RepoFixture(op, suite.orgs.Yandex, "listbranches", "generated/listbranches", nil)
	suite.repos.ListTags = suite.RepoFixture(op, suite.orgs.Yandex, "listtags", "generated/listtags", nil)
	suite.repos.PathInfo = suite.RepoFixture(op, suite.orgs.Yandex, "pathinfo", "generated/pathinfo", nil)
	suite.repos.TreeDiff = suite.RepoFixture(op, suite.orgs.Yandex, "treediff", "generated/tree-diff", nil)
	suite.repos.Submodule = suite.RepoFixture(op, suite.orgs.Yandex, "submodule", "generated/submodule", nil)
	suite.repos.SubmoduleParent = suite.RepoFixture(op, suite.orgs.Yandex, "submodule-parent", "generated/submodule-parent", nil)
	suite.repos.Blame = suite.RepoFixture(op, suite.orgs.Yandex, "blame", "generated/blame", nil)
	suite.repos.ThreeDot = suite.RepoFixture(op, suite.orgs.Yandex, "threedotdiff", "generated/threedotdiff", nil)
	suite.repos.SymLink = suite.RepoFixture(op, suite.orgs.Yandex, "symlinks", "generated/symlinks", nil)
	suite.repos.PrValidation = suite.RepoFixture(op, suite.orgs.Yandex, "prvalidation", "generated/prvalidation", nil)
	suite.repos.BigDiff = suite.RepoFixture(op, suite.orgs.Yandex, "bigdiff", "generated/bigdiff", nil)
	suite.repos.DifferentDiffs = suite.RepoFixture(op, suite.orgs.Yandex, "differentdiffs", "generated/differentdiffs", nil)
	suite.repos.Crisscross = suite.RepoFixture(op, suite.orgs.Yandex, "crisscross", "generated/crisscross", nil)
	suite.repos.Index = suite.RepoFixture(op, suite.orgs.Yandex, "index", "generated/index", nil)
	suite.repos.MergeBase = suite.RepoFixture(op, suite.orgs.Yandex, "mergebase", "generated/mergebase", nil)
	suite.repos.UtfAbuse = suite.RepoFixture(op, suite.orgs.Yandex, "utf", "generated/utf-abuse", nil)
	suite.repos.Dir = suite.RepoFixture(op, suite.orgs.Yandex, "gitlog-dir", "generated/gitlog-dir", nil)
	suite.repos.BranchPolicy = suite.RepoFixture(op, suite.orgs.Yandex, "branchpolicy", "generated/branch-policy", nil)
	suite.repos.Gitflow = suite.RepoFixture(op, suite.orgs.Yandex, "gitflow", "generated/gitflow", nil)

	suite.repos.GithubMigrated = suite.RepoFixture(op, suite.orgs.Yandex, "github-migrated", "generated/github-migrated", nil, WithCustomPushRefSpecs([]config.RefSpec{
		config.DefaultPushRefSpec,
		"refs/tags/*:refs/tags/*",
		"refs/pulls/*:refs/pulls/*",
	}))
	suite.repos.AuthRepoPrivate = suite.RepoFixture(op, suite.orgs.Yandex, "aaa-private", testutils.BasicRepo, &entities.Visibilities.Private)

	suite.repos.AlphaFork = suite.RepoFixtureFork(op, suite.orgs.Yandex, "alpha-repo-fork", suite.repos.Alpha.ID, entities.Visibilities.Public)

}

func (suite *IntegrationTestSuite) SetupBugRepoductionRepo(op LoadOp) {
	suite.repos.Repro = suite.RepoFixture(op, suite.orgs.Yandex, "repro", "https://git.o.cloud.yandex.net/yc/odevplatform.git", nil)
}

func (suite *IntegrationTestSuite) SetupMockMigrationRepo(op LoadOp) {
	suite.repos.MockMigration = suite.RepoFixture(op, suite.orgs.Yandex, "migration-mock", "https://github.com/src-migration-tests/generic", nil)
}

func (suite *IntegrationTestSuite) SetupLargeRepos() {
	suite.RepoFixture(0, suite.orgs.Yandex, "odyssey", "https://github.com/yandex/odyssey", nil)
}

func (suite *IntegrationTestSuite) SetupDefaultQuotaLimits() {
	if err := quota_service.CreateUnittestDefaultsQuotas(suite.QuotaDefaultRepo); err != nil {
		logging.Error(context.Background(), "Failed SetupDefaultQuotaLimits: %+v", err)
	}
}

func (suite *IntegrationTestSuite) importRepo(
	repoID uint64,
	org *entities.Organization,
	repoSlug string,
	fixtureName string,
	visibility *entities.Visibility,
	opts ...RepoFixtureOption,
) *entities.Repository {
	t := suite.T()
	ctx := context.Background()

	if visibility == nil {
		visibility = &entities.Visibilities.Public
	}
	pk, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		ID:           repoID,
		OrgID:        org.ID,
		Name:         repoSlug,
		Description:  "description is " + repoSlug,
		Slug:         repoSlug,
		ProjID:       nil,
		Visibility:   *visibility,
		ProtocolCaps: 0,
		IsEmpty:      true,
	})
	require.NoError(suite.T(), err)

	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, pk)
	require.NoError(suite.T(), err)

	if org.Identity.Src == entities.IdentityProviders.SelfHosted {
		orgIsExists := false
		_, err = suite.OrgRepo.GetOrganizationByID(context.Background(), org.ID)
		if err != nil && !errors.Is(err, except.OrgNotFound) {
			require.NoError(suite.T(), err)
		} else if err == nil {
			orgIsExists = true
		}

		userIsExists := false
		_, err = suite.UserRepo.GetUser(context.Background(), suite.users.Admin.Identity)
		if err != nil && !errors.Is(err, except.UserNotFound) {
			require.NoError(suite.T(), err)
		} else if err == nil {
			userIsExists = true
		}

		if orgIsExists && userIsExists {
			suite.addOrgRole(suite.T(), suite.users.Admin, org, iam.Roles.InternalOrganizationManagerMember)
		}
	}

	suite.addRole(suite.T(), suite.users.Admin, repo, iam.Roles.Admin)

	repoA, err := testutils.EnsureRepo(fixtureName, false)
	require.NoError(t, err)

	ggr := testutils.MakeGoGitRepo(t, repoA)

	_, err = ggr.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{suite.RepoURL(org.Slug, repoSlug)},
	})
	require.NoError(t, err)

	pushOptions := &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.DefaultPushRefSpec,
			"refs/tags/*:refs/tags/*",
		},
		Auth: &http2.BasicAuth{
			Username: suite.users.Admin.Identity.ID,
			Password: testutils.FakeIAMAuthToken(suite.users.Admin.Identity),
		},
	}

	for _, option := range opts {
		option.ApplyPushOptions(pushOptions)
	}

	err = ggr.Push(pushOptions)
	require.NoError(suite.T(), err)

	return repo
}

func (suite *IntegrationTestSuite) ImportRepo(
	org *entities.Organization,
	repoSlug string,
	fixtureName string,
	visibility *entities.Visibility,
	opts ...RepoFixtureOption,
) *entities.Repository {
	t := suite.T()

	pk, err := pkgenerator.GetNextID()
	require.NoError(t, err)

	return suite.importRepo(pk, org, repoSlug, fixtureName, visibility, opts...)
}

func (suite *IntegrationTestSuite) RepoFixture(
	op LoadOp,
	org *entities.Organization,
	repoSlug string,
	fixtureName string,
	visibility *entities.Visibility,
	opts ...RepoFixtureOption,
) *entities.Repository {
	suite.reposPk++
	ctx := context.Background()

	if op == LoadOnly {
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, suite.reposPk)
		require.NoError(suite.T(), err)

		return repo
	}

	return suite.importRepo(suite.reposPk, org, repoSlug, fixtureName, visibility, opts...)
}

func (suite *IntegrationTestSuite) RepoFixtureFork(op LoadOp, org *entities.Organization, slug string, sourceID uint64, v entities.Visibility) *entities.Repository {
	suite.reposPk++
	if op == LoadOnly {
		repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), suite.reposPk)
		require.NoError(suite.T(), err)

		return repo
	}

	fork, err := suite.RepoService.Create(context.Background(), &interfaces.CreateRepositoryArgs{
		ID:                    suite.reposPk,
		Name:                  slug,
		Description:           "description of " + slug,
		Slug:                  slug,
		OrgID:                 org.ID,
		CreatedBy:             suite.users.Admin.ID,
		Visibility:            v,
		Authenticator:         testutils.NewStubAuthenticator(&suite.users.Admin.Identity),
		ForkOriginID:          utils.PtrFromValue(sourceID),
		ForkDefaultBranchOnly: false,
		IsEmpty:               false,
	}, suite.users.Admin)
	require.NoError(suite.T(), err)

	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), fork.ID)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), fork.ID, repo.ID)
	require.Equal(suite.T(), slug, repo.Slug)

	return repo
}

func (suite *IntegrationTestSuite) OrganizationFixture(op LoadOp, orgSlug string, owner *entities.User, members []*entities.User) *entities.Organization {
	suite.orgsPk++
	ctx := context.Background()

	if op == LoadOnly {
		org, err := suite.OrgService.GetOrganizationByID(ctx, testutils.NewStubAuthenticator(&owner.Identity), suite.orgsPk)

		require.NoError(suite.T(), err)
		return org
	}

	_, _, err := suite.OrgService.CreateOrganization(
		ctx,
		testutils.NewStubAuthenticator(&owner.Identity),
		owner,
		entities.IdentityProviders.SelfHosted,
		entities.Organization{
			ID:   suite.orgsPk,
			Slug: orgSlug,
			Identity: entities.OrganizationIdentity{
				ID:  orgSlug,
				Src: entities.IdentityProviders.SelfHosted,
			},
			Claims: entities.OrganizationClaims{
				Name:       orgSlug,
				ExternalID: "",
			},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(suite.T(), err)

	org, err := suite.OrgRepo.GetOrganizationByID(ctx, suite.orgsPk)
	require.NoError(suite.T(), err)

	for _, user := range members {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, org.Identity)
		require.NoError(suite.T(), err)
	}

	return org
}

// noinspection GoUnusedParameter
func (suite *IntegrationTestSuite) ExternalOrganizationFixture(op LoadOp, orgSlug string, orgID string) *entities.Organization {
	ctx := context.Background()
	res := schemas.Organization{}

	if op == LoadOnly {
		org, err := suite.OrgService.GetOrganization(ctx,
			testutils.NewStubAuthenticator(&suite.users.Admin.Identity),
			entities.OrganizationIdentity{
				ID:  orgID,
				Src: entities.IdentityProviders.IAM,
			})
		require.NoError(suite.T(), err)
		return org
	}

	resp, err := suite.client.R().
		SetResult(&res).
		SetBody(&schemas.ImportExternalOrganizationRequest{
			Identity: entities.OrganizationIdentity{
				ID:  orgID,
				Src: entities.IdentityProviders.IAM,
			},
			Slug: utils.PtrFromValue(orgSlug),
		}).
		Post("/api/v1/orgs/import")
	yarequire.StatusCode(suite.T(), resp, err, http.StatusCreated)
	require.Equal(suite.T(), orgSlug, res.Slug)

	org, err := suite.OrgRepo.GetOrganization(ctx, orgSlug)
	require.NoError(suite.T(), err)
	return org
}

func (suite *IntegrationTestSuite) emptyRepo(orgSlug, slug string, owner *entities.User) (url string, id uint64) {
	t := suite.T()

	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(owner.Identity).
		SetResult(&repo).
		SetBody(schemas.CreateRepositoryRequest{
			Name:    slug,
			Slug:    slug,
			OrgSlug: utils.PtrFromValue(orgSlug),
		}).
		Post("/api/v1/repos/")).
		MustBe(t, 201)

	return suite.RepoURL(orgSlug, slug), repo.ID.MustToUint64()
}

func (suite *IntegrationTestSuite) RepoURL(orgSlug, slug string) string {
	return fmt.Sprintf("%s%s/%s.git", suite.gitHost, orgSlug, slug)
}

func (suite *IntegrationTestSuite) URL(repo *entities.Repository) string {
	return fmt.Sprintf("%s%s/%s.git", suite.gitHost, repo.OrgSlug, repo.Slug)
}

func (suite *IntegrationTestSuite) ResetCounters() {
	suite.usersPk = 0
	suite.reposPk = 0
	suite.orgsPk = 0
}

func (suite *IntegrationTestSuite) UserFixture(idx entities.UserIdentity, visibility entities.Visibility) *entities.User {
	suite.usersPk++ // for deterministic tests

	ctx := context.Background()

	user, err := suite.UserService.GetUser(ctx, testutils.NewStubAuthenticator(&idx), idx)

	if errors.Is(err, except.UserNotFound) {
		// pass
	} else {
		require.NoError(suite.T(), err)
		if user != nil {
			return user
		}
	}

	user, err = suite.UserService.CreateUser(ctx, testutils.NewStubAuthenticator(&idx), interfaces.UserCreateArgs{
		ID:       suite.usersPk,
		Identity: idx,
		UUID:     testutils.UUIDFromInt(suite.usersPk),
	}, false)
	require.NoError(suite.T(), err)

	err = suite.UserService.UpdateUser(ctx, user, interfaces.UserUpdateArgs{
		Visibility: &visibility,
	})
	require.NoError(suite.T(), err)

	user.Visibility = visibility

	if !idx.IsServiceAccount {
		_, err := suite.UserService.SetFlag(ctx, user, entities.UserFlags.Onboarded, true)
		require.NoError(suite.T(), err)

		err = suite.UserService.CreatePersonalOrg(ctx, user, testutils.NewStubAuthenticator(&idx), true)
		require.NoError(suite.T(), err)
	}
	suite.WaitForWorkflows(suite.T(), userservice.CreatePersonalOrgWorkflowType)

	return user
}

func (suite *IntegrationTestSuite) RandomUserFixture() *entities.User {
	ctx := context.Background()
	visibility := entities.Visibilities.Public
	idx := entities.UserIdentity{
		ID:  uuid.New().String(),
		Src: entities.IdentityProviders.IAM,
	}
	user, err := suite.UserService.CreateUser(ctx, testutils.NewStubAuthenticator(&idx), interfaces.UserCreateArgs{Identity: idx}, false)
	require.NoError(suite.T(), err)

	_, err = suite.UserService.SetFlag(ctx, user, entities.UserFlags.Onboarded, true)
	require.NoError(suite.T(), err)

	err = suite.UserService.CreatePersonalOrg(ctx, user, testutils.NewStubAuthenticator(&idx), true)
	require.NoError(suite.T(), err)

	err = suite.UserService.UpdateUser(ctx, user, interfaces.UserUpdateArgs{
		Visibility: &visibility,
	})
	require.NoError(suite.T(), err)

	user.Visibility = visibility

	suite.WaitForWorkflows(suite.T(), userservice.CreatePersonalOrgWorkflowType)

	return user
}

func (suite *IntegrationTestSuite) mustGenUniqueSlug() string {
	id, err := pkgenerator.GetNextID()
	require.NoError(suite.T(), err)

	return fmt.Sprintf("slug%d", id)
}

func (suite *IntegrationTestSuite) makeNewFile(cg cgit.CGit, fname string) {
	suite.makeNewFileWithContent(cg, fname, fmt.Sprintf("some_build_stuff in file %s\n", fname))
}

func (suite *IntegrationTestSuite) commitAll(cg cgit.CGit, commitMessage string, branch string) {
	t := suite.T()
	cg.Must(t, "add", "*")
	cg.Must(t, "commit", "-m", commitMessage)
	cg.Must(t, "push", "-u", "origin", branch)
}

func (suite *IntegrationTestSuite) makeNewFileWithContent(cg cgit.CGit, fname string, content string) {
	t := suite.T()

	dir := path.Dir(fname)
	if dir != "." {
		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), dir), 0755))
	}
	nf, err := os.Create(path.Join(cg.Path(), fname))
	require.NoError(t, err)

	_, err = nf.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, nf.Close())
}

func (suite *IntegrationTestSuite) addRole(t *testing.T, user *entities.User, repo *entities.Repository, role iam.Role) {
	err := suite.AccessBindingsService.CreateBindings(context.Background(), access.StubAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: repo.Object(), Role: role},
	})
	if errors.Is(err, except.EntityConflict) {
		// idempotent create
		return
	}
	require.NoError(t, err)
}

func (suite *IntegrationTestSuite) addOrgRole(t *testing.T, user *entities.User, org *entities.Organization, role iam.Role) {
	if role == iam.Roles.Viewer {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, org.Identity)
		require.NoError(t, err)
		return
	}

	err := suite.AccessBindingsService.CreateBindings(context.Background(), access.StubAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: role},
	})

	if err != nil && !errors.Is(err, except.EntityConflict) {
		require.NoError(t, err)
	}

	err = suite.MembershipRepo.Create(context.Background(), user.Identity, org.Identity)
	if err != nil && !errors.Is(err, except.EntityConflict) {
		require.NoError(t, err)
	}
}

func (suite *IntegrationTestSuite) addExternalOrgRole(t *testing.T, user *entities.User, org *entities.Organization, role iam.Role) {
	err := suite.AccessBindingsService.CreateBindings(context.Background(), access.StubAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: role},
	})

	if err != nil && !errors.Is(err, except.EntityConflict) {
		require.NoError(t, err)
	}
}

func (suite *IntegrationTestSuite) ensureDefaultMaster(repo *entities.Repository) {
	suite.mustBash(repo, `
		git branch -m master
		touch something.txt
		git add . && git commit -m "Master commit"
	`)
	cmdtag, err := suite.Pool.Exec(context.Background(), `update git_repos set default_branch = 'master' where id = $1`, repo.ID)
	require.NoError(suite.T(), err)
	require.EqualValues(suite.T(), 1, cmdtag.RowsAffected())
}

func (suite *IntegrationTestSuite) mustBash(repo *entities.Repository, bash string) {
	t := suite.T()
	tmpDir := t.TempDir()
	cg := cgit.NewCGit(tmpDir).
		WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, repo.FullSlug())
	logging.Info(nil, cg.Must(t, "clone", repoURL, "repo"))

	cg = cgit.NewCGit(path.Join(tmpDir, "repo")).
		WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

	require.NoError(t, cg.BashNoCapture(bash))
	logging.Info(nil, cg.Must(t, "push", "--all", "-f"))
	logging.Info(nil, cg.Must(t, "push", "--tags"))
}

func (suite *IntegrationTestSuite) setQuotaLimit(t *testing.T, orgID uint64, quotaID entities.QuotaID, limit int64) (cancel func()) {
	err := suite.quotaService.UpdateLimit(context.Background(), orgID, quotaID, limit)
	require.NoError(t, err)

	return func() {
		suite.resetQuotaLimit(t, orgID, quotaID)
	}
}

func (suite *IntegrationTestSuite) resetQuotaLimit(t *testing.T, orgID uint64, quotaID entities.QuotaID) {
	_, err := suite.Pool.Exec(
		context.Background(),
		"UPDATE org_quotas SET override_default_limit = NULL WHERE org_id=$1 AND quota_id=$2",
		orgID, quotaID,
	)
	require.NoError(t, err)
}

func (suite *IntegrationTestSuite) getFakeAuthenticator(identity entities.UserIdentity) interfaces.Authenticator {
	return accesscommon.NewIAMTokenAuthenticator(testutils.FakeIAMAuthToken(identity), &identity)
}

type stubAccessServiceVisitor struct {
	t                 *testing.T
	grpcGoldenMethods map[string]map[iam.Permission]struct{}
	mu                sync.RWMutex
}

var _ access.StubAccessServiceVisitor = &stubAccessServiceVisitor{}

func newStubAccessServiceVisitor(t *testing.T) *stubAccessServiceVisitor {
	result := &stubAccessServiceVisitor{
		t:                 t,
		grpcGoldenMethods: make(map[string]map[iam.Permission]struct{}),
		mu:                sync.RWMutex{},
	}
	grpcGldApp := routes.LoadGolden(t)
	for _, s := range grpcGldApp.Services {
		for _, m := range s.Methods {
			name := "/" + s.Name + "/" + m.Name
			result.grpcGoldenMethods[name] = make(map[iam.Permission]struct{})
			for _, p := range m.Permissions {
				result.grpcGoldenMethods[name][p] = struct{}{}
			}
		}
	}
	return result
}

func (s *stubAccessServiceVisitor) reloadGolden() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.grpcGoldenMethods = make(map[string]map[iam.Permission]struct{})
	grpcGldApp := routes.LoadGolden(s.t)
	for _, service := range grpcGldApp.Services {
		for _, m := range service.Methods {
			name := "/" + service.Name + "/" + m.Name
			s.grpcGoldenMethods[name] = make(map[iam.Permission]struct{})
			for _, p := range m.Permissions {
				s.grpcGoldenMethods[name][p] = struct{}{}
			}
		}
	}
}

func (s *stubAccessServiceVisitor) VisitAuthentificate(ctx context.Context) {
}

func (s *stubAccessServiceVisitor) VisitAuthorize(ctx context.Context, permissions ...iam.Permission) {
	methodNameAny := ctx.Value(stubAuthMiddlewareKey)
	if methodNameAny == nil {
		return
	}

	methodName, ok := methodNameAny.(string)
	assert.True(s.t, ok)

	needSave := false
	defer func() {
		if !update.IsUpdate() || !needSave {
			return
		}
		for _, p := range permissions {
			routes.AddPermToGolden(s.t, methodName, p)
		}
		s.reloadGolden()
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	permsMap, ok := s.grpcGoldenMethods[methodName]
	if !update.IsUpdate() {
		hint := fmt.Sprintf("\nPlease, update golden file by running %s with --update flag (or pass GOLDENFILE=1)", s.t.Name())
		assert.True(s.t, ok, "Method "+methodName+" is not registered in "+routes.GoldenFileName()+hint)
	} else if !ok {
		needSave = true
	}

	for _, p := range permissions {
		_, ok := permsMap[p]
		if !update.IsUpdate() {
			hint := fmt.Sprintf("\nPlease, update golden file by running %s with --update flag (or pass GOLDENFILE=1)", s.t.Name())
			assert.True(s.t, ok, "Permission "+string(p)+" for method "+methodName+" is not registered in "+routes.GoldenFileName()+"."+hint)
		} else if !ok {
			needSave = true
		}
	}
}

type stubAuthMiddleware struct {
	real commongrpc.ServerMiddleware
}

var stubAuthMiddlewareKey commongrpc.ServerMiddleware = &stubAuthMiddleware{}

func newStubAuthMiddleware(accessService interfaces.AccessService, userService interfaces.UserService) commongrpc.ServerMiddleware {
	return &stubAuthMiddleware{real: grpc_auth.NewAuthMiddleware(accessService, userService)}
}

func (s *stubAuthMiddleware) InterceptUnary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx = context.WithValue(ctx, stubAuthMiddlewareKey, info.FullMethod)
	return s.real.InterceptUnary(ctx, req, info, handler)
}

func (s *stubAuthMiddleware) InterceptStream(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return s.real.InterceptStream(srv, stream, info, handler)
}

func newEchoAuthMiddleware(
	accessService interfaces.AccessService,
	userService interfaces.UserService,
) echo.MiddlewareFunc {
	real := echo_auth.NewAuthMiddleware(accessService, userService)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			name := "/HTTP/" + c.Path()
			c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), stubAuthMiddlewareKey, name)))
			return real(next)(c)
		}
	}
}
