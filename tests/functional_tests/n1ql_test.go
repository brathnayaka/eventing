// +build all n1ql

package eventing

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/couchbase/gocb.v1"
)

func testFlexReset(handler string, t *testing.T) {
	functionName := t.Name()
	itemCount := 1000
	flushFunctionAndBucket(functionName)
	pumpBucketOps(opsType{count: itemCount}, &rateLimit{})
	createAndDeployFunction(functionName, handler, &commonSettings{
		lcbInstCap:       2,
		deadlineTimeout:  70,
		executionTimeout: 60,
	})
	waitForDeployToFinish(functionName)

	eventCount := verifyBucketOps(itemCount, statsLookupRetryCounter*2)
	if itemCount != eventCount {
		t.Error("For", functionName,
			"expected", itemCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestRecursiveMutationN1QL(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_insert_same_src"
	flushFunctionAndBucket(functionName)
	mainStoreResponse := createAndDeployFunction(functionName, handler, &commonSettings{})
	if mainStoreResponse.err != nil {
		t.Errorf("Unable to POST to main store, err : %v\n", mainStoreResponse.err)
		return
	}

	var response map[string]interface{}
	err := json.Unmarshal(mainStoreResponse.body, &response)
	if err != nil {
		t.Errorf("Failed to unmarshal response from Main store, err : %v\n", err)
		return
	}

	if response["name"].(string) != "ERR_HANDLER_COMPILATION" {
		t.Error("Compilation must fail")
		return
	}
}

func TestFlexReset1(t *testing.T) {
	handler := "n1ql_flex_reset1"
	testFlexReset(handler, t)
}

func TestFlexReset2(t *testing.T) {
	handler := "n1ql_flex_reset2"
	testFlexReset(handler, t)
}

func TestN1QLLabelledBreak(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_labelled_break"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{}, &rateLimit{})
	expectedCount := itemCount * 2
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", "N1QLLabelledBreak",
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLUnlabelledBreak(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_unlabelled_break"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{}, &rateLimit{})
	expectedCount := itemCount * 2
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", "N1QLUnlabelledBreak",
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLThrowStatement(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_throw_statement"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{}, &rateLimit{})
	expectedCount := itemCount * 2
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", "N1QLThrowStatement",
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLNestedForLoop(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_nested_for_loops"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{lcbInstCap: 6})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{}, &rateLimit{})
	expectedCount := itemCount
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", "N1QLNestedForLoop",
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLPosParams(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_pos_params"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{}, &rateLimit{})
	expectedCount := itemCount
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", t.Name(),
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLExhaustConnPool(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_exhaust_conn_pool"
	flushFunctionAndBucket(functionName)
	createAndDeployFunction(functionName, handler, &commonSettings{})
	waitForDeployToFinish(functionName)
	pumpBucketOps(opsType{count: 100}, &rateLimit{})
	expectedCount := 100
	eventCount := verifyBucketOps(expectedCount, statsLookupRetryCounter*2)
	if expectedCount != eventCount {
		t.Error("For", t.Name(),
			"expected", expectedCount,
			"got", eventCount,
		)
	}

	dumpStats()
	flushFunctionAndBucket(functionName)
}

func TestN1QLGraceLowTimeOut(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_timeout_query"

	flushFunctionAndBucket(functionName)
	pumpBucketOpsSrc(opsType{count: 30000}, "default", &rateLimit{})
	pumpBucketOpsSrc(opsType{count: 30000}, "hello-world", &rateLimit{})

	createAndDeployFunction(functionName, handler, &commonSettings{
		deadlineTimeout:  60,
		executionTimeout: 17,
		streamBoundary:   "from_now",
		aliasSources:     []string{dstBucket},
		aliasHandles:     []string{"dst_bucket"},
		metaBucket:       metaBucket,
		sourceBucket:     srcBucket,
	})
	waitForDeployToFinish(functionName)

	defer func() {
		flushFunctionAndBucket(functionName)
	}()

	pumpBucketOpsSrc(opsType{count: 1}, "default", &rateLimit{})
	eventCount := verifyBucketOps(30001, statsLookupRetryCounter*2)
	if eventCount != 30001 {
		t.Error("For", "TestN1QLGraceLowTimeOut",
			"expected", 3001,
			"got", eventCount,
		)
		return
	}

	//Check Timeout error from n1ql
	cluster, _ := gocb.Connect("couchbase://127.0.0.1:12000")
	cluster.Authenticate(gocb.PasswordAuthenticator{
		Username: rbacuser,
		Password: rbacpass,
	})
	bucket, err := cluster.OpenBucket(dstBucket, "")
	if err != nil {
		fmt.Println("Bucket open, err: ", err)
		t.Error("Error open result bucket")
		return
	}
	defer bucket.Close()
	var val string
	_, err = bucket.Get("result_key", &val)
	if err != nil {
		t.Error("Error getting result from bucket:", err)
		return
	}

	if !strings.Contains(val, "Timeout 15s exceeded") {
		fmt.Println("Result Value: ", val)
		t.Error("Error: Expected timeout error from n1ql")
	}

	dumpStats()
}

func TestN1QLGraceSufficientTimeOut(t *testing.T) {
	functionName := t.Name()
	handler := "n1ql_notimeout_query"

	flushFunctionAndBucket(functionName)
	pumpBucketOpsSrc(opsType{count: 30000}, "default", &rateLimit{})
	pumpBucketOpsSrc(opsType{count: 30000}, "hello-world", &rateLimit{})

	createAndDeployFunction(functionName, handler, &commonSettings{
		deadlineTimeout:  60,
		executionTimeout: 55,
		streamBoundary:   "from_now",
		aliasSources:     []string{dstBucket},
		aliasHandles:     []string{"dst_bucket"},
		metaBucket:       metaBucket,
		sourceBucket:     srcBucket,
	})
	waitForDeployToFinish(functionName)

	defer func() {
		flushFunctionAndBucket(functionName)
	}()

	pumpBucketOpsSrc(opsType{count: 1}, "default", &rateLimit{})
	eventCount := verifyBucketOps(30001, statsLookupRetryCounter*2)
	if eventCount != 30001 {
		t.Error("For", "TestN1QLGraceLowTimeOut",
			"expected", 30001,
			"got", eventCount,
		)
		return
	}

	//Check onupdate success in result
	cluster, _ := gocb.Connect("couchbase://127.0.0.1:12000")
	cluster.Authenticate(gocb.PasswordAuthenticator{
		Username: rbacuser,
		Password: rbacpass,
	})
	bucket, err := cluster.OpenBucket(dstBucket, "")
	if err != nil {
		fmt.Println("Bucket open, err: ", err)
		t.Error("Error open result bucket")
		return
	}
	defer bucket.Close()
	var val string
	_, err = bucket.Get("result_key", &val)
	if err != nil {
		t.Error("Error getting result from bucket:", err)
		return
	}

	if !strings.Contains(val, "onupdate success") {
		fmt.Println("Result Value: ", val)
		t.Error("Error: Expected onupdate success")
	}

	dumpStats()
}
