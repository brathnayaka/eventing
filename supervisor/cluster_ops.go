package supervisor

import (
	"fmt"
	"net"

	"github.com/couchbase/cbauth"
	"github.com/couchbase/eventing/logging"
	"github.com/couchbase/eventing/util"
)

var getHTTPServiceAuth = func(args ...interface{}) error {
	s := args[0].(*SuperSupervisor)
	user := args[1].(*string)
	password := args[2].(*string)

	var err error
	clusterURL := net.JoinHostPort(util.Localhost(), s.restPort)
	*user, *password, err = cbauth.GetHTTPServiceAuth(clusterURL)
	if err != nil {
		logging.Errorf("SSCO Failed to get cluster auth details, err: %v", err)
	}
	return err
}

var getEventingNodeAddrsCallback = func(args ...interface{}) error {
	s := args[0].(*SuperSupervisor)
	addrs := args[1].(*[]string)

	var err error
	clusterURL := net.JoinHostPort(util.Localhost(), s.restPort)
	*addrs, err = util.EventingNodesAddresses(s.auth, clusterURL)
	if err != nil {
		logging.Errorf("SSCO Failed to get addresses for nodes running eventing service, err: %v", err)
	} else if len(*addrs) == 0 {
		logging.Errorf("SSCO no eventing nodes reported")
		return fmt.Errorf("0 nodes reported for eventing service, unexpected")
	} else {
		logging.Infof("SSCO addrs: %r", fmt.Sprintf("%#v", addrs))
	}
	return err
}

var getCurrentEventingNodeAddrCallback = func(args ...interface{}) error {
	s := args[0].(*SuperSupervisor)
	addr := args[1].(*string)

	var err error
	clusterURL := net.JoinHostPort(util.Localhost(), s.restPort)
	*addr, err = util.CurrentEventingNodeAddress(s.auth, clusterURL)
	if err != nil {
		logging.Errorf("SSVA Failed to get address for current eventing node, err: %v", err)
	}
	return err
}
