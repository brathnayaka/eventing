package producer

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/couchbase/eventing/common"
	"github.com/couchbase/eventing/logging"
	"github.com/couchbase/eventing/util"
)

// Generates the vbucket to eventing node assignment, ideally generated map should
// be consistent across all nodes
func (p *Producer) vbEventingNodeAssign() error {
	logPrefix := "Producer::vbEventingNodeAssign"

	// Adding a sleep to mitigate stale values from metakv
	time.Sleep(5 * time.Second)

	util.Retry(util.NewFixedBackoff(time.Second), getKVNodesAddressesOpCallback, p)

	util.Retry(util.NewFixedBackoff(time.Second), getEventingNodesAddressesOpCallback, p)

	util.Retry(util.NewFixedBackoff(time.Second), getNsServerNodesAddressesOpCallback, p)

	// Would include eventing nodes that are about to be ejected out of the cluster
	onlineEventingNodes := p.getEventingNodeAddrs()
	if len(onlineEventingNodes) <= 0 {
		return fmt.Errorf("%v", errorUnexpectedEventingNodeCount)
	}

	// In-case of eventing node(s) removal, ns_server would reflect those node(s) within
	// eventing MDS service. Hence comparing node uuids received from prepareTopologyChange
	// call to uuids published by eventing nodes
	addrUUIDMap, err := util.GetNodeUUIDs("/uuid", onlineEventingNodes)
	if err != nil {
		logging.Errorf("%s [%s:%d] Failed to get eventing node uuids, err: %v",
			logPrefix, p.appName, p.LenRunningConsumers(), err)
		return err
	}

	// This will kick off post eventing-producer bootstrap in cases where it's killed and then
	// re-spawned by babysitter. Reads from metakv the list of keep nodes
	// from metakv, which were written on last StartTopologyChange RPC call
	if len(p.eventingNodeUUIDs) == 1 && p.eventingNodeUUIDs[0] == p.uuid {
		var data []byte
		util.Retry(util.NewFixedBackoff(bucketOpRetryInterval), metakvGetCallback, p, metakvConfigKeepNodes, &data)

		var keepNodes []string
		err := json.Unmarshal(data, &keepNodes)
		if err != nil {
			logging.Errorf("%s [%s:%d] Failed to unmarshal keep nodes received from metakv, err: %v",
				logPrefix, p.appName, p.LenRunningConsumers(), err)
		} else {
			if len(keepNodes) > 0 {
				logging.Infof("%s [%s:%d] Updating Eventing keep nodes uuids. Previous: %v current: %v",
					logPrefix, p.appName, p.LenRunningConsumers(), p.eventingNodeUUIDs, keepNodes)
				p.eventingNodeUUIDs = append([]string(nil), keepNodes...)
			} else {
				logging.Errorf("%s [%s:%d] KeepNodes is empty: %v",
					logPrefix, p.appName, p.LenRunningConsumers(), keepNodes)
			}
		}
	}

	// Only includes nodes that supposed to be part of cluster post StartTopologyChange call
	eventingNodeAddrs := make([]string, 0)
	for _, uuid := range p.eventingNodeUUIDs {
		eventingNodeAddrs = append(eventingNodeAddrs, addrUUIDMap[uuid])
	}
	sort.Strings(eventingNodeAddrs)

	logging.Debugf("%s [%s:%d] EventingNodeUUIDs: %v eventingNodeAddrs: %r",
		logPrefix, p.appName, p.LenRunningConsumers(), p.eventingNodeUUIDs, eventingNodeAddrs)

	vbucketsPerNode := p.numVbuckets / len(eventingNodeAddrs)
	var vbNo int
	var startVb uint16

	p.Lock()
	defer p.Unlock()
	p.vbEventingNodeAssignMap = make(map[uint16]string)

	vbCountPerNode := make([]int, len(eventingNodeAddrs))
	for i := 0; i < len(eventingNodeAddrs); i++ {
		vbCountPerNode[i] = vbucketsPerNode
		vbNo += vbucketsPerNode
	}

	remainingVbs := p.numVbuckets - vbNo
	if remainingVbs > 0 {
		for i := 0; i < remainingVbs; i++ {
			vbCountPerNode[i] = vbCountPerNode[i] + 1
		}
	}

	p.statsRWMutex.Lock()
	defer p.statsRWMutex.Unlock()
	p.plannerNodeMappings = make([]*common.PlannerNodeVbMapping, 0)

	for i, v := range vbCountPerNode {

		logging.Debugf("%s [%s:%d] EventingNodeUUIDs: %v Eventing node index: %d eventing node addr: %r startVb: %v vbs count: %v",
			logPrefix, p.appName, p.LenRunningConsumers(), p.eventingNodeUUIDs, i, eventingNodeAddrs[i], startVb, v)

		nodeMapping := &common.PlannerNodeVbMapping{
			Hostname: eventingNodeAddrs[i],
			StartVb:  int(startVb),
			VbsCount: v,
		}
		p.plannerNodeMappings = append(p.plannerNodeMappings, nodeMapping)

		for j := 0; j < v; j++ {
			p.vbEventingNodeAssignMap[startVb] = eventingNodeAddrs[i]
			startVb++
		}
	}
	return nil
}

