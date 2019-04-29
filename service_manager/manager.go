package servicemanager

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof" // For debugging
	"sync"
	"time"

	"github.com/couchbase/cbauth"
	"github.com/couchbase/cbauth/service"
	"github.com/couchbase/eventing/common"
	"github.com/couchbase/eventing/logging"
	"github.com/couchbase/eventing/util"
)

//NewServiceMgr creates handle for ServiceMgr, which implements cbauth service.Manager
func NewServiceMgr(config util.Config, rebalanceRunning bool, superSup common.EventingSuperSup) *ServiceMgr {

	logging.Infof("ServiceMgr::newServiceMgr config: %rm rebalanceRunning: %v", fmt.Sprintf("%#v", config), rebalanceRunning)

	mu := &sync.RWMutex{}

	mgr := &ServiceMgr{
		mu:      mu,
		servers: make([]service.NodeID, 0),
		state: state{
			rebalanceID:   "",
			rebalanceTask: nil,
			rev:           0,
			servers:       make([]service.NodeID, 0),
		},
		statsWritten: true,
		stopTracerCh: make(chan struct{}, 1),
		superSup:     superSup,
	}

	mgr.config.Store(config)
	mgr.nodeInfo = &service.NodeInfo{
		NodeID: service.NodeID(config["uuid"].(string)),
	}

	mgr.rebalanceRunning = rebalanceRunning
	mgr.servers = append(mgr.servers, mgr.nodeInfo.NodeID)
	mgr.waiters = make(waiters)

	mgr.initService()
	return mgr
}

