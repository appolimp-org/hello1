package integrationtests

import (
	"common/logging"
	"common/testutils/yarequire"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/services/code_explanation"
	pb "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

//go:embed "code_explanation_fixtures/hexagonal.html"
var TestHTML string

var validSnippet2 = `package port

import ...

// Transactor runs logic inside a single database transaction
type Transactor interface {
    // WithinTransaction runs a function within a database transaction.
    //
    // Transaction is propagated in the context,
    // so it is important to propagate it to underlying repositories.
    // Function commits if error is nil, and rollbacks if not.
    // It returns the same error.
    WithinTransaction(context.Context, func(ctx context.Context) error) error
}`

var validSnippet = `package port

import ...

// CarRepository car persistence repository
type CarRepository interface {
    UpsertCar(ctx context.Context, car *model.Car) error
    GetCar(ctx context.Context, id string) (*model.Car, error)
    GetCarsBatch(ctx context.Context, ids []string) ([]model.Car, error)
    GetCarsByTimePeriod(ctx context.Context, from, to time.Time) ([]model.Car, error)
    GetCarsByModel(ctx context.Context, model string) ([]model.Car, error)
    DeleteCar(ctx context.Context, id string) error
}`

//go:embed code_explanation_fixtures/snippet.txt
var e2eSnippet string

func (suite *RwApiTestSuite) TestCodeExplanationE2E() {
	suite.T().Skipf("Skipping E2E tests")

	// WARNING THIS TEST GOES TO REAL INFERENCE;
	// USE IT TO DEBUG LLM ISSUES ONLY!

	suite.pollCodeExplanation(e2eSnippet, "https://habr.com/ru/articles/972708/", nil)
}

func (suite *RwApiTestSuite) TestCodeExplanation() {
	t := suite.T()
	svc := suite.Params.CodeExplanationService
	ctx := context.Background()
	url := "https://habr.com/ru/articles/651799/"

	mocker := svc.(code_explanation.CodeExplanationMocker)
	returnTestHTML := func(ctx context.Context, url string) (string, error) {
		return TestHTML, nil
	}
	fluked := func(ctx context.Context, code string, article *code_explanation.Article) (chan code_explanation.Delta, error) {
		ch := make(chan code_explanation.Delta, 32)
		go func() {
			ch <- code_explanation.Delta{Finished: true, Err: errors.New("llm is temporary offline")}
		}()
		return ch, nil
	}

	foobarbaz := func(ctx context.Context, code string, article *code_explanation.Article) (chan code_explanation.Delta, error) {
		ch := make(chan code_explanation.Delta, 32)
		go func() {
			ch <- code_explanation.Delta{Data: "foo "}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Data: "bar "}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Data: "baz!"}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Finished: true}
		}()
		return ch, nil
	}
	foobarbam := func(ctx context.Context, code string, article *code_explanation.Article) (chan code_explanation.Delta, error) {
		ch := make(chan code_explanation.Delta, 32)
		go func() {
			ch <- code_explanation.Delta{Data: "foo "}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Data: "bar "}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Data: "bam!"}
			time.Sleep(100 * time.Millisecond)
			ch <- code_explanation.Delta{Finished: true}
		}()
		return ch, nil
	}
	// TODO: handle "lost" jobs: those were never picked up by workers

	t.Run("happy path", func(t *testing.T) {
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		resp := suite.pollCodeExplanation(validSnippet, url, nil)
		require.Equal(t, "foo bar baz!", resp.Explanation)
		// next should be from cache:
		var scheduleResult pb.ScheduleCodeExplanationResponse

		restResponse, err := suite.gwClient.
			As(suite.users.Habrotracker.Identity).
			SetBody(pb.ScheduleCodeExplanationRequest{Code: validSnippet, ArticleUrl: url}).
			Post("/integrations/code_explanations")

		yarequire.StatusCode(t, restResponse, err, 200)
		err = protojson.Unmarshal(restResponse.Body(), &scheduleResult)
		require.NoError(t, err)

		require.False(t, scheduleResult.WasScheduled)
		require.Equal(t, resp.Explanation, scheduleResult.Job.Explanation)
		require.Equal(t, resp.Status, scheduleResult.Job.Status)
	})

	t.Run("several snippets", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)

		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		resp := suite.pollCodeExplanation(validSnippet, url, nil)
		require.Equal(t, "foo bar baz!", resp.Explanation)

		mocker.SetInference(foobarbam)
		resp = suite.pollCodeExplanation(validSnippet2, url, nil)
		require.Equal(t, "foo bar bam!", resp.Explanation)

	})

	t.Run("403", func(t *testing.T) {
		restResponse, err := suite.gwClient.
			As(suite.users.Pikachu.Identity).
			SetBody(pb.ScheduleCodeExplanationRequest{Code: validSnippet, ArticleUrl: url}).
			Post("/integrations/code_explanations")

		yarequire.StatusCode(t, restResponse, err, 403)
	})

	t.Run("dogpiling", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)

		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		waiter := make(chan struct{})
		mocker.SetInference(func(ctx context.Context, code string, article *code_explanation.Article) (chan code_explanation.Delta, error) {
			ch := make(chan code_explanation.Delta, 32)
			go func() {
				ch <- code_explanation.Delta{Data: "foo bar "}
				<-waiter
				ch <- code_explanation.Delta{Data: "baz!"}
				ch <- code_explanation.Delta{Finished: true}
			}()
			return ch, nil
		})
		cntr := 10
		resp := suite.pollCodeExplanation(validSnippet, url, func(t *testing.T) {
			_, wasScheduled, err := svc.Schedule(ctx, validSnippet, url, true)
			require.NoError(t, err)
			require.False(t, wasScheduled)

			cntr--
			if cntr == 0 {
				close(waiter)
			}
		})

		require.Equal(t, "foo bar baz!", resp.Explanation)
	})

	t.Run("rejected, bad snippet", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		resp := suite.pollCodeExplanation("totally not in the article", url, nil)
		require.Equal(t, pb.CodeExplanation_rejected, resp.Status)

		// will not reschedule this one...
		_, wasScheduled, err := svc.Schedule(ctx, "totally not in the article", url, true)
		require.NoError(t, err)
		require.True(t, wasScheduled)
	})

	t.Run("304", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)
		mdl, wasScheduled, err := svc.Schedule(ctx, validSnippet, url, false)
		require.NoError(t, err)
		require.True(t, wasScheduled)
		mdl.Status = entities.CodeExplanationStatuses.Processing
		mdl.Rev = 33
		err = svc.UpdateProgress(ctx, mdl, "hewwo")
		require.NoError(t, err)

		restResponse, err := suite.gwClient.
			As(suite.users.Habrotracker.Identity).
			Get(fmt.Sprintf("/integrations/code_explanations/%s/?rev=34", mdl.Hash))

		yarequire.StatusCode(t, restResponse, err, 304)

	})

	t.Run("fluke + recover", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(fluked)

		resp := suite.pollCodeExplanation(validSnippet, url, nil)
		require.Equal(t, pb.CodeExplanation_failed, resp.Status)

		mocker.SetInference(foobarbaz)
		resp = suite.pollCodeExplanation(validSnippet, url, nil)
		require.Equal(t, pb.CodeExplanation_complete, resp.Status)
	})

	t.Run("reject + recover", func(t *testing.T) {
		_, err := svc.DropCacheByURL(ctx, url)
		require.NoError(t, err)
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		resp := suite.pollCodeExplanation("invalid snippet", url, nil)
		require.Equal(t, pb.CodeExplanation_rejected, resp.Status)

		resp = suite.pollCodeExplanation(validSnippet, url, nil)
		require.Equal(t, pb.CodeExplanation_complete, resp.Status)
	})

	t.Run("stuck status + recover", func(t *testing.T) {
		mocker.SetDownloader(returnTestHTML)
		mocker.SetInference(foobarbaz)

		statuses := []entities.CodeExplanationStatus{
			entities.CodeExplanationStatuses.Created,
			entities.CodeExplanationStatuses.Queued,
			entities.CodeExplanationStatuses.Processing,
		}

		prepareStuckTask := func(status entities.CodeExplanationStatus) (hash string) {
			_, err := svc.DropCacheByURL(ctx, url)
			require.NoError(t, err)

			mdl, _, err := suite.Params.CodeExplanationRepository.Schedule(context.Background(), validSnippet, url, true)
			require.NoError(t, err)

			expiredTimeout := time.Now().UTC().Add(-code_explanation.StatusTimeout[status] - 1*time.Second)
			_, err = suite.Pool.Exec(context.Background(),
				"UPDATE code_explanations SET status_updated_at = $1, status = $2 WHERE hash = $3", expiredTimeout, status, mdl.Hash)
			require.NoError(t, err)

			return mdl.Hash
		}

		// recover on polling
		for _, status := range statuses {
			hash := prepareStuckTask(status)

			var pollResult pb.CodeExplanation
			restResponse, err := suite.gwClient.
				As(suite.users.Habrotracker.Identity).
				Get(fmt.Sprintf("/integrations/code_explanations/%s/", hash))

			yarequire.StatusCode(t, restResponse, err, 200)
			err = protojson.Unmarshal(restResponse.Body(), &pollResult)
			require.NoError(t, err)

			require.Equal(t, pb.CodeExplanation_failed, pollResult.Status)
		}

		// recover on scheduling
		for _, status := range statuses {
			prepareStuckTask(status)

			resp := suite.pollCodeExplanation(validSnippet, url, nil)
			require.Equal(t, pb.CodeExplanation_complete, resp.Status)
		}
	})
}

