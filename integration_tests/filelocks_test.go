package integrationtests

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"testing"
)

func (suite *RwApiTestSuite) TestFileLock_Create() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	t.Run("create lock", func(t *testing.T) {
		req := &schemas.CreateFileLockRequest{
			Path: "some/file/path.txt",
		}

		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/locks")).
			MustBe(t, 201)

		require.Equal(t, req.Path, res.Lock.Path)
		require.Equal(t, suite.users.Kopatych.Username, res.Lock.Owner.Name)
		require.Nil(t, res.Message)
	})

	t.Run("create lock second time", func(t *testing.T) {
		req := &schemas.CreateFileLockRequest{
			Path: "some/file/path.txt",
		}

		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetError(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/locks")).
			MustBe(t, 409)

		require.Equal(t, req.Path, res.Lock.Path)
		require.Equal(t, suite.users.Kopatych.Username, res.Lock.Owner.Name)
		require.Equal(t, "Already locked", *res.Message)
	})

	t.Run("create lock no permission", func(t *testing.T) {
		req := &schemas.CreateFileLockRequest{
			Path: "some/file/path.txt",
		}

		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.Anonymous().
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetError(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/locks")).
			MustBe(t, 401)
	})
}

func (suite *RwApiTestSuite) TestFileLock_Delete() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	t.Run("delete lock", func(t *testing.T) {
		path := "first/file/path.txt"
		lock := suite.createLock(t, path, suite.users.Kopatych)

		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Post(fmt.Sprintf("/yandex/alpha.git/info/lfs/locks/%s/unlock", lock.ID))).
			MustBe(t, 200)

		require.Equal(t, path, res.Lock.Path)
		require.Equal(t, suite.users.Kopatych.Username, res.Lock.Owner.Name)
		require.Nil(t, res.Message)
	})

	t.Run("delete lock not owner", func(t *testing.T) {
		path := "second/file/path.txt"
		lock := suite.createLock(t, path, suite.users.Kopatych)

		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Krosh).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetError(res).
			Post(fmt.Sprintf("/yandex/alpha.git/info/lfs/locks/%s/unlock", lock.ID))).
			MustBe(t, 403)
	})

	t.Run("delete lock with force", func(t *testing.T) {
		path := "third/file/path.txt"
		lock := suite.createLock(t, path, suite.users.Kopatych)

		req := &schemas.DeleteFileLockRequest{
			Force: utils.PtrFromValue(true),
		}
		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Krosh).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetBody(req).
			SetResult(res).
			Post(fmt.Sprintf("/yandex/alpha.git/info/lfs/locks/%s/unlock", lock.ID))).
			MustBe(t, 200)

		require.Equal(t, path, res.Lock.Path)
		require.Equal(t, suite.users.Kopatych.Username, res.Lock.Owner.Name)
	})

	t.Run("delete not existing lock", func(t *testing.T) {
		res := &schemas.FileLockResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Krosh).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetError(res).
			Post("/yandex/alpha.git/info/lfs/locks/123/unlock")).
			MustBe(t, 404)
	})
}

func (suite *RwApiTestSuite) TestFileLock_List() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	lock1 := suite.createLock(t, "first/file/path.txt", suite.users.Kopatych)
	lock2 := suite.createLock(t, "second/file/path.txt", suite.users.Kopatych)
	lock3 := suite.createLock(t, "third/file/path.txt", suite.users.Krosh)
	lock4 := suite.createLock(t, "forth/file/path.txt", suite.users.Krosh)

	t.Run("list locks", func(t *testing.T) {
		res := &schemas.ListFileLocksResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Get("/yandex/alpha.git/info/lfs/locks")).
			MustBe(t, 200)

		require.Equal(t, 4, len(res.Locks))
		compareLocks(t, lock4, res.Locks[0])
		compareLocks(t, lock3, res.Locks[1])
		compareLocks(t, lock2, res.Locks[2])
		compareLocks(t, lock1, res.Locks[3])
	})

	t.Run("list locks with filter by path", func(t *testing.T) {
		res := &schemas.ListFileLocksResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Get(fmt.Sprintf("/yandex/alpha.git/info/lfs/locks?path=%s", lock2.Path))).
			MustBe(t, 200)

		require.Equal(t, 1, len(res.Locks))

		compareLocks(t, lock2, res.Locks[0])
	})

	t.Run("list locks with filter by id", func(t *testing.T) {
		res := &schemas.ListFileLocksResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Get(fmt.Sprintf("/yandex/alpha.git/info/lfs/locks?id=%s", lock3.ID))).
			MustBe(t, 200)

		require.Equal(t, 1, len(res.Locks))

		compareLocks(t, lock3, res.Locks[0])
	})

	t.Run("empty list", func(t *testing.T) {
		res := &schemas.ListFileLocksResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Slowpoke).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Get("/yandex/alpha.git/info/lfs/locks?path=123")).
			MustBe(t, 200)

		require.Equal(t, 0, len(res.Locks))
	})
}