func (m *ServiceMgr) initService() {
	cfg := m.config.Load()
	m.adminHTTPPort = cfg["eventing_admin_http_port"].(string)
	m.adminSSLPort = cfg["eventing_admin_ssl_port"].(string)
	m.certFile = cfg["eventing_admin_ssl_cert"].(string)
	m.keyFile = cfg["eventing_admin_ssl_key"].(string)
	m.restPort = cfg["rest_port"].(string)
	m.uuid = cfg["uuid"].(string)
	m.initErrCodes()

	logging.Infof("ServiceMgr::initService adminHTTPPort: %v", m.adminHTTPPort)
	logging.Infof("ServiceMgr::initService adminSSLPort: %v", m.adminSSLPort)
	logging.Infof("ServiceMgr::initService certFile: %v", m.certFile)
	logging.Infof("ServiceMgr::initService keyFile: %v", m.keyFile)

	util.Retry(util.NewFixedBackoff(time.Second), nil, getHTTPServiceAuth, m)

	go func(m *ServiceMgr) {
		for {
			err := m.registerWithServer()
			if err != nil {
				logging.Infof("Retrying to register against cbauth_service")
				time.Sleep(2 * time.Second)
			} else {
				break
			}
		}
	}(m)

	m.disableDebugger()

	mux := http.NewServeMux()

	//pprof REST APIs
	mux.HandleFunc("/debug/pprof/", m.indexHandler)
	mux.HandleFunc("/debug/pprof/cmdline", m.cmdlineHandler)
	mux.HandleFunc("/debug/pprof/profile", m.profileHandler)
	mux.HandleFunc("/debug/pprof/symbol", m.symbolHandler)
	mux.HandleFunc("/debug/pprof/trace", m.traceHandler)

	//expvar REST APIs
	mux.HandleFunc("/debug/vars", m.expvarHandler)

	// Internal REST APIs
	mux.HandleFunc("/cleanupEventing", m.cleanupEventing)
	mux.HandleFunc("/clearEventStats", m.clearEventStats)
	mux.HandleFunc("/die", m.die)
	mux.HandleFunc("/deleteApplication/", m.deletePrimaryStoreHandler)
	mux.HandleFunc("/deleteAppTempStore/", m.deleteTempStoreHandler)
	mux.HandleFunc("/freeOSMemory", m.freeOSMemory)
	mux.HandleFunc("/getAggBootstrappingApps", m.getAggBootstrappingApps)
	mux.HandleFunc("/getAggEventProcessingStats", m.getAggEventProcessingStats)
	mux.HandleFunc("/getAggRebalanceProgress", m.getAggRebalanceProgress)
	mux.HandleFunc("/getAggRebalanceStatus", m.getAggRebalanceStatus)
	mux.HandleFunc("/getApplication/", m.getPrimaryStoreHandler)
	mux.HandleFunc("/getAppTempStore/", m.getTempStoreHandler)
	mux.HandleFunc("/getBootstrappingApps", m.getBootstrappingApps)
	mux.HandleFunc("/getConsumerPids", m.getEventingConsumerPids)
	mux.HandleFunc("/getCpuCount", m.getCpuCount)
	mux.HandleFunc("/getCreds", m.getCreds)
	mux.HandleFunc("/getDcpEventsRemaining", m.getDcpEventsRemaining)
	mux.HandleFunc("/getDebuggerUrl/", m.getDebuggerURL)
	mux.HandleFunc("/getDeployedApps", m.getDeployedApps)
	mux.HandleFunc("/getErrorCodes", m.getErrCodes)
	mux.HandleFunc("/getEventProcessingStats", m.getEventProcessingStats)
	mux.HandleFunc("/getExecutionStats", m.getExecutionStats)
	mux.HandleFunc("/getFailureStats", m.getFailureStats)
	mux.HandleFunc("/getLatencyStats", m.getLatencyStats)
	mux.HandleFunc("/getLocallyDeployedApps", m.getLocallyDeployedApps)
	mux.HandleFunc("/getNamedParams", m.getNamedParamsHandler)
	mux.HandleFunc("/getRebalanceProgress", m.getRebalanceProgress)
	mux.HandleFunc("/getRebalanceStatus", m.getRebalanceStatus)
	mux.HandleFunc("/getRunningApps", m.getRunningApps)
	mux.HandleFunc("/getSeqsProcessed", m.getSeqsProcessed)
	mux.HandleFunc("/getLocalDebugUrl/", m.getLocalDebugURL)
	mux.HandleFunc("/getWorkerCount", m.getWorkerCount)
	mux.HandleFunc("/logFileLocation", m.logFileLocation)
	mux.HandleFunc("/parseQuery", m.parseQueryHandler)
	mux.HandleFunc("/saveAppTempStore/", m.saveTempStoreHandler)
	mux.HandleFunc("/setApplication/", m.savePrimaryStoreHandler)
	mux.HandleFunc("/setSettings/", m.setSettingsHandler)
	mux.HandleFunc("/startDebugger/", m.startDebugger)
	mux.HandleFunc("/startTracing", m.startTracing)
	mux.HandleFunc("/triggerGC", m.triggerGC)
	mux.HandleFunc("/stopDebugger/", m.stopDebugger)
	mux.HandleFunc("/stopTracing", m.stopTracing)
	mux.HandleFunc("/uuid", m.getNodeUUID)
	mux.HandleFunc("/version", m.getNodeVersion)
	mux.HandleFunc("/writeDebuggerURL/", m.writeDebuggerURLHandler)

	// Public REST APIs
	mux.HandleFunc("/api/v1/status", m.statusHandler)
	mux.HandleFunc("/api/v1/stats", m.statsHandler)
	mux.HandleFunc("/api/v1/config", m.configHandler)
	mux.HandleFunc("/api/v1/config/", m.configHandler)
	mux.HandleFunc("/api/v1/functions", m.functionsHandler)
	mux.HandleFunc("/api/v1/functions/", m.functionsHandler)
	mux.HandleFunc("/api/v1/export", m.exportHandler)
	mux.HandleFunc("/api/v1/export/", m.exportHandler)
	mux.HandleFunc("/api/v1/import", m.importHandler)
	mux.HandleFunc("/api/v1/import/", m.importHandler)

	go func() {
		addr := net.JoinHostPort("", m.adminHTTPPort)
		logging.Infof("Admin HTTP server started: %s", addr)
		srv := &http.Server{
			Addr:         addr,
			ReadTimeout:  httpReadTimeOut,
			WriteTimeout: httpWriteTimeOut,
			Handler:      mux,
		}
		err := srv.ListenAndServe()
		logging.Fatalf("Error in Admin HTTP Server: %v", err)
	}()

	if m.adminSSLPort != "" {
		sslAddr := net.JoinHostPort("", m.adminSSLPort)
		reload := false
		var tlslsnr *net.Listener

		refresh := func() error {
			if tlslsnr != nil {
				reload = true
				(*tlslsnr).Close()
			}
			return nil
		}

		go func() {
			for {
				err := cbauth.RegisterTLSRefreshCallback(refresh)
				if err == nil {
					break
				}
				logging.Errorf("Unable to register for cert refresh, will retry: %v", err)
				time.Sleep(10 * time.Second)
			}
			for {
				cert, err := tls.LoadX509KeyPair(m.certFile, m.keyFile)
				if err != nil {
					logging.Errorf("Error in loading SSL certificate: %v", err)
					return
				}

				clientAuthType, err := cbauth.GetClientCertAuthType()
				if err != nil {
					logging.Errorf("Error in getting client cert auth type, %v", err)
					return
				}

				config := &tls.Config{
					Certificates:             []tls.Certificate{cert},
					CipherSuites:             []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
					MinVersion:               tls.VersionTLS12,
					PreferServerCipherSuites: true,
					ClientAuth:               clientAuthType,
				}

				if clientAuthType != tls.NoClientCert {
					caCert, err := ioutil.ReadFile(m.certFile)
					if err != nil {
						logging.Errorf("Error in reading cacert file, %v", err)
						return
					}
					caCertPool := x509.NewCertPool()
					caCertPool.AppendCertsFromPEM(caCert)
					config.ClientCAs = caCertPool
				}

				// allow only strong ssl as this is an internal API and interop is not a concern
				sslsrv := &http.Server{
					Addr:         sslAddr,
					ReadTimeout:  httpReadTimeOut,
					WriteTimeout: httpWriteTimeOut,
					TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
					TLSConfig:    config,
					Handler:      mux,
				}
				reload = false
				logging.Infof("SSL server started: %v", sslAddr)
				err = sslsrv.ListenAndServeTLS(m.certFile, m.keyFile)
				if reload {
					logging.Warnf("SSL certificate change: %v", err)
				} else {
					logging.Errorf("Error in SSL Server: %v", err)
					return
				}
			}
		}()
	}
}