// unsupported URL
func (suite *RwApiTestSuite) pollCodeExplanation(validSnippet, url string, cb func(t *testing.T)) *pb.CodeExplanation {
	t := suite.T()
	ctx := context.Background()

	var scheduleResult pb.ScheduleCodeExplanationResponse

	restResponse, err := suite.gwClient.
		As(suite.users.Habrotracker.Identity).
		SetBody(pb.ScheduleCodeExplanationRequest{Code: validSnippet, ArticleUrl: url}).
		Post("/integrations/code_explanations")

	yarequire.StatusCode(t, restResponse, err, 200)
	err = protojson.Unmarshal(restResponse.Body(), &scheduleResult)
	require.NoError(t, err)

	require.True(t, scheduleResult.WasScheduled)

	hash := scheduleResult.Job.Id

	startedAt := time.Now()
	//var rev int64

	for {
		time.Sleep(100 * time.Millisecond)

		//result, err := svc.GetByHash(ctx, hash, false)
		var pollResult pb.CodeExplanation
		restResponse, err = suite.gwClient.
			As(suite.users.Habrotracker.Identity).
			Get(fmt.Sprintf("/integrations/code_explanations/%s/", hash))

		yarequire.StatusCode(t, restResponse, err, 200)
		err = protojson.Unmarshal(restResponse.Body(), &pollResult)
		require.NoError(t, err)

		if cb != nil {
			cb(t)
		}

		if pollResult.Status != pb.CodeExplanation_queued &&
			pollResult.Status != pb.CodeExplanation_created &&
			pollResult.Status != pb.CodeExplanation_processing {
			return &pollResult
		}
		logging.Info(ctx, "[polling] status: %v; rev: %v; explanation %v", pollResult.Status, pollResult.Rev, pollResult.Explanation)

		if time.Since(startedAt) > 5*time.Second {
			t.Error("did not converge in timeout")
		}
	}
}
