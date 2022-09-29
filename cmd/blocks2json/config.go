package main

import (
	"github.com/jessevdk/go-flags"
	"github.com/kaspanet/kaspad/infrastructure/config"
)

type configFlags struct {
	DbPath string `short:"d" long:"db-path" description:"Database path" required:"true"`
	config.NetworkFlags
}

func parseConfig() (*configFlags, error) {
	cfg := &configFlags{}
	parser := flags.NewParser(cfg, flags.HelpFlag)
	err := cfg.ResolveNetwork(parser)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}