func (p *Producer) initWorkerVbMap() {
	logPrefix := "Producer::initWorkerVbMap"

	hostAddress := net.JoinHostPort(util.Localhost(), p.nsServerPort)

	eventingNodeAddr, err := util.CurrentEventingNodeAddress(p.auth, hostAddress)
	if err != nil {
		logging.Errorf("%s [%s:%d] Failed to get address for current eventing node, err: %v",
			logPrefix, p.appName, p.LenRunningConsumers(), err)
	}

	// vbuckets the current eventing node is responsible to handle
	var vbucketsToHandle []uint16

	for k, v := range p.vbEventingNodeAssignMap {
		if v == eventingNodeAddr {
			vbucketsToHandle = append(vbucketsToHandle, k)
		}
	}

	sort.Sort(util.Uint16Slice(vbucketsToHandle))

	logging.Debugf("%s [%s:%d] eventingAddr: %r vbucketsToHandle, len: %d dump: %r",
		logPrefix, p.appName, p.LenRunningConsumers(), eventingNodeAddr, len(vbucketsToHandle), util.Condense(vbucketsToHandle))

	vbucketPerWorker := len(vbucketsToHandle) / p.workerCount
	var startVbIndex int

	vbCountPerWorker := make([]int, p.workerCount)
	for i := 0; i < p.workerCount; i++ {
		vbCountPerWorker[i] = vbucketPerWorker
		startVbIndex += vbucketPerWorker
	}

	remainingVbs := len(vbucketsToHandle) - startVbIndex
	if remainingVbs > 0 {
		for i := 0; i < remainingVbs; i++ {
			vbCountPerWorker[i] = vbCountPerWorker[i] + 1
		}
	}

	p.Lock()
	defer p.Unlock()

	var workerName string
	p.workerVbucketMap = make(map[string][]uint16)

	startVbIndex = 0

	for i := 0; i < p.workerCount; i++ {
		workerName = fmt.Sprintf("worker_%s_%d", p.appName, i)

		for j := 0; j < vbCountPerWorker[i]; j++ {
			p.workerVbucketMap[workerName] = append(p.workerVbucketMap[workerName], vbucketsToHandle[startVbIndex])
			startVbIndex++
		}

		logging.Debugf("%s [%s:%d] eventingAddr: %r worker name: %v assigned vbs len: %d dump: %r",
			logPrefix, p.appName, p.LenRunningConsumers(), eventingNodeAddr, workerName,
			len(p.workerVbucketMap[workerName]), util.Condense(p.workerVbucketMap[workerName]))
	}

}

func (p *Producer) getKvVbMap() {
	logPrefix := "Producer::getKvVbMap"

	var cinfo *util.ClusterInfoCache

	util.Retry(util.NewFixedBackoff(time.Second), getClusterInfoCacheOpCallback, p, &cinfo)

	kvAddrs := cinfo.GetNodesByServiceType(dataService)

	p.kvVbMap = make(map[uint16]string)

	for _, kvaddr := range kvAddrs {
		addr, err := cinfo.GetServiceAddress(kvaddr, dataService)
		if err != nil {
			logging.Errorf("%s [%s:%d] Failed to get address of KV host, err: %v",
				logPrefix, p.appName, p.LenRunningConsumers(), err)
			continue
		}

		vbs, err := cinfo.GetVBuckets(kvaddr, p.bucket)
		if err != nil {
			logging.Errorf("%s [%s:%d] Failed to get vbuckets for given kv util.NodeId, err: %v",
				logPrefix, p.appName, p.LenRunningConsumers(), err)
			continue
		}

		for i := 0; i < len(vbs); i++ {
			p.kvVbMap[uint16(vbs[i])] = addr
		}
	}
}