func (m *ServiceMgr) disableDebugger() {
	logPrefix := "ServiceMgr::enableDebugger"

	config, info := m.getConfig()
	if info.Code != m.statusCodes.ok.Code {
		return
	}

	if _, exists := config["enable_debugger"]; exists {
		logging.Tracef("%s enable_debugger field exists , not making any change", logPrefix)
		return
	}

	logging.Tracef("%s enable_debugger field does not exist, enabling it", logPrefix)

	config["enable_debugger"] = false
	if info := m.saveConfig(config); info.Code != m.statusCodes.ok.Code {
		logging.Errorf("Unable to enable debugger by default, err: %v", info.Info)
	}
}

func (m *ServiceMgr) registerWithServer() error {
	cfg := m.config.Load()
	logging.Infof("Registering against cbauth_service, uuid: %v", cfg["uuid"].(string))

	err := service.RegisterManager(m, nil)
	if err != nil {
		logging.Errorf("Failed to register against cbauth_service, err: %v", err)
		return err
	}

	return nil
}

func (m *ServiceMgr) prepareRebalance(change service.TopologyChange) error {

	if isSingleNodeRebal(change) {
		if change.KeepNodes[0].NodeInfo.NodeID == m.nodeInfo.NodeID {
			logging.Infof("ServiceMgr::prepareRebalance - only node in the cluster")
		} else {
			return fmt.Errorf("node receiving prepare request isn't part of the cluster")
		}
	}

	return nil
}

func (m *ServiceMgr) startRebalance(change service.TopologyChange) error {
	logPrefix := "ServiceMgr::startRebalance"

	// Reset the failoverNotif flag, which got set to signify failover action on the cluster
	if m.failoverNotif {
		m.failoverNotif = false
	}

	m.rebalanceCtx = &rebalanceContext{
		change: change,
		rev:    0,
	}

	logging.Infof("%s Garbage collecting old rebalance tokens", logPrefix)
	// Garbage collect old Rebalance Tokens
	util.Retry(util.NewFixedBackoff(time.Second), nil, cleanupEventingMetaKvPath, metakvRebalanceTokenPath)
	logging.Infof("%s Writing rebalance token: %s to metakv", logPrefix, change.ID)
	path := metakvRebalanceTokenPath + change.ID
	util.Retry(util.NewFixedBackoff(time.Second), nil, metaKVSetCallback, path, change.ID)

	m.updateRebalanceProgressLocked(0.0)

	return nil
}