func (suite *RwApiTestSuite) TestFileLock_ListVerify() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	lock1 := suite.createLock(t, "first/file/path.txt", suite.users.Kopatych)
	lock2 := suite.createLock(t, "second/file/path.txt", suite.users.Kopatych)
	lock3 := suite.createLock(t, "third/file/path.txt", suite.users.Krosh)
	lock4 := suite.createLock(t, "forth/file/path.txt", suite.users.Krosh)

	t.Run("list locks verify one user", func(t *testing.T) {
		res := &schemas.ListFileLocksVerifyResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Post("/yandex/alpha.git/info/lfs/locks/verify")).
			MustBe(t, 200)

		require.Equal(t, 2, len(res.Ours))
		compareLocks(t, lock2, res.Ours[0])
		compareLocks(t, lock1, res.Ours[1])
		require.Equal(t, 2, len(res.Theirs))
		compareLocks(t, lock4, res.Theirs[0])
		compareLocks(t, lock3, res.Theirs[1])
	})

	t.Run("list locks verify another user", func(t *testing.T) {
		res := &schemas.ListFileLocksVerifyResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Krosh).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Post("/yandex/alpha.git/info/lfs/locks/verify")).
			MustBe(t, 200)

		require.Equal(t, 2, len(res.Ours))
		compareLocks(t, lock4, res.Ours[0])
		compareLocks(t, lock3, res.Ours[1])
		require.Equal(t, 2, len(res.Theirs))
		compareLocks(t, lock2, res.Theirs[0])
		compareLocks(t, lock1, res.Theirs[1])
	})

	t.Run("list locks verify no edit permission", func(t *testing.T) {
		testutils.Expect(suite.gitClient.Anonymous().
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			Post("/yandex/alpha.git/info/lfs/locks/verify")).
			MustBe(t, 401)
	})

	suite.addRole(t, suite.users.Pikachu, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	t.Run("list locks verify with edit permission", func(t *testing.T) {
		res := &schemas.ListFileLocksVerifyResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Pikachu).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			Post("/yandex/alpha.git/info/lfs/locks/verify")).
			MustBe(t, 200)

		require.Equal(t, 4, len(res.Theirs))
		compareLocks(t, lock4, res.Theirs[0])
		compareLocks(t, lock3, res.Theirs[1])
		compareLocks(t, lock2, res.Theirs[2])
		compareLocks(t, lock1, res.Theirs[3])
	})
}

func (suite *RwApiTestSuite) createLock(t *testing.T, path string, user *entities.User) *schemas.FileLock {
	req := &schemas.CreateFileLockRequest{
		Path: path,
	}

	res := &schemas.FileLockResponse{}
	testutils.Expect(suite.gitClient.AsBasic(user).
		SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
		SetResult(res).
		SetBody(req).
		Post("/yandex/alpha.git/info/lfs/locks")).
		MustBe(t, 201)

	return res.Lock
}

func compareLocks(t *testing.T, expected *schemas.FileLock, actual *schemas.FileLock) {
	require.Equal(t, expected.ID, actual.ID)
	require.Equal(t, expected.Path, actual.Path)
	require.Equal(t, expected.Owner.Name, actual.Owner.Name)
}
