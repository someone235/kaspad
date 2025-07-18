package main

import "github.com/kaspanet/kaspad/cmd/kaspawallet/daemon/server"

func startDaemon(config *startDaemonConfig) error {
	return server.Start(config.NetParams(), config.Listen, config.RPCServer, config.KeysFile, config.Profile, config.Timeout, config.LogLevel)
}