func (m *ServiceMgr) updateRebalanceProgressLocked(progress float64) {
	changeID := m.rebalanceCtx.change.ID
	rev := m.rebalanceCtx.incRev()

	task := &service.Task{
		Rev:          encodeRev(rev),
		ID:           fmt.Sprintf("%s", changeID),
		Type:         service.TaskTypeRebalance,
		Status:       service.TaskStatusRunning,
		IsCancelable: true,
		Progress:     progress,

		Extra: map[string]interface{}{
			"rebalanceID": changeID,
		},
	}

	m.updateStateLocked(func(s *state) {
		s.rebalanceTask = task
	})
}

func (ctx *rebalanceContext) incRev() uint64 {
	curr := ctx.rev
	ctx.rev++

	return curr
}

func (m *ServiceMgr) wait(rev service.Revision, cancel service.Cancel) (state, error) {
	m.mu.Lock()
	unlock := newCleanup(func() {
		m.mu.Unlock()
	})
	defer unlock.run()

	currState := m.copyStateLocked()

	if rev == nil {
		return currState, nil
	}

	haveRev := decodeRev(rev)
	if haveRev != m.rev {
		return currState, nil
	}

	ch := m.addWaiterLocked()
	unlock.run()

	select {
	case <-cancel:
		return state{}, service.ErrCanceled
	case newState := <-ch:
		return newState, nil
	}
}

func stateToTaskList(s state) *service.TaskList {
	tasks := &service.TaskList{}

	tasks.Rev = encodeRev(s.rev)
	tasks.Tasks = make([]service.Task, 0)

	if s.rebalanceTask != nil {
		tasks.Tasks = append(tasks.Tasks, *s.rebalanceTask)
	}

	return tasks
}

func (m *ServiceMgr) stateToTopology(s state) *service.Topology {
	topology := &service.Topology{}

	topology.Rev = encodeRev(s.rev)
	topology.Nodes = append([]service.NodeID(nil), m.servers...)
	topology.IsBalanced = true
	topology.Messages = nil

	return topology
}

func (m *ServiceMgr) addWaiterLocked() waiter {
	ch := make(waiter, 1)
	m.waiters[ch] = struct{}{}

	return ch
}

func (m *ServiceMgr) removeWaiter(w waiter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.waiters, w)
}

func (m *ServiceMgr) copyStateLocked() state {
	return m.state
}

func (m *ServiceMgr) cancelActualTaskLocked(task *service.Task) error {
	switch task.Type {
	case service.TaskTypeRebalance:
		return m.cancelRebalanceTaskLocked(task)
	default:
		panic("can't happen")
	}
}

func (m *ServiceMgr) cancelRebalanceTaskLocked(task *service.Task) error {
	switch task.Status {
	case service.TaskStatusRunning:
		return m.cancelRunningRebalanceTaskLocked(task)
	case service.TaskStatusFailed:
		return m.cancelFailedRebalanceTaskLocked()
	default:
		panic("can't happen")
	}
}

func (m *ServiceMgr) cancelRunningRebalanceTaskLocked(task *service.Task) error {
	logPrefix := "ServiceMgr::cancelRunningRebalanceTaskLocked"

	m.rebalancer.cancel()
	m.onRebalanceDoneLocked(nil)

	util.Retry(util.NewFixedBackoff(time.Second), nil, stopRebalanceCallback, m.rebalancer, task.ID)

	logging.Infof("%s Updated rebalance token: %s in metakv as part of stopping ongoing rebalance", logPrefix, task.ID)

	return nil
}

func (m *ServiceMgr) cancelFailedRebalanceTaskLocked() error {
	m.updateStateLocked(func(s *state) {
		s.rebalanceTask = nil
	})

	return nil
}

