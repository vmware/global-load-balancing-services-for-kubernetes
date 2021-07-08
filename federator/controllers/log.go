/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	DEFAULT_FILE_SUFFIX = "avi.log"
)

func getFilePath() string {
	return strings.TrimLeft(os.Getenv("LOG_FILE_PATH")+"/", "/")
}

func getPodName() string {
	return strings.TrimLeft(os.Getenv("POD_NAME")+".", ".")
}

// log file sample name /log/amko-0.amko-federator.log.1234
func getFileName() string {
	input := os.Getenv("LOG_FILE_NAME")
	if input == "" {
		input = DEFAULT_FILE_SUFFIX
	}
	fileName := fmt.Sprintf("%s%s%s.%d", getFilePath(), getPodName(), input, time.Now().Unix())
	return fileName
}

// GetLogWriter returns a log writer if USE_PVC is set to true.
// If USE_PVC is not defined, it returns a false indicating that
// the default console should be used for logging.
func GetLogWriter() (*lumberjack.Logger, bool, error) {
	usePVC := os.Getenv("USE_PVC")

	if usePVC != "true" {
		return nil, false, nil
	}

	logPath := getFileName()
	file, err := os.OpenFile(logPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, false, fmt.Errorf("error in creating log file %s: %v", logPath, err)
	}
	if err := file.Close(); err != nil {
		return nil, false, fmt.Errorf("error in closing log file, %s: %v", logPath, err)
	}

	newLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    500, // megabytes after which new file is created
		MaxBackups: 5,   // number of backups
		MaxAge:     28,  // days
		Compress:   true,
	}
	return newLogger, true, nil
}
