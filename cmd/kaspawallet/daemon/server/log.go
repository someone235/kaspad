package server

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaspanet/kaspad/util"

	"github.com/kaspanet/kaspad/infrastructure/logger"
	"github.com/kaspanet/kaspad/util/panics"
)

var (
	backendLog = logger.NewBackend()
	log        = backendLog.Logger("KSWD")
	spawn      = panics.GoroutineWrapperFunc(log)

	defaultAppDir     = util.AppDir("kaspawallet", false)
	defaultLogFile    = filepath.Join(defaultAppDir, "daemon.log")
	defaultErrLogFile = filepath.Join(defaultAppDir, "daemon_err.log")
)

func initLog(logFile, errLogFile string, logLevelString string) {
	logLevel, found := logger.LevelFromString(logLevelString)
	log.SetLevel(logger.LevelDebug)
	err := backendLog.AddLogFile(logFile, logger.LevelTrace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding log file %s as log rotator for level %s: %s", logFile, logger.LevelTrace, err)
		os.Exit(1)
	}
	err = backendLog.AddLogFile(errLogFile, logger.LevelWarn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding log file %s as log rotator for level %s: %s", errLogFile, logger.LevelWarn, err)
		os.Exit(1)
	}
	err = backendLog.AddLogWriter(os.Stdout, logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding stdout to the loggerfor level %s: %s", logger.LevelWarn, err)
		os.Exit(1)
	}
	err = backendLog.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting the logger: %s ", err)
		os.Exit(1)
	}

	if !found {
		log.Warn("Invalid log level %s specified, defaulting to 'info'", logLevelString)
	}
}
