package utils

import (
	"io/ioutil"
	"log"
	"os"
)

type AviLogger struct {
	Trace   *log.Logger
	Info    *log.Logger
	Warning *log.Logger
	Error   *log.Logger
}

var AviLog AviLogger

const (
	InfoColor  = "\033[1;32mINFO: \033[0m"
	WarnColor  = "\033[1;33mWARNING: \033[0m"
	ErrColor   = "\033[1;31mERROR: \033[0m"
	TraceColor = "\033[0;36mTRACE: \033[0m"
)

func init() {
	// TODO (sudswas): evaluate if moving to a Regular function is better than package init)
	// Change from ioutil.Discard for log to appear
	AviLog.Trace = log.New(ioutil.Discard,
		TraceColor,
		log.Ldate|log.Ltime|log.Lshortfile)

	AviLog.Info = log.New(os.Stdout,
		InfoColor,
		log.Ldate|log.Ltime|log.Lshortfile)

	AviLog.Warning = log.New(os.Stdout,
		WarnColor,
		log.Ldate|log.Ltime|log.Lshortfile)

	AviLog.Error = log.New(os.Stdout,
		ErrColor,
		log.Ldate|log.Ltime|log.Lshortfile)
}
