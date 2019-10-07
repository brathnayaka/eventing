// +build all handler

package eventing

import (
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

func TestError(t *testing.T) {
	time.Sleep(5 * time.Second)
	itemCount := 100
	expectedCount := itemCount * 3
	handler := "error"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	pumpBucketOps(opsType{count: itemCount}, &rateLimit{})
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter)
	if expectedCount != eventCount {
		t.Error("For", "TestError",
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestCRLF(t *testing.T) {
	time.Sleep(5 * time.Second)
	itemCount := 100
	handler := "n1ql_newlines"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	pumpBucketOps(opsType{count: itemCount}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "TestCRLF",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestImportExport(t *testing.T) {
	time.Sleep(5 * time.Second)
	n1qlHandler := "n1ql_insert_on_update"
	flushFunctionAndBucket(n1qlHandler)
	createAndDeployFunction(n1qlHandler, n1qlHandler, &commonSettings{})

	bucketHandler := "bucket_op_on_update"
	flushFunctionAndBucket(bucketHandler)
	createAndDeployFunction(bucketHandler, bucketHandler, &commonSettings{})

	waitForDeployToFinish(n1qlHandler)
	waitForDeployToFinish(bucketHandler)

	exportResponse, err := makeRequest("GET", strings.NewReader(""), exportFunctionsURL)
	if err != nil {
		t.Errorf("Unable to export Functions %v, err : %v\n", exportFunctionsURL, err)
		return
	}

	flushFunctionAndBucket(bucketHandler)
	flushFunctionAndBucket(n1qlHandler)

	_, err = makeRequest("POST", strings.NewReader(string(exportResponse)), importFunctionsURL)
	if err != nil {
		t.Errorf("Unable import Functions, err : %v\n", err)
		return
	}

	response, err := makeRequest("GET", strings.NewReader(""), functionsURL)
	if err != nil {
		t.Errorf("Unable to list Functions err : %v\n", err)
		return
	}

	var functionsList []map[string]interface{}
	err = json.Unmarshal(response, &functionsList)
	if err != nil {
		t.Errorf("Unable to unmarshal response err %v\n", err)
		return
	}

	if !functionExists(n1qlHandler, functionsList) {
		t.Errorf("Import/Export failed for %v", n1qlHandler)
		return
	}

	if !functionExists(bucketHandler, functionsList) {
		t.Errorf("Import/Export failed for %v", bucketHandler)
		return
	}
}

func functionExists(name string, functionsList []map[string]interface{}) bool {
	for _, function := range functionsList {
		if funcName := function["appname"].(string); name == funcName {
			return true
		}
	}

	return false
}

func TestDeployUndeployLoopNonDefaultSettings(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	for i := 0; i < 5; i++ {
		createAndDeployFunction(handler, handler, &commonSettings{thrCount: 4, batchSize: 77})

		pumpBucketOps(opsType{}, &rateLimit{})
		eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
		if itemCount != eventCount {
			t.Error("For", "DeployUndeployLoopNonDefaultSettings",
				"expected", itemCount,
				"got", eventCount,
			)
		}

		dumpStats()
		log.Println("Undeploying app:", handler)
		setSettings(handler, false, false, &commonSettings{})
		bucketFlush("default")
		bucketFlush("hello-world")
		time.Sleep(30 * time.Second)
	}

	deleteFunction(handler)
}

func TestOnUpdateN1QLOp(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "n1ql_insert_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnUpdateN1QLOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnUpdateBucketOpDefaultSettings(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnUpdateBucketOpDefaultSettings",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnUpdateBucketOpNonDefaultSettings(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{thrCount: 4, batchSize: 77})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnUpdateBucketOpNonDefaultSettings",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnUpdateBucketOpDefaultSettings10K(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployLargeFunction(handler, handler, &commonSettings{}, 10*1024)

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnUpdateBucketOpDefaultSettings",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnUpdateBucketOpDefaultSettings100K(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployLargeFunction(handler, handler, &commonSettings{}, 100*1024)

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnUpdateBucketOpDefaultSettings",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnDeleteBucketOp(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_delete"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{delete: true}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnDeleteBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestOnDeleteBucketOp5K(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_delete"
	flushFunctionAndBucket(handler)
	createAndDeployLargeFunction(handler, handler, &commonSettings{}, 5*1024)

	pumpBucketOps(opsType{delete: true}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "OnDeleteBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestTimerBucketOp(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "TimerBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestTimerInPastBucketOp(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer_in_past"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "TestTimerInPastBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestDeployUndeployLoopTimer(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer"
	flushFunctionAndBucket(handler)

	for i := 0; i < 5; i++ {
		createAndDeployFunction(handler, handler, &commonSettings{})

		pumpBucketOps(opsType{}, &rateLimit{})
		eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
		if itemCount != eventCount {
			t.Error("For", "DeployUndeployLoopTimer",
				"expected", itemCount,
				"got", eventCount,
			)
		}

		dumpStats()
		log.Println("Undeploying app:", handler)
		setSettings(handler, false, false, &commonSettings{})
		bucketFlush("default")
		bucketFlush("hello-world")
		time.Sleep(30 * time.Second)
	}

	deleteFunction(handler)
}

func TestMultipleHandlers(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler1 := "bucket_op_on_update"
	handler2 := "bucket_op_on_delete"

	flushFunctionAndBucket(handler1)
	flushFunctionAndBucket(handler2)

	createAndDeployFunction(handler1, handler1, &commonSettings{})
	createAndDeployFunction(handler2, handler2, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "MultipleHandlers UpdateOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	pumpBucketOps(opsType{delete: true}, &rateLimit{})
	eventCount = verifyBucketOps(itemCount*2, statsLookupRetryCounter)
	if eventCount != itemCount*2 {
		t.Error("For", "MultipleHandlers DeleteOp",
			"expected", itemCount*2,
			"got", eventCount,
		)
	}

	dumpStats()

	// Pause the apps
	setSettings(handler1, true, false, &commonSettings{})
	setSettings(handler2, true, false, &commonSettings{})

	flushFunctionAndBucket(handler1)
	flushFunctionAndBucket(handler2)
}

/* Disabling pause/resume tests as it's retired. Keeping the tests around as
we might need to use these tests for allowing deploy/undeploy to pick
things up from last checkpointed seq no
func TestPauseResumeLoopDefaultSettings(t *testing.T) {
	time.Sleep(5 * time.Second)

	handler := "bucket_op_on_update"

	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	for i := 0; i < 5; i++ {
		if i > 0 {
			setSettings(handler, true, true, &commonSettings{})
		}

		pumpBucketOps(opsType{startIndex: itemCount * i}, &rateLimit{})
		eventCount := verifyBucketOps(itemCount*(i+1), statsLookupRetryCounter)
		if itemCount*(i+1) != eventCount {
			t.Error("For", "PauseAndResumeLoopDefaultSettings",
				"expected", itemCount*(i+1),
				"got", eventCount,
			)
		}

		dumpStats()
		fmt.Printf("Pausing the app: %s\n\n", handler)
		setSettings(handler, true, false, &commonSettings{})
	}

	flushFunctionAndBucket(handler)
}

func TestPauseResumeLoopNonDefaultSettings(t *testing.T) {
	time.Sleep(5 * time.Second)

	handler := "bucket_op_on_update"

	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{thrCount: 4, batchSize: 77, workerCount: 4})

	for i := 0; i < 5; i++ {
		if i > 0 {
			setSettings(handler, true, true, &commonSettings{thrCount: 4, batchSize: 77, workerCount: 4})
		}

		pumpBucketOps(opsType{startIndex: itemCount * i}, &rateLimit{})
		eventCount := verifyBucketOps(itemCount*(i+1), statsLookupRetryCounter)
		if itemCount*(i+1) != eventCount {
			t.Error("For", "PauseAndResumeLoopNonDefaultSettings",
				"expected", itemCount*(i+1),
				"got", eventCount,
			)
		}

		dumpStats()
		fmt.Printf("Pausing the app: %s\n\n", handler)
		setSettings(handler, true, false, &commonSettings{})
	}

	flushFunctionAndBucket(handler)
}*/

func TestCommentUnCommentOnDelete(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "on_delete_bucket_op_comment"
	appName := "comment_uncomment_test"
	flushFunctionAndBucket(handler)

	createAndDeployFunction(appName, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "CommentUnCommentOnDelete",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	log.Println("Undeploying app:", appName)
	setSettings(appName, false, false, &commonSettings{})

	time.Sleep(30 * time.Second)

	handler = "on_delete_bucket_op_uncomment"
	createAndDeployFunction(appName, handler, &commonSettings{})

	pumpBucketOps(opsType{delete: true}, &rateLimit{})
	eventCount = verifyBucketOps(0, statsLookupRetryCounter)
	if eventCount != 0 {
		t.Error("For", "CommentUnCommentOnDelete",
			"expected", 0,
			"got", eventCount,
		)
	}

	dumpStats()
	log.Println("Undeploying app:", appName)
	setSettings(appName, false, false, &commonSettings{})

	time.Sleep(5 * time.Second)
	flushFunctionAndBucket(handler)
}

func TestCPPWorkerCleanup(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{batchSize: 100, workerCount: 16})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "CPPWorkerCleanup",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
	time.Sleep(30 * time.Second)
}

func TestWithUserXattrs(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "on_delete_bucket_op_comment"
	flushFunctionAndBucket(handler)
	pumpBucketOps(opsType{}, &rateLimit{})
	pumpBucketOps(opsType{writeXattrs: true, xattrPrefix: "_1"}, &rateLimit{})
	createAndDeployFunction(handler, handler, &commonSettings{streamBoundary: "from_now"})
	waitForDeployToFinish(handler)

	pumpBucketOps(opsType{writeXattrs: true, xattrPrefix: "_2"}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "WithUserXattrs",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestMetaBucketDelete(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	log.Println("Deleting metadata bucket:", metaBucket)
	deleteBucket(metaBucket)
	log.Println("Deleted metadata bucket:", metaBucket)

	waitForUndeployToFinish(handler)

	time.Sleep(10 * time.Second)
	createBucket(metaBucket, bucketmemQuota)
	flushFunctionAndBucket(handler)
}

func TestMetaBucketDeleteWithBootstrap(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	go createAndDeployFunction(handler, handler, &commonSettings{})
	time.Sleep(20 * time.Second) // let the boostrap process to make some progress

	log.Println("Deleting metadata bucket:", metaBucket)
	deleteBucket(metaBucket)
	log.Println("Deleted metadata bucket:", metaBucket)

	time.Sleep(10 * time.Second)
	waitForUndeployToFinish(handler)

	time.Sleep(10 * time.Second)
	createBucket(metaBucket, bucketmemQuota)
	flushFunctionAndBucket(handler)
}

func TestSourceBucketDelete(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	log.Println("Deleting source bucket:", srcBucket)
	deleteBucket(srcBucket)
	log.Println("Deleted source bucket:", srcBucket)

	waitForUndeployToFinish(handler)

	time.Sleep(10 * time.Second)
	createBucket(srcBucket, bucketmemQuota)
	flushFunctionAndBucket(handler)
}

func TestSourceBucketDeleteWithBootstrap(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	go createAndDeployFunction(handler, handler, &commonSettings{})
	time.Sleep(20 * time.Second) // let the boostrap process to make some progress

	log.Println("Deleting source bucket:", srcBucket)
	deleteBucket(srcBucket)
	log.Println("Deleted source bucket:", srcBucket)

	time.Sleep(10 * time.Second)
	waitForUndeployToFinish(handler)

	time.Sleep(10 * time.Second)
	createBucket(srcBucket, bucketmemQuota)
	flushFunctionAndBucket(handler)
}

func TestSourceAndMetaBucketDeleteWithBootstrap(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)

	go createAndDeployFunction(handler, handler, &commonSettings{})
	time.Sleep(20 * time.Second) // let the boostrap process to make some progress

	log.Println("Deleting source bucket:", srcBucket)
	deleteBucket(srcBucket)
	log.Println("Deleted source bucket:", srcBucket)

	log.Println("Deleting metadata bucket:", metaBucket)
	deleteBucket(metaBucket)
	log.Println("Deleted metadata bucket:", metaBucket)

	time.Sleep(10 * time.Second)
	waitForUndeployToFinish(handler)

	time.Sleep(10 * time.Second)
	createBucket(srcBucket, bucketmemQuota)
	createBucket(metaBucket, bucketmemQuota)
	flushFunctionAndBucket(handler)
}

func TestUndeployDuringBootstrap(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{workerCount: 1})

	pumpBucketOps(opsType{}, &rateLimit{})

	time.Sleep(15 * time.Second)

	dumpStats()
	setSettings(handler, false, false, &commonSettings{})

	// Double check needed because of a logic on supersupervisor, that
	// handles bucket deletion.
	waitForUndeployToFinish(handler)
	time.Sleep(20 * time.Second)
	waitForUndeployToFinish(handler)

	flushFunctionAndBucket(handler)
}

func TestDeleteBeforeUndeploy(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	pumpBucketOps(opsType{}, &rateLimit{})

	setSettings(handler, false, false, &commonSettings{})
	resp, _ := deleteFunction(handler)
	if resp.httpResponseCode == 200 {
		t.Error("Expected non 200 response code")
	}

	if resp.httpResponseCode != 200 && resp.Name != "ERR_APP_DELETE_NOT_ALLOWED" {
		t.Error("Expected ERR_APP_DELETE_NOT_ALLOWED got", resp.Name)
	}

	waitForUndeployToFinish(handler)
	flushFunctionAndBucket(handler)
}

func TestUndeployWhenTimersAreFired(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer_with_large_context"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	go pumpBucketOps(opsType{count: itemCount * 8}, &rateLimit{})

	time.Sleep(30 * time.Second)
	setSettings(handler, false, false, &commonSettings{})
	waitForUndeployToFinish(handler)
	time.Sleep(100 * time.Second)
	itemCount, err := getBucketItemCount(metaBucket)
	if itemCount != 0 && err == nil {
		t.Error("Item count in metadata bucket after undeploy", itemCount)
	}

	flushFunctionAndBucket(handler)
}

func TestUndeployWithKVFailover(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "TimerBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	addNodeFromRest("127.0.0.1:9003", "kv")
	rebalanceFromRest([]string{""})
	waitForRebalanceFinish()

	failoverFromRest([]string{"127.0.0.1:9003"})
	time.Sleep(10 * time.Second)

	setSettings(handler, false, false, &commonSettings{})

	time.Sleep(60 * time.Second)
	rebalanceFromRest([]string{""})
	waitForRebalanceFinish()
	waitForUndeployToFinish(handler)

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestTimerOverwrite(t *testing.T) {
	time.Sleep(5 * time.Second)
	handler := "bucket_op_with_timer_overwritten"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{})
	waitForDeployToFinish(handler)

	time.Sleep(10 * time.Second)
	itemCountB, err := getBucketItemCount(metaBucket)
	if err != nil {
		log.Printf("Encountered err: %v while fetching item count from meta bucket: %s\n", err, metaBucket)
		return
	}

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter)
	if itemCount != eventCount {
		t.Error("For", "TimerBucketOp",
			"expected", itemCount,
			"got", eventCount,
		)
	}

	time.Sleep(10 * time.Second)
	itemCountA, err := getBucketItemCount(metaBucket)
	if err != nil {
		log.Printf("Encountered err: %v while fetching item count from meta bucket: %s\n", err, metaBucket)
		return
	}

	if itemCountB != itemCountA {
		t.Error("Expected", itemCountB, "got", itemCountA)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
}

func TestUndeployBackdoorDuringBootstrap(t *testing.T) {
	time.Sleep(5 * time.Second)

	addNodeFromRest("127.0.0.1:9003", "kv")
	rebalanceFromRest([]string{""})
	waitForRebalanceFinish()

	failoverFromRest([]string{"127.0.0.1:9003"})

	handler := "bucket_op_on_update"
	flushFunctionAndBucket(handler)
	createAndDeployLargeFunction(handler, handler, &commonSettings{workerCount: 1}, 10*1024)

	go pumpBucketOps(opsType{}, &rateLimit{})

	time.Sleep(10 * time.Second)
	dumpStats()

	setSettings(handler, false, false, &commonSettings{})
	setRetryCounter(handler)

	time.Sleep(60 * time.Second)
	waitForUndeployToFinish(handler)
	dumpStats()
	resp, _ := deleteFunction(handler)
	if resp.httpResponseCode != 200 {
		t.Error("Expected 200 response code, got code", resp.httpResponseCode, resp.Name)
	}

	flushFunctionAndBucket(handler)
	rebalanceFromRest([]string{""})
	waitForRebalanceFinish()
}

// Disabling as for the time being source bucket mutations aren't allowed
/* func TestSourceBucketMutations(t *testing.T) {
	time.Sleep(time.Second * 5)
	handler := "source_bucket_update_op"
	flushFunctionAndBucket(handler)
	createAndDeployFunction(handler, handler, &commonSettings{recursiveBehavior: "disable"})

	pumpBucketOps(opsType{}, &rateLimit{})
	eventCount := verifySourceBucketOps(itemCount*2, statsLookupRetryCounter)
	if itemCount*2 != eventCount {
		t.Error("For", "WithUserXattrs",
			"expected", itemCount*2,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(handler)
} */
