package integrationtests

import (
	"cmp"
	"common/temporalutils"
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/workflow"
	"strings"
	"time"
)

func SleepWorkflow(ctx workflow.Context, _ struct{}) error {
	return workflow.Sleep(ctx, 1*time.Hour)
}

func (suite *RepoApiTestSuite) TestScheduler_List() {
	workflowTypes := map[temporalutils.WorkflowType]int{
		"TestWorkflow1": 1,
		"TestWorkflow2": 2,
		"TestWorkflow3": 3,
	}

	for wft := range workflowTypes {
		temporalutils.RegisterWorkflow(SleepWorkflow, wft)
	}
	params := struct{}{}
	for wft, count := range workflowTypes {
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("%s:%d", wft, i)
			_, err := suite.Scheduler.ExecuteWorkflow(context.Background(), id, wft, params)
			require.NoError(suite.T(), err)
		}
	}

	// it's tricky to wait until all workflows start
	retryInterval := 1 * time.Second
	retries := 5
	for {
		retries--
		if retries < 0 {
			suite.FailNow("condition not satisfied after all retries are exhausted")
		}

		ok := true
		for wft, count := range workflowTypes {
			runs, err := suite.Scheduler.ListRunningWorkflows(context.Background(), wft)
			require.NoError(suite.T(), err)
			for _, run := range runs {
				require.True(suite.T(), strings.HasPrefix(run.WorkflowID, string(wft)))
			}

			switch cmp.Compare(len(runs), count) {
			case -1:
				ok = false
			case 1:
				suite.FailNowf("too much workflows", "type %s: %d workflows, expected %d", string(wft), len(runs), count)
			}
		}

		if ok {
			break
		}
		time.Sleep(retryInterval) // retry later
	}
}