func isSingleNodeRebal(change service.TopologyChange) bool {
	if len(change.KeepNodes) == 1 && len(change.EjectNodes) == 0 {
		return true
	}
	return false
}

func (m *ServiceMgr) updateState(body func(state *state)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateStateLocked(body)
}

func (m *ServiceMgr) updateStateLocked(body func(state *state)) {
	body(&m.state)
	m.state.rev++

	m.notifyWaitersLocked()
}

func (m *ServiceMgr) notifyWaitersLocked() {
	s := m.copyStateLocked()
	for ch := range m.waiters {
		if ch != nil {
			ch <- s
		}
	}

	m.waiters = make(waiters)
}

func (m *ServiceMgr) runRebalanceCallback(cancel <-chan struct{}, body func()) {

	done := make(chan struct{})

	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		select {
		case <-cancel:
			break
		default:
			body()
		}

		close(done)
	}()

	select {
	case <-done:
	case <-cancel:
	}
}

func (m *ServiceMgr) rebalanceProgressCallback(progress float64, cancel <-chan struct{}) {
	m.runRebalanceCallback(cancel, func() {
		m.updateRebalanceProgressLocked(progress)
	})
}

func (m *ServiceMgr) rebalanceDoneCallback(err error, cancel <-chan struct{}) {
	m.runRebalanceCallback(cancel, func() {
		m.onRebalanceDoneLocked(err)
	})
}

func (m *ServiceMgr) onRebalanceDoneLocked(err error) {
	newTask := (*service.Task)(nil)
	if err != nil {
		ctx := m.rebalanceCtx
		rev := ctx.incRev()

		newTask = &service.Task{
			Rev:          encodeRev(rev),
			ID:           fmt.Sprintf("rebalance/%s", ctx.change.ID),
			Type:         service.TaskTypeRebalance,
			Status:       service.TaskStatusFailed,
			IsCancelable: true,

			ErrorMessage: err.Error(),

			Extra: map[string]interface{}{
				"rebalanceId": ctx.change.ID,
			},
		}
	}

	m.rebalancer = nil
	m.rebalanceCtx = nil

	m.updateStateLocked(func(s *state) {
		s.rebalanceTask = newTask
		s.rebalanceID = ""
	})
}

func (m *ServiceMgr) getActiveNodeAddrs() ([]string, error) {
	logPrefix := "ServiceMgr::getActiveNodeAddrs"

	util.Retry(util.NewFixedBackoff(time.Second), nil, getEventingNodesAddressesOpCallback, m, true)

	nodeAddrs := make([]string, 0)

	// m.eventingNodeAddrs would capture all online Eventing nodes in the cluster, so it might include
	// Eventing nodes that aren't actually part of cluster yet but were requested as part of KeepNodes
	// from ns_server in PrepareTopologyChange RPC call. So filtering out only the previously existing
	// Eventing nodes to check if any app is undergoing bootstrap is needed.
	addrUUIDMap, err := util.GetNodeUUIDs("/uuid", m.eventingNodeAddrs)
	if err != nil {
		logging.Errorf("%s Failed to get eventing node uuids, err: %v", logPrefix, err)
		return nodeAddrs, err
	}

	var data []byte
	util.Retry(util.NewFixedBackoff(time.Second), nil, metakvGetCallback, metakvConfigKeepNodes, &data)

	if len(data) == 0 {
		return nodeAddrs, nil
	}

	var keepNodes []string
	err = json.Unmarshal(data, &keepNodes)
	if err != nil {
		logging.Warnf("%s Failed to unmarshal keepNodes received from metakv, err: %v",
			logPrefix, err)
		return nodeAddrs, err
	}

	for _, uuid := range keepNodes {
		if nodeAddr, ok := addrUUIDMap[uuid]; ok {
			nodeAddrs = append(nodeAddrs, nodeAddr)
		}
	}

	logging.Debugf("%s keepNodes from metakv: %v addrUUIDMap: %rm nodeAddrs: %rs",
		logPrefix, keepNodes, addrUUIDMap, nodeAddrs)

	return nodeAddrs, nil
}
